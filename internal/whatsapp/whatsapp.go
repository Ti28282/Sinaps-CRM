// Package whatsapp — подключение WhatsApp как связанного устройства (whatsmeow,
// протокол WhatsApp Web): вход по QR, выбор чатов вручную, история, живые сообщения,
// отправка текста и файлов, удаление своих сообщений.
//
// Что приложение НЕ делает само: не отправляет сообщений без нажатия «Отправить»,
// не ставит «прочитано» (синие галочки), не меняет статус «в сети», рассылок нет.
package whatsapp

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"sinaps-crm/internal/store"
)

const (
	StateOff        = "off"        // не подключали
	StateConnecting = "connecting" // соединяемся
	StateNeedQR     = "need_qr"    // показываем QR
	StateReady      = "ready"
	StateError      = "error"
)

// MaxUpload — лимит на отправляемый файл (как в Telegram-части).
const MaxUpload = 1536 << 20

// HistoryLimit — сколько сообщений истории брать в CRM при выборе чата.
const HistoryLimit = 200

type Emitter func(event string, data any)

type Status struct {
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
	QR      string `json:"qr,omitempty"` // содержимое QR-кода
	Name    string `json:"name,omitempty"`
	Phone   string `json:"phone,omitempty"`
	Known   int    `json:"known"` // сколько личных чатов знаем (для окна выбора)
	Syncing bool   `json:"syncing"`
}

type Client struct {
	st      *store.Store
	dataDir string
	emit    Emitter

	mu        sync.Mutex
	container *sqlstore.Container
	cli       *whatsmeow.Client
	status    Status
	qrCancel  context.CancelFunc
}

func New(st *store.Store, dataDir string, emit Emitter) *Client {
	return &Client{st: st, dataDir: dataDir, emit: emit, status: Status{State: StateOff}}
}

func (c *Client) sessionPath() string { return filepath.Join(c.dataDir, "wa-session.db") }

func (c *Client) setStatus(f func(*Status)) {
	c.mu.Lock()
	f(&c.status)
	s := c.status
	c.mu.Unlock()
	c.emit("wa:status", s)
}

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// HasSession — подключались ли раньше (тогда подключаемся при запуске сами).
func (c *Client) HasSession() bool {
	_, err := os.Stat(c.sessionPath())
	return err == nil
}

func (c *Client) client() (*whatsmeow.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cli == nil || !c.cli.IsLoggedIn() {
		return nil, errors.New("WhatsApp не подключён")
	}
	return c.cli, nil
}

// Start подключается. Без сохранённой сессии — выдаёт QR (статус need_qr).
func (c *Client) Start(ctx context.Context) error {
	c.Stop()
	c.setStatus(func(s *Status) { *s = Status{State: StateConnecting} })

	db, err := sql.Open("sqlite", "file:"+c.sessionPath()+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return c.fail(err)
	}
	container := sqlstore.NewWithDB(db, "sqlite", nil)
	if err := container.Upgrade(ctx); err != nil {
		_ = container.Close()
		return c.fail(fmt.Errorf("сессия WhatsApp: %w", err))
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = container.Close()
		return c.fail(err)
	}
	cli := whatsmeow.NewClient(device, nil)
	cli.AddEventHandler(c.onEvent)

	c.mu.Lock()
	c.container, c.cli = container, cli
	c.mu.Unlock()

	if cli.Store.ID == nil {
		qctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		c.mu.Lock()
		c.qrCancel = cancel
		c.mu.Unlock()
		qr, err := cli.GetQRChannel(qctx)
		if err != nil {
			cancel()
			return c.fail(err)
		}
		if err := cli.Connect(); err != nil {
			cancel()
			return c.fail(err)
		}
		go func() {
			defer cancel()
			for item := range qr {
				switch item.Event {
				case "code":
					code := item.Code
					c.setStatus(func(s *Status) { s.State, s.QR, s.Error = StateNeedQR, code, "" })
				case "success":
					c.setStatus(func(s *Status) { s.QR = "" })
					return // дальше — events.Connected
				case "timeout":
					c.Stop()
					c.setStatus(func(s *Status) {
						*s = Status{State: StateOff, Error: "QR-код не отсканировали за 5 минут — попробуйте ещё раз"}
					})
					return
				default:
					if item.Error != nil {
						c.setStatus(func(s *Status) { s.State, s.Error = StateError, item.Error.Error() })
						return
					}
				}
			}
		}()
		return nil
	}
	if err := cli.Connect(); err != nil {
		return c.fail(err)
	}
	return nil
}

