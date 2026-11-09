package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Плагин «Чеки».
//
// Чек — один платёж клиента (например 7 200 ₽). Он состоит из частей:
// «В счёт проекта» 5 000 уменьшает остаток по проекту клиента, «Правки» 2 200 — сверх проекта.
// Сумма чека всегда равна сумме частей.

// PartProject — тип части, которая идёт в оплату проекта клиента.
const PartProject = "project"

// ReceiptPart — часть чека.
type ReceiptPart struct {
	Kind   string `json:"kind"` // project | edits | site | support | c:<свой тип>
	What   string `json:"what"`
	Amount int64  `json:"amount"`
}

// Receipt — чек клиента.
type Receipt struct {
	ID        int64         `json:"id"`
	ChatID    int64         `json:"chatId"`
	MsgID     int64         `json:"msgId"`
	ChatTitle string        `json:"chatTitle"`
	Channel   string        `json:"channel"`
	FileName  string        `json:"fileName"`
	Amount    int64         `json:"amount"`
	Currency  string        `json:"currency"`
	Status    string        `json:"status"` // paid | wait | refund
	What      string        `json:"what"`   // комментарии частей одной строкой (для выгрузки)
	PaidAt    int64         `json:"paidAt"`
	CreatedAt int64         `json:"createdAt"`
	Parts     []ReceiptPart `json:"parts"`
}

// Project — проект клиента и его полная стоимость.
type Project struct {
	ChatID int64  `json:"chatId"`
	Name   string `json:"name"`
	Price  int64  `json:"price"`
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
	r.Parts = []ReceiptPart{}
	return r, err
}

// migrateReceiptParts: чеки до 0.4.0 были без частей — каждый становится одной частью «В счёт проекта».
func (s *Store) migrateReceiptParts() error {
	_, err := s.db.Exec(`INSERT INTO receipt_parts(receipt_id, kind, what, amount, pos)
SELECT r.id, 'project', r.what, r.amount, 0 FROM receipts r
WHERE NOT EXISTS (SELECT 1 FROM receipt_parts p WHERE p.receipt_id = r.id)`)
	return err
}

// cleanParts проверяет части и считает сумму чека.
func cleanParts(parts []ReceiptPart) ([]ReceiptPart, int64, string, error) {
	out := []ReceiptPart{}
	var sum int64
	var whats []string
	for _, p := range parts {
		p.Kind = strings.TrimSpace(p.Kind)
		p.What = strings.TrimSpace(p.What)
		if p.Amount < 0 {
			return nil, 0, "", errors.New("сумма части не может быть меньше нуля")
		}
		if p.Amount == 0 && p.What == "" {
			continue // пустая строка формы
		}
		if p.Kind == "" {
			p.Kind = PartProject
		}
		out = append(out, p)
		sum += p.Amount
		if p.What != "" {
			whats = append(whats, p.What)
		}
	}
	if len(out) == 0 {
		return nil, 0, "", errors.New("добавьте хотя бы одну часть чека")
	}
	return out, sum, strings.Join(whats, "; "), nil
}

