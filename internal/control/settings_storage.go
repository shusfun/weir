package control

import (
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/imzyb/MiGate/internal/mailer"
)

type storedSystemSettings struct {
	Security SecuritySettings  `json:"security"`
	SMTP     mailer.SMTPConfig `json:"smtp"`
	Laya     ModelConfig       `json:"laya"`
	Revision string            `json:"settings_revision"`
}
type systemSettingsUpdate struct {
	Security          SecuritySettings  `json:"security"`
	SMTP              mailer.SMTPConfig `json:"smtp"`
	Laya              ModelConfig       `json:"laya"`
	Revision          string            `json:"settings_revision"`
	Password          string            `json:"password"`
	Code              string            `json:"code"`
	ClearSMTPPassword bool              `json:"clear_smtp_password"`
	ClearLayaKey      bool              `json:"clear_laya_key"`
}
type settingsQuerier interface {
	QueryContext(context.Context, string, ...any) (*stdsql.Rows, error)
}

func readSystemSettings(ctx context.Context, q settingsQuerier) (storedSystemSettings, error) {
	var out storedSystemSettings
	rows, err := q.QueryContext(ctx, `SELECT key,data FROM migate.resources WHERE kind='settings' AND key IN ('security','smtp','laya') ORDER BY key`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	values := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			return out, err
		}
		values[key] = raw
		var target any
		switch key {
		case "security":
			target = &out.Security
		case "smtp":
			target = &out.SMTP
		case "laya":
			target = &out.Laya
		}
		if err = json.Unmarshal(raw, target); err != nil {
			return out, err
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	// JSONB textual whitespace is stable, but canonicalize values before hashing.
	canonical := map[string]any{}
	for key, raw := range values {
		var v any
		if err = json.Unmarshal(raw, &v); err != nil {
			return out, err
		}
		canonical[key] = v
	}
	b, err := json.Marshal(canonical)
	if err != nil {
		return out, err
	}
	out.Revision = digest(string(b))
	return out, nil
}

var errSettingsConflict = errors.New("settings changed")

type settingsValidationError string

func (e settingsValidationError) Error() string { return string(e) }

func validateStoredSettings(v systemSettingsUpdate) error {
	if v.Security.RequireAdminMFA && !v.Security.TOTP && !v.Security.Passkey {
		return settingsValidationError("second_factor_method_required")
	}
	if v.Security.EmailVerification && v.SMTP.Host == "" {
		return settingsValidationError("smtp_required")
	}
	if len(v.SMTP.Host) > 253 || len(v.SMTP.Username) > 320 || len(v.SMTP.Password) > 8192 || len(v.SMTP.FromName) > 200 || strings.ContainsAny(v.SMTP.Host, "\r\n /\\@") || strings.ContainsAny(v.SMTP.FromName, "\r\n") {
		return settingsValidationError("invalid_smtp_settings")
	}
	if v.SMTP.Host != "" {
		if v.SMTP.Port < 1 || v.SMTP.Port > 65535 || !validEmail(v.SMTP.From) || v.SMTP.Username == "" || v.SMTP.Password == "" {
			return settingsValidationError("invalid_smtp_settings")
		}
		if net.ParseIP(v.SMTP.Host) == nil {
			for _, label := range strings.Split(v.SMTP.Host, ".") {
				if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
					return settingsValidationError("invalid_smtp_settings")
				}
				for _, c := range label {
					if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
						return settingsValidationError("invalid_smtp_settings")
					}
				}
			}
		}
	}
	if len(v.Laya.Key) > 8192 || len(v.Laya.Model) > 200 || strings.ContainsAny(v.Laya.Model, "\r\n") {
		return settingsValidationError("invalid_laya_settings")
	}
	if v.Laya.URL != "" {
		if _, err := modelEndpoint(v.Laya.URL); err != nil {
			return settingsValidationError("https_model_url_required")
		}
	}
	if v.Laya.Enabled && (v.Laya.URL == "" || v.Laya.Key == "" || strings.TrimSpace(v.Laya.Model) == "") {
		return settingsValidationError("invalid_laya_settings")
	}
	return nil
}

// Accept a base address or the native endpoint, without silently discarding
// URL credentials/query values or appending the native path twice.
func modelEndpoint(raw string) (*url.URL, error) {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\t ") {
		return nil, errors.New("invalid model URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return nil, errors.New("invalid model URL")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/v1/systemone") {
		u.Path += "/v1/systemone"
	}
	return u, nil
}

func (s *Server) writeSystemSettings(ctx context.Context, req systemSettingsUpdate) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(69442902)`); err != nil {
		return false, err
	}
	old, err := readSystemSettings(ctx, tx)
	if err != nil {
		return false, err
	}
	if req.Revision != "" && req.Revision != old.Revision {
		return false, errSettingsConflict
	}
	if req.ClearSMTPPassword && req.SMTP.Password != "" || req.ClearLayaKey && req.Laya.Key != "" {
		return false, settingsValidationError("invalid_secret_replacement")
	}
	// Validate with a presence placeholder for preserved encrypted secrets.
	if req.SMTP.Password == "" && !req.ClearSMTPPassword {
		req.SMTP.Password = old.SMTP.Password
	} else if req.SMTP.Password != "" {
		req.SMTP.Password, err = s.Crypto.Encrypt(req.SMTP.Password)
		if err != nil {
			return false, err
		}
	}
	if req.Laya.Key == "" && !req.ClearLayaKey {
		req.Laya.Key = old.Laya.Key
	} else if req.Laya.Key != "" {
		req.Laya.Key, err = s.Crypto.Encrypt(req.Laya.Key)
		if err != nil {
			return false, err
		}
	}
	if err = validateStoredSettings(req); err != nil {
		return false, err
	}
	for _, entry := range []struct {
		key   string
		value any
	}{{"security", req.Security}, {"smtp", req.SMTP}, {"laya", req.Laya}} {
		raw, e := json.Marshal(entry.value)
		if e != nil {
			return false, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,owner_id,data,created_at) VALUES('settings',$1,0,$2,$3) ON CONFLICT(kind,key) DO UPDATE SET data=EXCLUDED.data`, entry.key, raw, time.Now()); err != nil {
			return false, fmt.Errorf("settings write failed: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return old.Security != req.Security, nil
}