func (c *Client) fail(err error) error {
	c.setStatus(func(s *Status) { s.State, s.Error, s.QR = StateError, err.Error(), "" })
	return err
}

func (c *Client) Stop() {
	c.mu.Lock()
	cli, container, cancel := c.cli, c.container, c.qrCancel
	c.cli, c.container, c.qrCancel = nil, nil, nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cli != nil {
		cli.Disconnect()
	}
	if container != nil {
		_ = container.Close()
	}
}

// Logout отвязывает устройство в WhatsApp и удаляет сессию с компьютера.
// Переписка в CRM остаётся.
func (c *Client) Logout(ctx context.Context) error {
	c.mu.Lock()
	cli := c.cli
	c.mu.Unlock()
	if cli != nil && cli.IsLoggedIn() {
		_ = cli.Logout(ctx)
	}
	c.Stop()
	for _, sfx := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(c.sessionPath() + sfx)
	}
	c.setStatus(func(s *Status) { *s = Status{State: StateOff} })
	return nil
}

func (c *Client) markReady() {
	c.mu.Lock()
	cli := c.cli
	c.mu.Unlock()
	if cli == nil || cli.Store.ID == nil {
		return
	}
	known, _ := c.st.WAKnownChats(context.Background())
	c.setStatus(func(s *Status) {
		s.State, s.Error, s.QR = StateReady, "", ""
		s.Name, s.Phone, s.Known = cli.Store.PushName, "+"+cli.Store.ID.User, len(known)
	})
}

// ---------- события ----------

func (c *Client) onEvent(raw any) {
	ctx := context.Background()
	switch evt := raw.(type) {
	case *events.Connected, *events.PushNameSetting:
		go c.refreshContacts(ctx)
		c.markReady()
	case *events.LoggedOut:
		c.Stop()
		for _, sfx := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(c.sessionPath() + sfx)
		}
		c.setStatus(func(s *Status) {
			*s = Status{State: StateOff, Error: "Устройство отвязано в телефоне — подключите заново"}
		})
	case *events.StreamReplaced:
		c.setStatus(func(s *Status) {
			s.State, s.Error = StateError, "WhatsApp открыт в другой копии SINAPS CRM"
		})
	case *events.Disconnected:
		c.setStatus(func(s *Status) { s.State = StateConnecting })
	case *events.HistorySync:
		c.onHistory(ctx, evt)
	case *events.Message:
		c.onMessage(ctx, evt)
	}
}

// refreshContacts — имена из адресной книги телефона для окна выбора чатов.
func (c *Client) refreshContacts(ctx context.Context) {
	c.mu.Lock()
	cli := c.cli
	c.mu.Unlock()
	if cli == nil {
		return
	}
	all, err := cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return
	}
	for jid, ci := range all {
		if jid.Server != types.DefaultUserServer {
			continue
		}
		if name := contactName(ci); name != "" {
			_ = c.st.WAUpsertKnown(ctx, store.WAKnown{JID: jid.ToNonAD().String(), Name: name})
		}
	}
}

func contactName(ci types.ContactInfo) string {
	for _, n := range []string{ci.FullName, ci.FirstName, ci.BusinessName, ci.PushName} {
		if strings.TrimSpace(n) != "" {
			return strings.TrimSpace(n)
		}
	}
	return ""
}

