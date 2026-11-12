package center

import (
	"context"
	"crypto/sha256"
	dbsql "database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

const relaySessionKind = "asset-relay-session"
const relayChunkKind = "asset-relay-chunk"
const relayTransferTimeout = 2 * time.Hour
const relayMaximumBytes int64 = 512 << 20

type relaySession struct {
	ID            string                   `json:"id"`
	GateID        int                      `json:"gate_id"`
	ActionID      int                      `json:"action_id"`
	RequestKey    string                   `json:"request_key"`
	TraceID       string                   `json:"trace_id"`
	SHA256        string                   `json:"sha256"`
	Size          int64                    `json:"size"`
	MaxSize       int64                    `json:"max_size"`
	Received      int64                    `json:"received"`
	Chunks        int                      `json:"chunks"`
	Closed        bool                     `json:"closed"`
	ConsumerToken string                   `json:"consumer_token"`
	ConsumerUntil time.Time                `json:"consumer_until"`
	CreatedAt     time.Time                `json:"created_at"`
	ExpiresAt     time.Time                `json:"expires_at"`
	NextChunkAt   time.Time                `json:"next_chunk_at"`
	Progress      contracts.UploadProgress `json:"progress"`
}

type relayChunk struct {
	Offset int64  `json:"offset"`
	Body   []byte `json:"body"`
	SHA256 string `json:"sha256"`
}

func (s *Server) loadRelay(ctx context.Context, id string, gateID int) (relaySession, error) {
	session, err := s.readRelay(ctx, id, gateID)
	if err == nil && (session.Closed || !time.Now().Before(session.ExpiresAt)) {
		err = errors.New("upload_not_found")
	}
	return session, err
}

func (s *Server) readRelay(ctx context.Context, id string, gateID int) (relaySession, error) {
	var session relaySession
	if !validTraceHex(id, 32) {
		return session, errors.New("upload_not_found")
	}
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(relaySessionKind), resource.KeyEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return session, errors.New("upload_not_found")
	}
	if err != nil {
		return session, err
	}
	if decodeStrict(row.Data, &session) != nil || session.ID != id || session.GateID != gateID {
		return session, errors.New("upload_not_found")
	}
	if err := session.validate(); err != nil {
		return session, err
	}
	return session, nil
}

func (r relaySession) validate() error {
	if !validTraceHex(r.ID, 32) || !validTraceHex(r.ConsumerToken, 48) || !validTraceHex(r.SHA256, 64) || r.GateID <= 0 || r.ActionID <= 0 || r.MaxSize < 1 || r.MaxSize > relayMaximumBytes || r.Size < 0 || r.Size > r.MaxSize || r.Received < 0 || r.Received > r.MaxSize || r.Size > 0 && r.Received > r.Size || r.Chunks != int((r.Received+contracts.UploadChunk-1)/contracts.UploadChunk) || r.CreatedAt.IsZero() || !r.ExpiresAt.After(r.CreatedAt) || r.ExpiresAt.Sub(r.CreatedAt) > relayTransferTimeout {
		return errors.New("relay_session_invalid")
	}
	return nil
}

