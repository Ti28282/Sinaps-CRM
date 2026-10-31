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

func TestRemoteDelete(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := s.UpsertChat(ctx, Chat{Channel: "tg", ExtID: "u7", Kind: "user", PeerID: 7, Title: "Клиент"})
	ch, _, _ := s.UpsertChat(ctx, Chat{Channel: "tg", ExtID: "ch9", Kind: "channel", PeerID: 9, Title: "Группа"})
	_, _ = s.AddMessage(ctx, Message{ChatID: id, MsgID: 1, Out: false, Text: "вопрос", Date: 1}, false)
	_, _ = s.AddMessage(ctx, Message{ChatID: id, MsgID: 2, Out: true, Text: "ответ", Date: 2}, false)
	_, _ = s.AddMessage(ctx, Message{ChatID: id, MsgID: 3, Out: true, MediaKind: "photo", MediaRef: `{"id":1}`, Date: 3}, false)
	_, _ = s.AddMessage(ctx, Message{ChatID: ch, MsgID: 2, Out: true, Text: "в группе", Date: 2}, false)

	c, _ := s.Chat(ctx, id)
	if c.LastMsg != "Фото" {
		t.Fatalf("preview %q", c.LastMsg)
	}
	// удалили в личке id 1 (клиента), 2 и 3 (мои); в канале с тем же id 2 ничего не трогаем
	res, err := s.ApplyRemoteDelete(ctx, "", []int{1, 2, 3})
	if err != nil || len(res) != 1 || len(res[0].Removed) != 2 || len(res[0].Marked) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	ms, _ := s.Messages(ctx, id, 0)
	if len(ms) != 1 || !ms[0].Deleted || ms[0].Text != "вопрос" {
		t.Fatalf("%+v", ms)
	}
	c, _ = s.Chat(ctx, id)
	if c.LastMsg != "вопрос" || c.LastOut {
		t.Fatalf("last %+v", c)
	}
	if g, _ := s.Messages(ctx, ch, 0); len(g) != 1 {
		t.Fatal("channel message touched")
	}
	// старое «[фото]» дополняется ссылкой при повторном импорте
	_, _ = s.AddMessage(ctx, Message{ChatID: ch, MsgID: 5, Text: "[фото]", Date: 5}, false)
	_, _ = s.AddMessage(ctx, Message{ChatID: ch, MsgID: 5, MediaKind: "photo", MediaRef: `{"id":2}`, Date: 5}, false)
	m, _ := s.MessageByMsg(ctx, ch, 5)
	if m.MediaKind != "photo" || m.Text != "" {
		t.Fatalf("%+v", m)
	}
}

func TestHideChats(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := s.UpsertChat(ctx, Chat{Channel: "tg", ExtID: "u1", Kind: "user", PeerID: 1, Title: "А"})
	_, _ = s.AddMessage(ctx, Message{ChatID: id, MsgID: 1, Text: "x", Date: 1}, false)
	n, err := s.HideChats(ctx, "tg", map[string]string{"u1": "А", "u2": "Б"})
	if err != nil || n != 1 {
		t.Fatal(err, n)
	}
	if h := s.HiddenSet(ctx, "tg"); !h["u1"] || !h["u2"] {
		t.Fatal(h)
	}
	if list, _ := s.Chats(ctx, "tg", ""); len(list) != 0 {
		t.Fatal("chat not removed")
	}
	_ = s.UnhideChats(ctx, "tg", []string{"u1"})
	if h := s.HiddenSet(ctx, "tg"); h["u1"] || !h["u2"] {
		t.Fatal(h)
	}
}