// chatJID приводит адрес лички к номеру телефона (новые чаты WhatsApp бывают на «скрытых» LID).
func (c *Client) chatJID(ctx context.Context, info types.MessageInfo) (types.JID, bool) {
	if info.IsGroup || info.Chat.Server == types.BroadcastServer || info.Chat.Server == types.NewsletterServer {
		return types.JID{}, false
	}
	j := info.Chat.ToNonAD()
	if j.Server == types.HiddenUserServer {
		alt := info.SenderAlt
		if info.IsFromMe {
			alt = info.RecipientAlt
		}
		if !alt.IsEmpty() && alt.Server == types.DefaultUserServer {
			return alt.ToNonAD(), true
		}
		c.mu.Lock()
		cli := c.cli
		c.mu.Unlock()
		if cli != nil {
			if pn, err := cli.Store.LIDs.GetPNForLID(ctx, j); err == nil && !pn.IsEmpty() {
				return pn.ToNonAD(), true
			}
		}
	}
	return j, j.Server == types.DefaultUserServer || j.Server == types.HiddenUserServer
}

func (c *Client) onHistory(ctx context.Context, evt *events.HistorySync) {
	c.mu.Lock()
	cli := c.cli
	c.mu.Unlock()
	if cli == nil || evt.Data == nil {
		return
	}
	c.setStatus(func(s *Status) { s.Syncing = true })
	defer func() {
		known, _ := c.st.WAKnownChats(ctx)
		c.setStatus(func(s *Status) { s.Syncing, s.Known = false, len(known) })
		c.emit("wa:known", nil)
	}()
	for _, conv := range evt.Data.GetConversations() {
		chat, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		var msgs []store.Message
		var key types.JID
		var lastAt int64
		var lastText string
		for _, hm := range conv.GetMessages() {
			m, err := cli.ParseWebMessage(chat, hm.GetMessage())
			if err != nil {
				continue
			}
			j, ok := c.chatJID(ctx, m.Info)
			if !ok {
				break // группа/канал — пропускаем весь разговор
			}
			key = j
			sm, ok := toStore(m)
			if !ok {
				continue
			}
			msgs = append(msgs, sm)
			if sm.Date > lastAt {
				lastAt, lastText = sm.Date, previewText(sm)
			}
		}
		if key.IsEmpty() {
			if chat.Server != types.DefaultUserServer {
				continue
			}
			key = chat.ToNonAD()
		}
		name := conv.GetDisplayName()
		if name == "" {
			name = conv.GetName()
		}
		_ = c.st.WAUpsertKnown(ctx, store.WAKnown{JID: key.String(), Name: name, LastAt: lastAt, LastMsg: lastText})
		_ = c.st.WACacheHistory(ctx, key.String(), msgs)
		// если чат уже выбран в CRM — сразу докидываем сообщения
		if ch, err := c.st.ChatByExt(ctx, "wa", key.String()); err == nil {
			for _, m := range msgs {
				m.ChatID = ch.ID
				_, _ = c.st.AddMessage(ctx, m, false)
			}
			c.emit("chats:changed", nil)
		}
	}
}

func (c *Client) onMessage(ctx context.Context, evt *events.Message) {
	j, ok := c.chatJID(ctx, evt.Info)
	if !ok {
		return
	}
	key := j.String()
	ch, err := c.st.ChatByExt(ctx, "wa", key)

	// удаление «у всех»
	if pm := evt.Message.GetProtocolMessage(); pm != nil {
		if pm.GetType() == waE2E.ProtocolMessage_REVOKE && err == nil {
			r, derr := c.st.ApplyExtDelete(ctx, ch.ID, pm.GetKey().GetID())
			if derr == nil && len(r.Removed)+len(r.Marked) > 0 {
				c.emit("message:deleted", r)
				c.emit("chats:changed", nil)
			}
		}
		return
	}

	sm, ok := toStore(evt)
	if !ok {
		return
	}
	name := ""
	if !evt.Info.IsFromMe {
		name = evt.Info.PushName
	}
	_ = c.st.WAUpsertKnown(ctx, store.WAKnown{JID: key, Name: name, LastAt: sm.Date, LastMsg: previewText(sm)})
	if err != nil {
		_ = c.st.WACacheHistory(ctx, key, []store.Message{sm}) // чат не выбран — копим на будущее
		return
	}
	sm.ChatID = ch.ID
	if ins, err := c.st.AddMessage(ctx, sm, true); err == nil && ins {
		c.emit("message:new", sm)
		c.emit("chats:changed", nil)
	}
}