// Only the explicit fallback path stores binary chunks in the database. This
// lets any center accept an upload without requiring shared local directories.
// A single, bounded transfer avoids multiplying that exceptional storage load.
func (s *Server) openRelay(ctx context.Context, gateID int, req contracts.AssetFetch) (relaySession, error) {
	var session relaySession
	if req.MaxSize == 0 {
		req.MaxSize = 256 << 20
	}
	want, err := hex.DecodeString(req.SHA256)
	if err != nil || len(want) != sha256.Size || !validTraceHex(req.SHA256, 64) || req.Size < 0 || req.MaxSize < 1 || req.MaxSize > relayMaximumBytes || req.Size > req.MaxSize {
		return session, errors.New("invalid_relay_request")
	}
	key := digest(fmt.Sprintf("%d:%s:%d:%d", gateID, req.SHA256, req.Size, req.MaxSize))
	err = s.Store.Lock.Do(ctx, "asset-relay-slot", func(ctx context.Context) error {
		rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(relaySessionKind), resource.CreatedAtGT(time.Now().Add(-relayTransferTimeout)), func(q *sql.Selector) { q.Where(sqljson.ValueEQ(resource.FieldData, false, sqljson.Path("closed"))) }).Order(ent.Desc(resource.FieldID)).Limit(2).All(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var existing relaySession
			if decodeStrict(row.Data, &existing) != nil || existing.validate() != nil {
				return errors.New("relay_session_invalid")
			}
			if existing.Closed || !time.Now().Before(existing.ExpiresAt) {
				continue
			}
			if existing.RequestKey == key && existing.GateID == gateID {
				return s.Store.Lock.Do(ctx, "asset-relay:"+existing.ID, func(ctx context.Context) error {
					current, err := s.loadRelay(ctx, existing.ID, gateID)
					if err != nil {
						return err
					}
					if time.Now().Before(current.ConsumerUntil) {
						return errors.New("relay_transfer_busy")
					}
					current.ConsumerToken, current.ConsumerUntil = randomHex(24), time.Now().UTC().Add(time.Minute)
					session = current
					return s.Store.Ent.Resource.UpdateOne(row).SetData(toMap(current)).Exec(ctx)
				})
			}
			return errors.New("relay_transfer_busy")
		}
		now := time.Now().UTC()
		session = relaySession{ID: randomHex(16), GateID: gateID, RequestKey: key, TraceID: taskTrace(ctx), SHA256: req.SHA256, Size: req.Size, MaxSize: req.MaxSize, CreatedAt: now, ExpiresAt: now.Add(relayTransferTimeout)}
		session.ConsumerToken, session.ConsumerUntil = randomHex(24), now.Add(time.Minute)
		req.UploadID = session.ID
		if raw, err := json.Marshal(req); err != nil {
			return err
		} else if err = contracts.ValidateTaskPayload(contracts.ActionAssetFetch, contracts.TaskVersion, raw); err != nil {
			return err
		}
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		action, err := tx.GateAction.Create().SetGateID(gateID).SetKind(contracts.ActionAssetFetch).SetPayload(toMap(req)).SetStatus(gateaction.StatusPending).SetExpiresAt(session.ExpiresAt).Save(ctx)
		if err != nil {
			return err
		}
		session.ActionID = action.ID
		if _, err = tx.Resource.Create().SetKind(relaySessionKind).SetKey(session.ID).SetData(toMap(session)).Save(ctx); err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetAction("asset.relay.open").SetTarget(session.ID).SetResult("accepted").SetDetail(map[string]any{"trace_id": session.TraceID, "gate_id": gateID, "origin": "center.openRelay", "maximum_bytes": session.MaxSize}).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err == nil {
		if _, capErr := s.Store.Tasks.Capabilities(ctx, gateID); capErr == nil {
			if err = s.migratePendingActions(ctx, gateID); err != nil {
				return session, err
			}
		} else if !errors.Is(capErr, dbsql.ErrNoRows) {
			return session, capErr
		}
		// New workers migrate this row into the same durable task queue. Old
		// workers see only the compatibility row, never a second task delivery.
		_ = s.Store.Notifications.Publish(ctx, "gate:"+itoa(gateID))
		s.actionWaiters.notify(gateID)
	}
	return session, err
}

func (s *Server) closeRelay(ctx context.Context, id string, gateID int, consumer string) error {
	return s.Store.Lock.Do(ctx, "asset-relay:"+id, func(ctx context.Context) error {
		session, err := s.readRelay(ctx, id, gateID)
		if err != nil && err.Error() == "upload_not_found" {
			return nil
		}
		if err != nil {
			return err
		}
		if session.ConsumerToken != consumer {
			return errors.New("relay_consumer_lost")
		}
		if session.Closed {
			return nil
		}
		session.Closed = true
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = tx.Resource.Update().Where(resource.KindEQ(relaySessionKind), resource.KeyEQ(id)).SetData(toMap(session)).Exec(ctx); err != nil {
			return err
		}
		if _, err = tx.Resource.Delete().Where(resource.KindEQ(relayChunkKind), resource.KeyHasPrefix(id+":")).Exec(ctx); err != nil {
			return err
		}
		if _, err = tx.GateAction.Update().Where(gateaction.IDEQ(session.ActionID), gateaction.StatusIn(gateaction.StatusPending, gateaction.StatusRunning)).SetStatus(gateaction.StatusFailed).SetError("relay_session_closed").SetFinishedAt(time.Now()).Save(ctx); err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetAction("asset.relay.close").SetTarget(id).SetResult("closed").SetDetail(map[string]any{"trace_id": session.TraceID, "gate_id": gateID, "received_bytes": session.Received, "origin": "center.closeRelay"}).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (s *Server) renewRelayConsumer(ctx context.Context, previous relaySession) (relaySession, error) {
	var current relaySession
	err := s.Store.Lock.Do(ctx, "asset-relay:"+previous.ID, func(ctx context.Context) error {
		var err error
		current, err = s.loadRelay(ctx, previous.ID, previous.GateID)
		if err != nil {
			return err
		}
		if current.ConsumerToken != previous.ConsumerToken {
			return errors.New("relay_consumer_lost")
		}
		current.ConsumerUntil = time.Now().UTC().Add(time.Minute)
		return s.Store.Ent.Resource.Update().Where(resource.KindEQ(relaySessionKind), resource.KeyEQ(current.ID)).SetData(toMap(current)).Exec(ctx)
	})
	return current, err
}

func (s *Server) writeRelayChunk(ctx context.Context, id string, gateID int, offset int64, body []byte, rate int64, guards ...func(context.Context) error) (relaySession, error) {
	var session relaySession
	err := s.Store.Lock.Do(ctx, "asset-relay:"+id, func(ctx context.Context) error {
		var err error
		session, err = s.loadRelay(ctx, id, gateID)
		if err != nil {
			return err
		}
		if offset != session.Received {
			return errors.New("offset_mismatch")
		}
		end := offset + int64(len(body))
		if len(body) == 0 || len(body) > contracts.UploadChunk || end > session.MaxSize || session.Size > 0 && end > session.Size || session.Received%contracts.UploadChunk != 0 || session.Chunks >= int(relayMaximumBytes/contracts.UploadChunk) {
			return errors.New("upload_too_large")
		}
		if delay := time.Until(session.NextChunkAt); delay > 0 {
			if delay > 10*time.Second {
				return errors.New("relay_rate_state_invalid")
			}
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		if rate < 64<<10 || rate > 128<<20 {
			return errors.New("relay_rate_invalid")
		}
		for _, guard := range guards {
			if err := guard(ctx); err != nil {
				return err
			}
		}
		sum := sha256.Sum256(body)
		chunk := relayChunk{Offset: offset, Body: body, SHA256: hex.EncodeToString(sum[:])}
		session.Received, session.Chunks = end, session.Chunks+1
		session.NextChunkAt = time.Now().UTC().Add(time.Duration(int64(len(body)) * int64(time.Second) / rate))
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.Resource.Create().SetKind(relayChunkKind).SetKey(fmt.Sprintf("%s:%012d", id, offset)).SetData(toMap(chunk)).Save(ctx); err != nil {
			return err
		}
		if err = tx.Resource.Update().Where(resource.KindEQ(relaySessionKind), resource.KeyEQ(id)).SetData(toMap(session)).Exec(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
	return session, err
}

func (s *Server) materializeRelay(ctx context.Context, session relaySession, destination string) (err error) {
	return s.Store.Lock.Do(ctx, "asset-relay:"+session.ID, func(ctx context.Context) error {
		current, err := s.loadRelay(ctx, session.ID, session.GateID)
		if err != nil {
			return err
		}
		if current.ConsumerToken != session.ConsumerToken {
			return errors.New("relay_consumer_lost")
		}
		// Holding the session lock prevents takeover and chunk deletion while
		// verification is in progress, including on a different center.
		return s.materializeRelayLocked(ctx, current, destination)
	})
}

func (s *Server) materializeRelayLocked(ctx context.Context, session relaySession, destination string) (err error) {
	// The caller owns this temporary download path. Do not publish partial or
	// unverified content, even when a Gate reports a successful task result.
	f, err := os.CreateTemp(filepath.Dir(destination), ".weir-relay-")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer func() { _ = f.Close(); _ = os.Remove(temporary) }()
	h := sha256.New()
	offset := int64(0)
	for i := 0; i < session.Chunks; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(relayChunkKind), resource.KeyEQ(fmt.Sprintf("%s:%012d", session.ID, offset))).Only(ctx)
		if err != nil {
			return err
		}
		var chunk relayChunk
		if decodeStrict(row.Data, &chunk) != nil || chunk.Offset != offset || len(chunk.Body) == 0 || len(chunk.Body) > contracts.UploadChunk || i < session.Chunks-1 && len(chunk.Body) != contracts.UploadChunk {
			return errors.New("relay_chunk_invalid")
		}
		sum := sha256.Sum256(chunk.Body)
		if hex.EncodeToString(sum[:]) != chunk.SHA256 {
			return errors.New("relay_chunk_digest_mismatch")
		}
		if _, err = io.MultiWriter(f, h).Write(chunk.Body); err != nil {
			return err
		}
		offset += int64(len(chunk.Body))
		if offset > session.MaxSize {
			return errors.New("upload_too_large")
		}
	}
	if offset != session.Received || session.Size > 0 && offset != session.Size || hex.EncodeToString(h.Sum(nil)) != session.SHA256 {
		return errors.New("relay_digest_mismatch")
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	session.ConsumerUntil = time.Now().UTC().Add(time.Minute)
	if err = s.Store.Ent.Resource.Update().Where(resource.KindEQ(relaySessionKind), resource.KeyEQ(session.ID)).SetData(toMap(session)).Exec(ctx); err != nil {
		return err
	}
	if err = os.Rename(temporary, destination); err != nil {
		return err
	}
	return nil
}

func (s *Server) validateRelayResult(ctx context.Context, record taskqueue.Record, raw json.RawMessage) error {
	var result contracts.AssetFetchResult
	if err := contracts.DecodeStrict(raw, &result, 4096); err != nil {
		return err
	}
	body, err := s.Crypto.Decrypt(record.Ciphertext)
	if err != nil {
		return err
	}
	var req contracts.AssetFetch
	if err = contracts.DecodeStrict([]byte(body), &req, contracts.MaxTaskPayload); err != nil {
		return err
	}
	session, err := s.loadRelay(ctx, req.UploadID, record.GateID)
	if err != nil {
		return err
	}
	if session.ActionID != record.LegacyID || session.SHA256 != req.SHA256 || session.Size != req.Size || session.MaxSize != req.MaxSize || result.Size != session.Received || session.Size > 0 && result.Size != session.Size || result.Size <= 0 {
		return errors.New("relay_result_invalid")
	}
	return nil
}

func (s *Server) pruneRelaySessions(ctx context.Context, now time.Time) error {
	return s.Store.Lock.Do(ctx, "asset-relay-slot", func(ctx context.Context) error {
		rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(relaySessionKind), resource.CreatedAtLT(now.Add(-relayTransferTimeout))).Order(ent.Asc(resource.FieldID)).Limit(100).All(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := s.Store.Lock.Do(ctx, "asset-relay:"+row.Key, func(ctx context.Context) error {
				tx, err := s.Store.Ent.Tx(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				if _, err = tx.Resource.Delete().Where(resource.KindEQ(relayChunkKind), resource.KeyHasPrefix(row.Key+":")).Exec(ctx); err != nil {
					return err
				}
				if err = tx.Resource.DeleteOneID(row.ID).Exec(ctx); err != nil {
					return err
				}
				return tx.Commit()
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
