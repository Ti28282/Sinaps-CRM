package store

import (
	"context"
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
	pdf := `{"name":"pp_418.pdf","bytes":1000}`
	for _, m := range []Message{
		{ChatID: id, MsgID: 1, Text: "привет", Date: 100},
		{ChatID: id, MsgID: 2, Text: "[файл]", MediaKind: "file", MediaRef: pdf, Date: 200},
	} {
		if _, err := s.AddMessage(ctx, m, false); err != nil {
			t.Fatal(err)
		}
	}

	r, created, err := s.SaveReceipt(ctx, Receipt{ChatID: id, MsgID: 2, Amount: 25000, Status: "paid", What: "этап 2"})
	if err != nil || !created || r.FileName != "pp_418.pdf" || r.PaidAt != 200 || r.ChatTitle != "Мед-Лайн" {
		t.Fatalf("save: %v %v %+v", err, created, r)
	}
	if _, created, _ = s.SaveReceipt(ctx, Receipt{ChatID: id, MsgID: 2, Amount: 30000, Status: "paid"}); created {
		t.Fatal("second save must update")
	}
	if sum, _ := s.PaidSum(ctx, id); sum != 30000 {
		t.Fatalf("sum %d", sum)
	}

	// убрать сообщение клиента из CRM: синхронизация не возвращает, чек остаётся
	if err := s.HideMessage(ctx, id, 2); err != nil {
		t.Fatal(err)
	}
	if ins, _ := s.AddMessage(ctx, Message{ChatID: id, MsgID: 2, Text: "[файл]", Date: 200}, false); ins {
		t.Fatal("hidden message came back")
	}
	if ms, _ := s.Messages(ctx, id, 0); len(ms) != 1 {
		t.Fatalf("messages: %d", len(ms))
	}
	if rs, _ := s.Receipts(ctx, 0, 0); len(rs) != 1 || rs[0].Amount != 30000 {
		t.Fatalf("receipts: %+v", rs)
	}
	// клиента убрали из CRM — чек виден с сохранённым названием
	if _, err := s.HideChats(ctx, "tg", map[string]string{"u7": "Мед-Лайн"}); err != nil {
		t.Fatal(err)
	}
	if rs, _ := s.Receipts(ctx, 0, 0); len(rs) != 1 || rs[0].ChatTitle != "Мед-Лайн" {
		t.Fatalf("receipts after hide chat: %+v", rs)
	}
	if err := s.DeleteReceipt(ctx, id, 2); err != nil {
		t.Fatal(err)
	}
	if rs, _ := s.Receipts(ctx, 0, 0); len(rs) != 0 {
		t.Fatal("not deleted")
	}
}