// ---------- выбор чатов ----------

type KnownChat struct {
	store.WAKnown
	InCRM  bool `json:"inCrm"`
	Hidden bool `json:"hidden"`
}

func (c *Client) KnownChats(ctx context.Context) ([]KnownChat, error) {
	list, err := c.st.WAKnownChats(ctx)
	if err != nil {
		return nil, err
	}
	hidden := c.st.HiddenSet(ctx, "wa")
	out := make([]KnownChat, 0, len(list))
	for _, k := range list {
		if k.Name == "" {
			k.Name = "+" + strings.SplitN(k.JID, "@", 2)[0]
		}
		_, err := c.st.ChatByExt(ctx, "wa", k.JID)
		out = append(out, KnownChat{WAKnown: k, InCRM: err == nil, Hidden: hidden[k.JID]})
	}
	return out, nil
}

// AddChats добавляет выбранные чаты в CRM вместе с историей из кэша.
// В WhatsApp ничего не меняется.
func (c *Client) AddChats(ctx context.Context, jids []string) (added, messages int, err error) {
	known, _ := c.st.WAKnownChats(ctx)
	names := map[string]string{}
	for _, k := range known {
		names[k.JID] = k.Name
	}
	_ = c.st.UnhideChats(ctx, "wa", jids)
	for _, jid := range jids {
		j, perr := types.ParseJID(jid)
		if perr != nil {
			continue
		}
		title := names[jid]
		if title == "" {
			title = "+" + j.User
		}
		phone := ""
		if j.Server == types.DefaultUserServer {
			phone = j.User
		}
		id, isNew, uerr := c.st.UpsertChat(ctx, store.Chat{Channel: "wa", ExtID: jid, Kind: "user", Title: title, Phone: phone})
		if uerr != nil {
			return added, messages, uerr
		}
		if isNew {
			added++
		}
		hist, _ := c.st.WAHistory(ctx, jid, HistoryLimit)
		for _, m := range hist {
			m.ChatID = id
			if ins, _ := c.st.AddMessage(ctx, m, false); ins {
				messages++
			}
		}
	}
	c.emit("chats:changed", nil)
	return added, messages, nil
}

// ---------- отправка ----------

func (c *Client) target(chat store.Chat) (types.JID, error) {
	if chat.Channel != "wa" {
		return types.JID{}, errors.New("это не чат WhatsApp")
	}
	return types.ParseJID(chat.ExtID)
}

func (c *Client) Send(ctx context.Context, chat store.Chat, text string) error {
	cli, err := c.client()
	if err != nil {
		return err
	}
	to, err := c.target(chat)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("пустое сообщение")
	}
	resp, err := cli.SendMessage(ctx, to, &waE2E.Message{Conversation: proto.String(text)})
	if err != nil {
		return fmt.Errorf("WhatsApp: %w", err)
	}
	c.saveSent(ctx, chat, resp, store.Message{Text: text})
	return nil
}

