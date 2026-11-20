package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// StoredTime 是 SQLite 时间列读出的时刻及其原样存储文本；Valid 为 false 表示 NULL。
type StoredTime struct {
	Time  time.Time
	Text  string
	Valid bool
}

// CertificateStoredTimes 是证书行的 updated_at、not_after、renewed_at 的原样存储。
type CertificateStoredTimes struct {
	UpdatedAt, NotAfter, RenewedAt StoredTime
}

// SQLiteCertificateStoredTimes 在一条语句里读出证书时间列的时刻与原文，供调用方做精确 CAS。
// SQLite 保存时间的原始时区写法，Ent 读出时只保留时刻，按时刻比较会与存储文本不一致；
// 读取原文需要 CAST(... AS TEXT)，Ent 不提供这种列表达式。行不存在时 found 为 false。
func (s *Store) SQLiteCertificateStoredTimes(ctx context.Context, id int) (times CertificateStoredTimes, found bool, err error) {
	if s.Driver != DriverSQLite {
		return times, false, errors.New("certificate stored times are only read on SQLite")
	}
	var expiry, renewed sql.NullTime
	var expiryRaw, renewedRaw sql.NullString
	err = s.DB.QueryRowContext(ctx, "SELECT updated_at, CAST(updated_at AS TEXT), not_after, CAST(not_after AS TEXT), renewed_at, CAST(renewed_at AS TEXT) FROM certificates WHERE id = ?", id).
		Scan(&times.UpdatedAt.Time, &times.UpdatedAt.Text, &expiry, &expiryRaw, &renewed, &renewedRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return times, false, nil
	}
	if err != nil {
		return times, false, err
	}
	times.UpdatedAt.Valid = true
	times.NotAfter = StoredTime{Time: expiry.Time, Text: expiryRaw.String, Valid: expiry.Valid && expiryRaw.Valid}
	times.RenewedAt = StoredTime{Time: renewed.Time, Text: renewedRaw.String, Valid: renewed.Valid && renewedRaw.Valid}
	return times, true, nil
}
