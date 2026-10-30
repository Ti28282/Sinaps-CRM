package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDedup(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatal(err)
	}
	c := Chat{Channel: "tg", ExtID: "u1", Kind: "user", PeerID: 1, AccessHash: 9, Title: "Иван Петров"}
	id, isNew, err := s.UpsertChat(ctx, c)
	if err != nil || !isNew {
		t.Fatal(err, isNew)
	}
	st := "deal"
	amt := int64(50000)
	if err := s.PatchChat(ctx, id, ChatPatch{Stage: &st, Amount: &amt}); err != nil {
		t.Fatal(err)
	}
	c.Title = "Иван П."
	id2, isNew2, err := s.UpsertChat(ctx, c)
	if err != nil || isNew2 || id2 != id {
		t.Fatal(err, isNew2, id2)
	}
	got, _ := s.Chat(ctx, id)
	if got.Stage != "deal" || got.Amount != 50000 || got.Title != "Иван П." {
		t.Fatalf("%+v", got)
	}
	for i := 0; i < 2; i++ {
		for m := 1; m <= 3; m++ {
			if _, err := s.AddMessage(ctx, Message{ChatID: id, MsgID: int64(m), Text: "hi", Date: int64(100 + m)}, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	ms, _ := s.Messages(ctx, id, 0)
	if len(ms) != 3 {
		t.Fatalf("dup: %d", len(ms))
	}
	// live reply moves sent->replied
	st = "sent"
	_ = s.PatchChat(ctx, id, ChatPatch{Stage: &st})
	ins, err := s.AddMessage(ctx, Message{ChatID: id, MsgID: 10, Text: "ответ", Date: 200}, true)
	if err != nil || !ins {
		t.Fatal(err)
	}
	got, _ = s.Chat(ctx, id)
	if got.Stage != "replied" || !got.Unread || got.LastMsg != "ответ" {
		t.Fatalf("%+v", got)
	}
	ev, _ := s.Events(ctx, id)
	if len(ev) < 3 {
		t.Fatalf("events %v", ev)
	}
	_ = s.LogSend(ctx, id, "broadcast")
	if s.SentToday(ctx, "broadcast") != 1 {
		t.Fatal("sends")
	}
	stt, _ := s.Stats(ctx)
	if stt.Total != 1 || stt.Unread != 1 {
		t.Fatalf("%+v", stt)
	}
	list, _ := s.Chats(ctx, "tg", "Иван")
	if len(list) != 1 {
		t.Fatal("search")
	}
}