func (c *Client) SendFile(ctx context.Context, chat store.Chat, path, caption string, asPhoto bool, progress func(done, total int64)) error {
	cli, err := c.client()
	if err != nil {
		return err
	}
	to, err := c.target(chat)
	if err != nil {
		return err
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("файл не найден: %w", err)
	}
	if st.IsDir() {
		return errors.New("папки отправлять нельзя — выберите файл")
	}
	if st.Size() > MaxUpload {
		return fmt.Errorf("файл больше 1,5 ГБ (%.1f ГБ)", float64(st.Size())/(1<<30))
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// большие файлы шифруются через временный файл, а не в памяти
	tmp, err := os.CreateTemp("", "sinaps-wa-*")
	if err != nil {
		return err
	}
	defer func() { tmp.Close(); os.Remove(tmp.Name()) }()

	name := filepath.Base(path)
	mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if mt == "" {
		mt = "application/octet-stream"
	}
	caption = strings.TrimSpace(caption)
	kind := whatsmeow.MediaDocument
	if asPhoto {
		kind = whatsmeow.MediaImage
	}
	if progress != nil {
		progress(0, st.Size())
	}
	up, err := cli.UploadReader(ctx, f, tmp, kind)
	if err != nil {
		return fmt.Errorf("WhatsApp: загрузка не удалась: %w", err)
	}
	if progress != nil {
		progress(st.Size(), st.Size())
	}
	var msg *waE2E.Message
	if asPhoto {
		msg = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(mt), Caption: optStr(caption),
		}}
	} else {
		msg = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(mt), FileName: proto.String(name), Title: proto.String(name), Caption: optStr(caption),
		}}
	}
	resp, err := cli.SendMessage(ctx, to, msg)
	if err != nil {
		return fmt.Errorf("WhatsApp: %w", err)
	}
	sm, _ := toStore(&events.Message{Info: types.MessageInfo{ID: resp.ID, Timestamp: resp.Timestamp, MessageSource: types.MessageSource{IsFromMe: true}}, Message: msg})
	c.saveSent(ctx, chat, resp, sm)
	return nil
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return proto.String(s)
}

func (c *Client) saveSent(ctx context.Context, chat store.Chat, resp whatsmeow.SendResponse, m store.Message) {
	m.ChatID, m.Out = chat.ID, true
	m.ExtMsgID, m.MsgID = resp.ID, store.MsgIDFromExt(resp.ID)
	m.Date = resp.Timestamp.Unix()
	if m.Date <= 0 {
		m.Date = time.Now().Unix()
	}
	m.FillFile()
	if ins, err := c.st.AddMessage(ctx, m, false); err == nil && ins {
		c.emit("message:new", m)
		c.emit("chats:changed", nil)
	}
}

// DeleteMessage — удалить своё сообщение у всех.
func (c *Client) DeleteMessage(ctx context.Context, chat store.Chat, msgID int64) error {
	cli, err := c.client()
	if err != nil {
		return err
	}
	to, err := c.target(chat)
	if err != nil {
		return err
	}
	m, err := c.st.MessageByMsg(ctx, chat.ID, msgID)
	if err != nil {
		return errors.New("сообщение не найдено")
	}
	if !m.Out {
		return errors.New("удалять можно только свои сообщения — переписка клиента сохраняется")
	}
	if m.ExtMsgID == "" {
		return errors.New("у сообщения нет ID WhatsApp")
	}
	if _, err := cli.SendMessage(ctx, to, cli.BuildRevoke(to, types.EmptyJID, m.ExtMsgID)); err != nil {
		return fmt.Errorf("WhatsApp: %w", err)
	}
	r, err := c.st.ApplyExtDelete(ctx, chat.ID, m.ExtMsgID)
	if err != nil {
		return err
	}
	c.emit("message:deleted", r)
	c.emit("chats:changed", nil)
	return nil
}

// ---------- вложения ----------

// mediaRef хранит зашифрованное описание файла (protobuf), по нему WhatsApp отдаёт файл.
type mediaRef struct {
	Kind  string `json:"wa"`              // image | document
	Proto string `json:"p"`               // base64(proto)
	Name  string `json:"name,omitempty"`  // для карточки файла
	Bytes int64  `json:"bytes,omitempty"` //
}

