package whatsapp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"sinaps-crm/internal/store"
)

func msg(chat types.JID, id string, fromMe bool, m *waE2E.Message, at int64) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat, IsFromMe: fromMe},
			ID:            id, PushName: "Анна", Timestamp: time.Unix(at, 0),
		},
		Message: m,
	}
}

func TestFlow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "crm.db"))
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	c := New(st, dir, func(e string, _ any) { events = append(events, e) })
	jid := types.NewJID("79001234567", types.DefaultUserServer)

	// чат ещё не выбран — сообщение копится в кэше, чат появляется в списке
	c.onMessage(ctx, msg(jid, "A1", false, &waE2E.Message{Conversation: proto.String("Здравствуйте")}, 100))
	c.onMessage(ctx, msg(jid, "A2", true, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("Добрый день!")}}, 110))
	if list, _ := st.Chats(ctx, "wa", ""); len(list) != 0 {
		t.Fatal("chat added without selection")
	}
	known, _ := c.KnownChats(ctx)
	if len(known) != 1 || known[0].Name != "Анна" || known[0].InCRM {
		t.Fatalf("%+v", known)
	}

	// выбрали — подтянулась история
	added, n, err := c.AddChats(ctx, []string{jid.String()})
	if err != nil || added != 1 || n != 2 {
		t.Fatal(err, added, n)
	}
	ch, _ := st.ChatByExt(ctx, "wa", jid.String())
	if ch.Title != "Анна" || ch.Phone != "79001234567" || ch.LastMsg != "Добрый день!" {
		t.Fatalf("%+v", ch)
	}
	// повторный выбор не дублирует
	if _, n, _ := c.AddChats(ctx, []string{jid.String()}); n != 0 {
		t.Fatal("dup history", n)
	}

	// живое сообщение с фото
	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("прайс"), DirectPath: proto.String("/x"), MediaKey: []byte{1}}}
	c.onMessage(ctx, msg(jid, "A3", false, img, 120))
	ms, _ := st.Messages(ctx, ch.ID, 0)
	if len(ms) != 3 || ms[2].MediaKind != "photo" || ms[2].Text != "прайс" {
		t.Fatalf("%+v", ms)
	}
	ch, _ = st.ChatByExt(ctx, "wa", jid.String())
	if !ch.Unread {
		t.Fatal("not unread")
	}

	// клиент удалил своё — остаётся с пометкой; я удалил своё — пропадает
	revoke := func(id string) *waE2E.Message {
		return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: &waCommon.MessageKey{ID: proto.String(id)}}}
	}
	c.onMessage(ctx, msg(jid, "R1", false, revoke("A1"), 130))
	c.onMessage(ctx, msg(jid, "R2", true, revoke("A2"), 131))
	ms, _ = st.Messages(ctx, ch.ID, 0)
	if len(ms) != 2 || !ms[0].Deleted || ms[0].ExtMsgID != "A1" {
		t.Fatalf("%+v", ms)
	}

	// документ — карточка с именем и размером
	doc := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("КП.pdf"), FileLength: proto.Uint64(2048)}}
	sm, ok := toStore(msg(jid, "D1", false, doc, 140))
	if !ok || sm.MediaKind != "file" || sm.FileName != "КП.pdf" || sm.FileSize != 2048 {
		t.Fatalf("%+v", sm)
	}
	// реакции и служебные — не показываем
	if _, ok := toStore(msg(jid, "X", false, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}}, 150)); ok {
		t.Fatal("reaction stored")
	}
	// группы игнорируются
	g := types.NewJID("12345", types.GroupServer)
	ev := msg(g, "G1", false, &waE2E.Message{Conversation: proto.String("всем привет")}, 160)
	ev.Info.IsGroup = true
	c.onMessage(ctx, ev)
	if k, _ := c.KnownChats(ctx); len(k) != 1 {
		t.Fatal("group leaked into known chats")
	}
}
