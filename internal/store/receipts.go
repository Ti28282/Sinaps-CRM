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

// CreateReceipt — новый чек по клиенту из воронки (создаётся в плагине «Чеки»).
func (s *Store) CreateReceipt(ctx context.Context, r Receipt) (Receipt, error) {
	if err := checkReceipt(&r); err != nil {
		return r, err
	}
	c, err := s.Chat(ctx, r.ChatID)
	if err != nil {
		return r, errors.New("клиент не найден")
	}
	if r.PaidAt == 0 {
		r.PaidAt = time.Now().Unix()
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO receipts(chat_id,msg_id,chat_title,channel,file_name,amount,currency,status,what,paid_at,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ChatID, r.MsgID, c.Title, c.Channel, r.FileName, r.Amount, r.Currency, r.Status, r.What, r.PaidAt, time.Now().Unix())
	if err != nil {
		return r, err
	}
	id, _ := res.LastInsertId()
	return s.ReceiptByID(ctx, id)
}

// UpdateReceipt меняет сумму, статус, «за что» и дату. Клиента у чека не меняем.
func (s *Store) UpdateReceipt(ctx context.Context, r Receipt) (Receipt, error) {
	if err := checkReceipt(&r); err != nil {
		return r, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE receipts SET amount=?, currency=?, status=?, what=?, paid_at=? WHERE id=?`,
		r.Amount, r.Currency, r.Status, r.What, r.PaidAt, r.ID)
	if err != nil {
		return r, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r, errors.New("чек не найден")
	}
	return s.ReceiptByID(ctx, r.ID)
}

func checkReceipt(r *Receipt) error {
	if !receiptStatuses[r.Status] {
		r.Status = "paid"
	}
	if r.Amount < 0 {
		return errors.New("сумма не может быть меньше нуля")
	}
	if strings.TrimSpace(r.Currency) == "" {
		r.Currency = "₽"
	}
	r.What = strings.TrimSpace(r.What)
	return nil
}

func (s *Store) ReceiptByID(ctx context.Context, id int64) (Receipt, error) {
	r, err := scanReceipt(s.db.QueryRowContext(ctx, `SELECT `+receiptCols+`
FROM receipts r LEFT JOIN chats c ON c.id=r.chat_id WHERE r.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, errors.New("чек не найден")
	}
	return r, err
}

// DeleteReceipt удаляет чек. Переписка не меняется.
func (s *Store) DeleteReceipt(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM receipts WHERE id=?`, id)
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
