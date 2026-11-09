package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestReceiptsAndHide(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := s.UpsertChat(ctx, Chat{Channel: "tg", ExtID: "u7", Kind: "user", PeerID: 7, Title: "Мед-Лайн"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMessage(ctx, Message{ChatID: id, MsgID: 1, Text: "привет", Date: 100}, false); err != nil {
		t.Fatal(err)
	}

	// чек из частей: 5 000 в счёт проекта + 2 200 правки = 7 200
	r1, err := s.CreateReceipt(ctx, Receipt{ChatID: id, Status: "paid", PaidAt: 200, Parts: []ReceiptPart{
		{Kind: PartProject, What: " Работа с проектом ", Amount: 5000},
		{Kind: "edits", What: "Правки за сайт", Amount: 2200},
		{}, // пустая строка формы пропускается
	}})
	if err != nil || r1.Amount != 7200 || len(r1.Parts) != 2 || r1.Parts[0].What != "Работа с проектом" || r1.ChatTitle != "Мед-Лайн" || r1.Currency != "₽" {
		t.Fatalf("create: %v %+v", err, r1)
	}
	if _, err := s.CreateReceipt(ctx, Receipt{ChatID: id, Status: "wait", PaidAt: 300, Parts: []ReceiptPart{{Amount: 10000}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateReceipt(ctx, Receipt{ChatID: id, Parts: nil}); err == nil {
		t.Fatal("receipt without parts must fail")
	}
	if _, err := s.CreateReceipt(ctx, Receipt{ChatID: 999, Parts: []ReceiptPart{{Amount: 1}}}); err == nil {
		t.Fatal("unknown client must fail")
	}
	r1.Parts = []ReceiptPart{{Kind: PartProject, Amount: 6000}}
	if u, err := s.UpdateReceipt(ctx, r1); err != nil || u.Amount != 6000 || len(u.Parts) != 1 {
		t.Fatalf("update: %v %+v", err, u)
	}
	if sum, _ := s.PaidSum(ctx, id); sum != 6000 {
		t.Fatalf("sum %d", sum)
	}
	if err := s.SetProject(ctx, Project{ChatID: id, Name: " Бот записи ", Price: 67000}); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Project(ctx, id); p.Name != "Бот записи" || p.Price != 67000 {
		t.Fatalf("project %+v", p)
	}

	// убрать сообщение из CRM: синхронизация его не возвращает
	if err := s.HideMessage(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	if ins, _ := s.AddMessage(ctx, Message{ChatID: id, MsgID: 1, Text: "привет", Date: 100}, false); ins {
		t.Fatal("hidden message came back")
	}

	// клиента убрали из CRM — чеки видны с сохранённым названием
	if _, err := s.HideChats(ctx, "tg", map[string]string{"u7": "Мед-Лайн"}); err != nil {
		t.Fatal(err)
	}
	rs, _ := s.Receipts(ctx, 0, 0)
	if len(rs) != 2 || rs[0].ChatTitle != "Мед-Лайн" {
		t.Fatalf("receipts after hide chat: %+v", rs)
	}
	if err := s.DeleteReceipt(ctx, r1.ID); err != nil {
		t.Fatal(err)
	}
	if rs, _ := s.Receipts(ctx, 0, 0); len(rs) != 1 {
		t.Fatal("not deleted")
	}
}

// База 0.2.0: таблица чеков с UNIQUE(chat_id, msg_id) переделывается без потери чеков.
func TestMigrateReceiptsV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crm.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE receipts(
  id INTEGER PRIMARY KEY, chat_id INTEGER NOT NULL, msg_id INTEGER NOT NULL,
  chat_title TEXT NOT NULL DEFAULT '', channel TEXT NOT NULL DEFAULT '', file_name TEXT NOT NULL DEFAULT '',
  amount INTEGER NOT NULL DEFAULT 0, currency TEXT NOT NULL DEFAULT '₽', status TEXT NOT NULL DEFAULT 'paid',
  what TEXT NOT NULL DEFAULT '', paid_at INTEGER NOT NULL, created_at INTEGER NOT NULL,
  UNIQUE(chat_id, msg_id));
INSERT INTO receipts(chat_id,msg_id,chat_title,amount,paid_at,created_at) VALUES(1,5,'Старый',5000,10,10);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rs, _ := s.Receipts(ctx, 0, 0)
	if len(rs) != 1 || rs[0].ChatTitle != "Старый" || rs[0].Amount != 5000 || len(rs[0].Parts) != 1 || rs[0].Parts[0].Kind != PartProject {
		t.Fatalf("lost receipts: %+v", rs)
	}
	for i := 0; i < 2; i++ { // два чека без сообщения у одного клиента
		if _, err := s.db.Exec(`INSERT INTO receipts(chat_id,msg_id,amount,paid_at,created_at) VALUES(1,0,1,1,1)`); err != nil {
			t.Fatal("unique still there:", err)
		}
	}
}
