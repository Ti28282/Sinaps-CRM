package store

import (
	"context"
	"time"
)

// Плагин «Уведомления на телефон»: телефоны, журнал событий, отметки напоминаний.

// NotifyDevice — привязанный телефон. Key — общий ключ шифрования (из привязки).
type NotifyDevice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Key      []byte `json:"-"`
	PairedAt int64  `json:"pairedAt"`
	LastSeen int64  `json:"lastSeen"`
}

func (s *Store) NotifyDevices(ctx context.Context) ([]NotifyDevice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, key, paired_at, last_seen FROM notify_devices ORDER BY paired_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotifyDevice{}
	for rows.Next() {
		var d NotifyDevice
		if err := rows.Scan(&d.ID, &d.Name, &d.Key, &d.PairedAt, &d.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) NotifySaveDevice(ctx context.Context, d NotifyDevice) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO notify_devices(id,name,key,paired_at,last_seen) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, key=excluded.key`, d.ID, d.Name, d.Key, d.PairedAt, d.LastSeen)
	return err
}

func (s *Store) NotifyTouchDevice(ctx context.Context, id, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notify_devices SET last_seen=?, name=CASE WHEN ?<>'' THEN ? ELSE name END WHERE id=?`,
		time.Now().Unix(), name, name, id)
	return err
}

func (s *Store) NotifyDeleteDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notify_devices WHERE id=?`, id)
	return err
}

// NotifyAddEvent кладёт событие в журнал и возвращает его номер. Хранятся последние 300.
func (s *Store) NotifyAddEvent(ctx context.Context, body string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO notify_events(at, body) VALUES(?,?)`, time.Now().Unix(), body)
	if err != nil {
		return 0, err
	}
	seq, _ := res.LastInsertId()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM notify_events WHERE seq <= ?`, seq-300)
	return seq, nil
}

// NotifyEventsAfter — события новее seq (не больше limit), по порядку.
func (s *Store) NotifyEventsAfter(ctx context.Context, seq int64, limit int) (seqs []int64, bodies []string, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq, body FROM (
  SELECT seq, body FROM notify_events WHERE seq > ? ORDER BY seq DESC LIMIT ?
) ORDER BY seq`, seq, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n int64
		var b string
		if err := rows.Scan(&n, &b); err != nil {
			return nil, nil, err
		}
		seqs, bodies = append(seqs, n), append(bodies, b)
	}
	return seqs, bodies, rows.Err()
}

func (s *Store) NotifyLastSeq(ctx context.Context) int64 {
	var n int64
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM notify_events`).Scan(&n)
	return n
}

// NotifyMark запоминает, что о (kind, ref) уже напоминали. first=true — это первый раз.
func (s *Store) NotifyMark(ctx context.Context, kind, ref string) (first bool, err error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO notify_marks(kind, ref, at) VALUES(?,?,?) ON CONFLICT(kind, ref) DO NOTHING`,
		kind, ref, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SilentChats — клиенты, которым мы написали последними и они молчат дольше since.
func (s *Store) SilentChats(ctx context.Context, before int64) ([]Chat, error) {
	return s.chatsWhere(ctx, `last_out=1 AND last_at>0 AND last_at<? AND stage IN ('sent','replied') AND channel IN ('tg','wa')`, before)
}

// WaitingReceipts — чеки в статусе «ожидает» старше before.
func (s *Store) WaitingReceipts(ctx context.Context, before int64) ([]Receipt, error) {
	all, err := s.Receipts(ctx, 0, 0)
	if err != nil {
		return nil, err
	}
	out := []Receipt{}
	for _, r := range all {
		if r.Status == "wait" && r.PaidAt < before {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Store) chatsWhere(ctx context.Context, where string, args ...any) ([]Chat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+chatCols+` FROM chats WHERE `+where+` ORDER BY last_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chat{}
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
