package control

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var errCertificateRetryConflict = errors.New("certificate retry conflict")

// Retry the existing encrypted delivery when issuance already succeeded. A
// failed Gate receipt must not consume another CA issuance or change the key.
func (s *Server) queueCertificateRetry(ctx context.Context, deviceID, orderID int) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(8400000000)+int64(deviceID)); err != nil {
		return "", err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE id=$1 AND kind='certificate-order' AND data->>'device_id'=$2 FOR UPDATE`, orderID, strconv.Itoa(deviceID)).Scan(&raw); err != nil {
		return "", errCertificateRetryConflict
	}
	var data map[string]any
	if json.Unmarshal(raw, &data) != nil || data["status"] != "failed" || number(data["attempts"]) >= 3 {
		return "", errCertificateRetryConflict
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migate.resources WHERE kind='certificate-order' AND data->>'device_id'=$1 AND data->>'status' IN ('pending','running','deploying'))`, strconv.Itoa(deviceID)).Scan(&active); err != nil {
		return "", err
	}
	if active {
		return "", errCertificateRetryConflict
	}
	var status string
	var metadata []byte
	if err = tx.QueryRowContext(ctx, `SELECT status,metadata FROM migate.devices WHERE id=$1 FOR SHARE`, deviceID).Scan(&status, &metadata); err != nil || status != "approved" {
		return "", errCertificateRetryConflict
	}
	var meta map[string]any
	json.Unmarshal(metadata, &meta)
	capable := false
	if caps, ok := meta["capabilities"].([]any); ok {
		for _, cap := range caps {
			capable = capable || cap == "certificate.delivery.v1"
		}
	}
	if !capable {
		return "", errCertificateRetryConflict
	}
	nextStatus := "pending"
	if data["last_error"] == "certificate_delivery_failed" {
		var payloadRaw []byte
		var operation, taskStatus string
		var expires time.Time
		if err = tx.QueryRowContext(ctx, `SELECT operation,status,expires_at,payload FROM migate.tasks WHERE id=$1 AND device_id=$2 FOR UPDATE`, int(number(data["delivery_task_id"])), deviceID).Scan(&operation, &taskStatus, &expires, &payloadRaw); err != nil {
			return "", errCertificateRetryConflict
		}
		if operation != "certificate.upsert" || taskStatus == "applied" || taskStatus != "failed" && time.Now().Before(expires) {
			return "", errCertificateRetryConflict
		}
		var payload map[string]any
		if json.Unmarshal(payloadRaw, &payload) != nil || number(payload["id"]) != number(data["certificate_id"]) {
			return "", errCertificateRetryConflict
		}
		block, _ := pem.Decode([]byte(text(payload["certificate_pem"])))
		if block == nil {
			return "", errCertificateRetryConflict
		}
		leaf, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil || !time.Now().Before(leaf.NotAfter) {
			return "", errCertificateRetryConflict
		}
		key, decryptErr := s.Crypto.Decrypt(text(payload["sealed_key"]))
		if decryptErr != nil || !strings.HasPrefix(key, fmt.Sprintf("certificate:%d:%d:", deviceID, int(number(data["certificate_id"])))) {
			return "", errCertificateRetryConflict
		}
		attempt := int(number(data["attempts"])) + 1
		var taskID int
		if err = tx.QueryRowContext(ctx, `INSERT INTO migate.tasks(device_id,operation,key,revision,payload,status,attempts,expires_at,leased_until,created_at) VALUES($1,'certificate.upsert',$2,1,$3,'pending',0,$4,$5,$6) RETURNING id`, deviceID, fmt.Sprintf("dns01:%d:%d", orderID, attempt), payloadRaw, time.Now().Add(24*time.Hour), time.Unix(0, 0), time.Now()).Scan(&taskID); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE migate.tasks SET revision=id WHERE id=$1`, taskID); err != nil {
			return "", err
		}
		data["delivery_task_id"], data["attempts"] = taskID, attempt
		nextStatus = "deploying"
	}
	data["status"] = nextStatus
	delete(data, "last_error")
	delete(data, "finished_at")
	raw, err = json.Marshal(data)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE migate.resources SET data=$1 WHERE id=$2`, raw, orderID); err != nil {
		return "", err
	}
	return nextStatus, tx.Commit()
}