// CreateReceipt — новый чек по клиенту из воронки.
func (s *Store) CreateReceipt(ctx context.Context, r Receipt) (Receipt, error) {
	parts, sum, what, err := cleanParts(r.Parts)
	if err != nil {
		return r, err
	}
	normReceipt(&r)
	c, err := s.Chat(ctx, r.ChatID)
	if err != nil {
		return r, errors.New("клиент не найден")
	}
	if r.PaidAt == 0 {
		r.PaidAt = time.Now().Unix()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO receipts(chat_id,msg_id,chat_title,channel,file_name,amount,currency,status,what,paid_at,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ChatID, r.MsgID, c.Title, c.Channel, r.FileName, sum, r.Currency, r.Status, what, r.PaidAt, time.Now().Unix())
	if err != nil {
		return r, err
	}
	id, _ := res.LastInsertId()
	if err := writeParts(ctx, tx, id, parts); err != nil {
		return r, err
	}
	if err := tx.Commit(); err != nil {
		return r, err
	}
	return s.ReceiptByID(ctx, id)
}

// UpdateReceipt меняет части, статус и дату. Клиента у чека не меняем.
func (s *Store) UpdateReceipt(ctx context.Context, r Receipt) (Receipt, error) {
	parts, sum, what, err := cleanParts(r.Parts)
	if err != nil {
		return r, err
	}
	normReceipt(&r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE receipts SET amount=?, status=?, what=?, paid_at=? WHERE id=?`,
		sum, r.Status, what, r.PaidAt, r.ID)
	if err != nil {
		return r, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r, errors.New("чек не найден")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM receipt_parts WHERE receipt_id=?`, r.ID); err != nil {
		return r, err
	}
	if err := writeParts(ctx, tx, r.ID, parts); err != nil {
		return r, err
	}
	if err := tx.Commit(); err != nil {
		return r, err
	}
	return s.ReceiptByID(ctx, r.ID)
}

func writeParts(ctx context.Context, tx *sql.Tx, id int64, parts []ReceiptPart) error {
	for i, p := range parts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO receipt_parts(receipt_id,kind,what,amount,pos) VALUES(?,?,?,?,?)`,
			id, p.Kind, p.What, p.Amount, i); err != nil {
			return err
		}
	}
	return nil
}

func normReceipt(r *Receipt) {
	if !receiptStatuses[r.Status] {
		r.Status = "paid"
	}
	if strings.TrimSpace(r.Currency) == "" {
		r.Currency = "₽"
	}
}

func (s *Store) ReceiptByID(ctx context.Context, id int64) (Receipt, error) {
	r, err := scanReceipt(s.db.QueryRowContext(ctx, `SELECT `+receiptCols+`
FROM receipts r LEFT JOIN chats c ON c.id=r.chat_id WHERE r.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, errors.New("чек не найден")
	}
	if err != nil {
		return r, err
	}
	list := []Receipt{r}
	if err := s.fillParts(ctx, list); err != nil {
		return r, err
	}
	return list[0], nil
}

// DeleteReceipt удаляет чек вместе с частями. Переписка не меняется.
func (s *Store) DeleteReceipt(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM receipt_parts WHERE receipt_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM receipts WHERE id=?`, id)
	return err
}

// Receipts — чеки с частями, новые сверху. chatID=0 — все клиенты; from=0 — за всё время.
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
	out := []Receipt{}
	for rows.Next() {
		r, err := scanReceipt(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.fillParts(ctx, out)
}

// fillParts подгружает части для списка чеков одним запросом.
func (s *Store) fillParts(ctx context.Context, list []Receipt) error {
	if len(list) == 0 {
		return nil
	}
	idx := make(map[int64]int, len(list))
	for i, r := range list {
		idx[r.ID] = i
	}
	rows, err := s.db.QueryContext(ctx, `SELECT receipt_id, kind, what, amount FROM receipt_parts ORDER BY receipt_id, pos, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var p ReceiptPart
		if err := rows.Scan(&id, &p.Kind, &p.What, &p.Amount); err != nil {
			return err
		}
		if i, ok := idx[id]; ok {
			list[i].Parts = append(list[i].Parts, p)
		}
	}
	return rows.Err()
}

// PaidSum — сумма оплаченных чеков клиента.
func (s *Store) PaidSum(ctx context.Context, chatID int64) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT SUM(amount) FROM receipts WHERE chat_id=? AND status='paid'`, chatID).Scan(&n)
	return n.Int64, err
}

// ---------- проект клиента ----------

func (s *Store) Project(ctx context.Context, chatID int64) (Project, error) {
	p := Project{ChatID: chatID}
	err := s.db.QueryRowContext(ctx, `SELECT name, price FROM receipt_projects WHERE chat_id=?`, chatID).Scan(&p.Name, &p.Price)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	return p, err
}

func (s *Store) SetProject(ctx context.Context, p Project) error {
	if p.Price < 0 {
		return errors.New("стоимость проекта не может быть меньше нуля")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO receipt_projects(chat_id,name,price,updated_at) VALUES(?,?,?,?)
ON CONFLICT(chat_id) DO UPDATE SET name=excluded.name, price=excluded.price, updated_at=excluded.updated_at`,
		p.ChatID, strings.TrimSpace(p.Name), p.Price, time.Now().Unix())
	return err
}
