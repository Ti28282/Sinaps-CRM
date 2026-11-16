package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSearch(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fts5: %v", s.fts)
	id, _, err := s.UpsertChat(ctx, Chat{Channel: "tg", ExtID: "u1", Kind: "user", PeerID: 1, Title: "Клиника «Мед-Лайн»"})
	if err != nil {
		t.Fatal(err)
	}
	for i, txt := range []string{"Счёт получили, оплатим сегодня", "Можно добавить запись к медсестре?", "Ёлка и ЕЖИК", "Отправил счёт на 25 000 ₽"} {
		if _, err := s.AddMessage(ctx, Message{ChatID: id, MsgID: int64(i + 1), Text: txt, Date: int64(100 + i)}, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddMessage(ctx, Message{ChatID: id, MsgID: 9, MediaKind: "file", MediaRef: `{"name":"Платёжка_ноябрь.pdf","bytes":2048}`, Date: 200}, false); err != nil {
		t.Fatal(err)
	}
	cases := map[string]int{"счет": 2, "СЧЁТ": 2, "мед": 1, "елка ежик": 1, "25 000": 1, "оплатим завтра": 0, "": 0}
	for q, want := range cases {
		hits, total, err := s.SearchMessages(ctx, Words(q), 10)
		if err != nil || total != want || len(hits) != want {
			t.Fatalf("%q: total %d hits %d err %v, want %d", q, total, len(hits), err, want)
		}
	}
	// без FTS5 (перебором) — те же результаты
	s.fts = false
	for q, want := range cases {
		if _, total, _ := s.SearchMessages(ctx, Words(q), 10); total != want {
			t.Fatalf("scan %q: %d, want %d", q, total, want)
		}
	}
	s.fts = true
	if h, _, _ := s.SearchMessages(ctx, Words("счет"), 10); h[0].MsgID != 4 || h[0].ChatTitle != "Клиника «Мед-Лайн»" {
		t.Fatalf("order/client %+v", h[0])
	}
	if f, n, _ := s.SearchFiles(ctx, Words("платежка"), 10); n != 1 || f[0].FileName != "Платёжка_ноябрь.pdf" {
		t.Fatalf("files %d %+v", n, f)
	}
	// индекс следит за правкой и удалением
	if _, err := s.db.Exec(`UPDATE messages SET text='привет' WHERE chat_id=? AND msg_id=1`, id); err != nil {
		t.Fatal(err)
	}
	if _, n, _ := s.SearchMessages(ctx, Words("счет"), 10); n != 1 {
		t.Fatalf("after update %d", n)
	}
	if _, err := s.db.Exec(`DELETE FROM messages WHERE chat_id=? AND msg_id=4`, id); err != nil {
		t.Fatal(err)
	}
	if _, n, _ := s.SearchMessages(ctx, Words("счет"), 10); n != 0 {
		t.Fatalf("after delete %d", n)
	}
	_ = s.AddEvent(ctx, id, "note", "Заметка: писать утром")
	if e, n, _ := s.SearchEvents(ctx, Words("утром"), 10); n != 1 || e[0].ChatTitle == "" {
		t.Fatalf("events %d %+v", n, e)
	}
	if ms, err := s.MessagesAround(ctx, id, 2, 30); err != nil || len(ms) == 0 {
		t.Fatalf("around %v %d", err, len(ms))
	}
	if ms, err := s.MessagesNear(ctx, id, 2, 1); err != nil || len(ms) != 3 || ms[1].MsgID != 2 {
		t.Fatalf("near %v %+v", err, ms)
	}
	if got := Snippet("раз два три четыре пять шесть семь восемь девять десять", Words("девять"), 20); got == "" || got[0:3] != "…" {
		t.Fatalf("snippet %q", got)
	}
}

// База из прошлой версии: индекс строится по уже сохранённым сообщениям.
func TestSearchRebuild(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "crm.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.fts {
		t.Skip("нет FTS5")
	}
	id, _, _ := s.UpsertChat(ctx, Chat{Channel: "tg", ExtID: "u1", Kind: "user", PeerID: 1, Title: "А"})
	_, _ = s.AddMessage(ctx, Message{ChatID: id, MsgID: 1, Text: "старое сообщение", Date: 1}, false)
	// как будто индекса не было: убираем его и метку
	for _, q := range []string{`DROP TRIGGER messages_fts_ai`, `DROP TRIGGER messages_fts_ad`, `DROP TRIGGER messages_fts_au`, `DROP TABLE messages_fts`, `DELETE FROM settings WHERE key='search_fts_v1'`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, n, _ := s.SearchMessages(ctx, Words("старое"), 10); n != 1 {
		t.Fatalf("rebuild %d", n)
	}
}
