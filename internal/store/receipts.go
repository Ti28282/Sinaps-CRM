package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Receipt — файл клиента, отмеченный как чек (плагин «Чеки»).
type Receipt struct {
	ID        int64  `json:"id"`
	ChatID    int64  `json:"chatId"`
	MsgID     int64  `json:"msgId"`
	ChatTitle string `json:"chatTitle"`
	Channel   string `json:"channel"`
	FileName  string `json:"fileName"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"` // paid | wait | refund
	What      string `json:"what"`
	PaidAt    int64  `json:"paidAt"`
	CreatedAt int64  `json:"createdAt"`
}

var receiptStatuses = map[string]bool{"paid": true, "wait": true, "refund": true}

// ReceiptStatusName — подпись статуса для таблиц и выгрузки.
func ReceiptStatusName(s string) string {
	switch s {
	case "wait":
		return "ожидает"
	case "refund":
		return "возврат"
	}
	return "оплачен"
}

const receiptCols = `r.id, r.chat_id, r.msg_id, COALESCE(c.title, r.chat_title), COALESCE(c.channel, r.channel),
r.file_name, r.amount, r.currency, r.status, r.what, r.paid_at, r.created_at`

func scanReceipt(row interface{ Scan(...any) error }) (Receipt, error) {
	var r Receipt
	err := row.Scan(&r.ID, &r.ChatID, &r.MsgID, &r.ChatTitle, &r.Channel, &r.FileName,
		&r.Amount, &r.Currency, &r.Status, &r.What, &r.PaidAt, &r.CreatedAt)
	return r, err
}

// SaveReceipt отмечает сообщение как чек или обновляет уже отмеченный.
// created=true — чек новый (а не правка).
func (s *Store) SaveReceipt(ctx context.Context, r Receipt) (Receipt, bool, error) {
	if !receiptStatuses[r.Status] {
		r.Status = "paid"
	}
	if r.Amount < 0 {
		return r, false, errors.New("сумма не может быть меньше нуля")
	}
	c, err := s.Chat(ctx, r.ChatID)
	if err != nil {
		return r, false, errors.New("клиент не найден")
	}
	m, err := s.MessageByMsg(ctx, r.ChatID, r.MsgID)
	if err != nil {
		return r, false, errors.New("сообщение не найдено")
	}
	if r.FileName == "" {
		r.FileName = m.FileName
		if r.FileName == "" && m.MediaKind == "photo" {
			r.FileName = "Фото"
		}
	}
	if r.PaidAt == 0 {
		r.PaidAt = m.Date
	}
	if strings.TrimSpace(r.Currency) == "" {
		r.Currency = "₽"
	}
	var existed int
	_ = s.db.QueryRowContext(ctx, `SELECT 1 FROM receipts WHERE chat_id=? AND msg_id=?`, r.ChatID, r.MsgID).Scan(&existed)
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `INSERT INTO receipts(chat_id,msg_id,chat_title,channel,file_name,amount,currency,status,what,paid_at,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(chat_id,msg_id) DO UPDATE SET chat_title=excluded.chat_title, channel=excluded.channel,
  file_name=excluded.file_name, amount=excluded.amount, currency=excluded.currency, status=excluded.status,
  what=excluded.what, paid_at=excluded.paid_at`,
		r.ChatID, r.MsgID, c.Title, c.Channel, r.FileName, r.Amount, r.Currency, r.Status, strings.TrimSpace(r.What), r.PaidAt, now)
	if err != nil {
		return r, false, err
	}
	saved, err := s.ReceiptByMsg(ctx, r.ChatID, r.MsgID)
	return saved, existed == 0, err
}

func (s *Store) ReceiptByMsg(ctx context.Context, chatID, msgID int64) (Receipt, error) {
	return scanReceipt(s.db.QueryRowContext(ctx, `SELECT `+receiptCols+`
FROM receipts r LEFT JOIN chats c ON c.id=r.chat_id WHERE r.chat_id=? AND r.msg_id=?`, chatID, msgID))
}

// DeleteReceipt снимает отметку «чек». Файл и переписка не меняются.
func (s *Store) DeleteReceipt(ctx context.Context, chatID, msgID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM receipts WHERE chat_id=? AND msg_id=?`, chatID, msgID)
	return err
}

// Receipts — чеки, новые сверху. chatID=0 — все клиенты; from=0 — за всё время.
func (s *Store) Receipts(ctx context.Context, chatID, from int64) ([]Receipt, error) {
	q := `SELECT ` + receiptCols + ` FROM receipts r LEFT JOIN chats c ON c.id=r.chat_id WHERE r.paid_at>=?`
	args := []any{from}
	if chatID != 0 {
		q += ` AND r.chat_id=?`
		args = append(args, chatID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY r.paid_at DESC, r.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Receipt{}
	for rows.Next() {
		r, err := scanReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PaidSum — сумма оплаченных чеков клиента.
func (s *Store) PaidSum(ctx context.Context, chatID int64) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT SUM(amount) FROM receipts WHERE chat_id=? AND status='paid'`, chatID).Scan(&n)
	return n.Int64, err
}