func (c *Client) DownloadMedia(ctx context.Context, m store.Message, path string) error {
	cli, err := c.client()
	if err != nil {
		return err
	}
	var r mediaRef
	if err := json.Unmarshal([]byte(m.MediaRef), &r); err != nil || r.Proto == "" {
		return errors.New("у сообщения нет вложения")
	}
	raw, err := base64.StdEncoding.DecodeString(r.Proto)
	if err != nil {
		return err
	}
	var dl whatsmeow.DownloadableMessage
	switch r.Kind {
	case "image":
		im := &waE2E.ImageMessage{}
		if err := proto.Unmarshal(raw, im); err != nil {
			return err
		}
		dl = im
	case "document":
		dm := &waE2E.DocumentMessage{}
		if err := proto.Unmarshal(raw, dm); err != nil {
			return err
		}
		dl = dm
	default:
		return errors.New("неизвестный тип вложения")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = cli.DownloadToFile(ctx, dl, f)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		if errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) || errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) {
			return errors.New("файл больше недоступен на серверах WhatsApp")
		}
		return fmt.Errorf("WhatsApp: %w", err)
	}
	return os.Rename(tmp, path)
}

// ---------- преобразование ----------

// toStore — сообщение WhatsApp в формат CRM. false — служебное, не показываем.
func toStore(evt *events.Message) (store.Message, bool) {
	msg := evt.Message
	if msg == nil {
		return store.Message{}, false
	}
	m := store.Message{
		Out: evt.Info.IsFromMe, Date: evt.Info.Timestamp.Unix(),
		ExtMsgID: evt.Info.ID, MsgID: store.MsgIDFromExt(evt.Info.ID),
	}
	switch {
	case msg.GetConversation() != "":
		m.Text = msg.GetConversation()
	case msg.GetExtendedTextMessage() != nil:
		m.Text = msg.GetExtendedTextMessage().GetText()
	case msg.GetImageMessage() != nil:
		im := msg.GetImageMessage()
		m.Text = im.GetCaption()
		if b, err := proto.Marshal(im); err == nil {
			ref, _ := json.Marshal(mediaRef{Kind: "image", Proto: base64.StdEncoding.EncodeToString(b)})
			m.MediaKind, m.MediaRef = "photo", string(ref)
		}
	case msg.GetDocumentMessage() != nil:
		dm := msg.GetDocumentMessage()
		m.Text = dm.GetCaption()
		name := dm.GetFileName()
		if name == "" {
			name = dm.GetTitle()
		}
		if name == "" {
			name = "файл"
		}
		if b, err := proto.Marshal(dm); err == nil {
			ref, _ := json.Marshal(mediaRef{Kind: "document", Proto: base64.StdEncoding.EncodeToString(b), Name: name, Bytes: int64(dm.GetFileLength())})
			m.MediaKind, m.MediaRef = "file", string(ref)
		}
	case msg.GetAudioMessage() != nil:
		m.Text = "[голосовое сообщение]"
	case msg.GetVideoMessage() != nil:
		m.Text = strings.TrimSpace("[видео] " + msg.GetVideoMessage().GetCaption())
	case msg.GetStickerMessage() != nil:
		m.Text = "[стикер]"
	case msg.GetContactMessage() != nil:
		m.Text = "[контакт] " + msg.GetContactMessage().GetDisplayName()
	case msg.GetLocationMessage() != nil:
		m.Text = "[геолокация]"
	case msg.GetReactionMessage() != nil, msg.GetProtocolMessage() != nil,
		msg.GetSenderKeyDistributionMessage() != nil && msg.GetConversation() == "":
		return store.Message{}, false
	default:
		return store.Message{}, false
	}
	m.Text = strings.TrimSpace(m.Text)
	m.FillFile()
	return m, true
}

func previewText(m store.Message) string {
	if m.Text != "" {
		return m.Text
	}
	switch m.MediaKind {
	case "photo":
		return "Фото"
	case "file":
		return "Файл"
	}
	return ""
}
