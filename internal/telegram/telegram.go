// Package telegram — подключение личного аккаунта через MTProto (gotd/td):
// вход по номеру, чтение папок, импорт истории, живые сообщения, отправка.
package telegram

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	gotg "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"sinaps-crm/internal/store"
)

// Состояния подключения — их же показывает экран «Подключения».
const (
	StateNoKeys       = "no_keys"
	StateConnecting   = "connecting"
	StateNeedPhone    = "need_phone"
	StateNeedCode     = "need_code"
	StateNeedQR       = "need_qr"
	StateNeedPassword = "need_password"
	StateReady        = "ready"
	StateError        = "error"
)

type Emitter func(event string, data any)

type Status struct {
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
	Phone    string `json:"phone,omitempty"`
	CodeVia  string `json:"codeVia,omitempty"` // куда ушёл код
	NextVia  string `json:"nextVia,omitempty"` // куда уйдёт при повторной отправке
	Timeout  int    `json:"timeout,omitempty"` // через сколько секунд можно запросить снова
	QRURL    string `json:"qrUrl,omitempty"`   // tg://login?token=… для QR-входа
	QRExpire int64  `json:"qrExpire,omitempty"`
}

type Folder struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Count    int    `json:"count"`
	HasRules bool   `json:"hasRules"` // папка собрана по правилам («все контакты» и т.п.)
}

type SyncResult struct {
	Chats       int `json:"chats"`
	NewChats    int `json:"newChats"`
	NewMessages int `json:"newMessages"`
	Skipped     int `json:"skipped"`
	Hidden      int `json:"hidden"`
}

type Client struct {
	st      *store.Store
	dataDir string
	emit    Emitter

	mu       sync.Mutex
	client   *gotg.Client
	api      *tg.Client
	cancel   context.CancelFunc
	status   Status
	phone    string
	codeHash string
	loggedIn qrlogin.LoggedIn
	qrCancel context.CancelFunc
	syncing  bool
}

func New(st *store.Store, dataDir string, emit Emitter) *Client {
	return &Client{st: st, dataDir: dataDir, emit: emit, status: Status{State: StateNoKeys}}
}

func (c *Client) sessionPath() string { return filepath.Join(c.dataDir, "tg-session.json") }

func (c *Client) setStatus(s Status) {
	c.mu.Lock()
	c.status = s
	c.mu.Unlock()
	c.emit("tg:status", s)
}

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Client) apiClient() (*tg.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.api == nil {
		return nil, errors.New("Telegram не подключён")
	}
	return c.api, nil
}

// Start поднимает MTProto-соединение. Если сессия сохранена — сразу готов к работе.
func (c *Client) Start(apiID int, apiHash string) error {
	c.Stop()
	if apiID == 0 || apiHash == "" {
		c.setStatus(Status{State: StateNoKeys})
		return errors.New("укажите API ID и API Hash")
	}

	d := tg.NewUpdateDispatcher()
	loggedIn := qrlogin.OnLoginToken(d) // сигнал «QR подтверждён на телефоне»
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		c.onMessage(ctx, u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		c.onMessage(ctx, u.Message)
		return nil
	})
	// удаления: личка и группы приходят без чата (id сообщений общие на аккаунт)
	d.OnDeleteMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteMessages) error {
		c.onDelete(ctx, "", u.Messages)
		return nil
	})
	d.OnDeleteChannelMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		c.onDelete(ctx, fmt.Sprintf("ch%d", u.ChannelID), u.Messages)
		return nil
	})

	client := gotg.NewClient(apiID, apiHash, gotg.Options{
		SessionStorage: &session.FileStorage{Path: c.sessionPath()},
		UpdateHandler:  updateHandler{c: c, d: d},
		Device: gotg.DeviceConfig{
			DeviceModel:   "SINAPS CRM",
			SystemVersion: goruntime.GOOS,
			AppVersion:    "0.1.0",
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.client, c.cancel, c.loggedIn = client, cancel, loggedIn
	c.mu.Unlock()
	c.setStatus(Status{State: StateConnecting})

	go func() {
		err := client.Run(ctx, func(ctx context.Context) error {
			c.mu.Lock()
			c.api = client.API()
			c.mu.Unlock()

			st, err := client.Auth().Status(ctx)
			if err != nil {
				return err
			}
			if st.Authorized {
				c.onAuthorized(ctx, st.User)
			} else {
				c.setStatus(Status{State: StateNeedPhone})
			}
			<-ctx.Done()
			return ctx.Err()
		})
		c.mu.Lock()
		c.api = nil
		c.mu.Unlock()
		if err != nil && !errors.Is(err, context.Canceled) {
			c.setStatus(Status{State: StateError, Error: err.Error()})
		}
	}()
	return nil
}

func (c *Client) Stop() {
	c.mu.Lock()
	cancel := c.cancel
	if c.qrCancel != nil {
		c.qrCancel()
		c.qrCancel = nil
	}
	c.cancel, c.client, c.api = nil, nil, nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Client) onAuthorized(ctx context.Context, u *tg.User) {
	// без этого запроса Telegram не шлёт обновления в новую сессию
	if api, err := c.apiClient(); err == nil {
		_, _ = api.UpdatesGetState(ctx)
	}
	s := Status{State: StateReady}
	if u != nil {
		s.Name = strings.TrimSpace(u.FirstName + " " + u.LastName)
		s.Username = u.Username
		s.Phone = u.Phone
	}
	c.setStatus(s)
}

// ---------- вход ----------

func (c *Client) authClient() (*auth.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil || c.api == nil {
		return nil, errors.New("нет соединения с Telegram, подождите пару секунд")
	}
	return c.client.Auth(), nil
}

func (c *Client) SendCode(ctx context.Context, phone string) error {
	a, err := c.authClient()
	if err != nil {
		return err
	}
	phone = strings.ReplaceAll(strings.TrimSpace(phone), " ", "")
	sent, err := a.SendCode(ctx, phone, auth.SendCodeOptions{})
	if err != nil {
		return humanErr(err)
	}
	return c.onSentCode(ctx, phone, sent)
}

// ResendCode — повторная отправка; Telegram сам выбирает следующий способ (SMS, звонок).
func (c *Client) ResendCode(ctx context.Context) error {
	api, err := c.apiClient()
	if err != nil {
		return err
	}
	c.mu.Lock()
	phone, hash := c.phone, c.codeHash
	c.mu.Unlock()
	if hash == "" {
		return errors.New("сначала запросите код")
	}
	sent, err := api.AuthResendCode(ctx, &tg.AuthResendCodeRequest{PhoneNumber: phone, PhoneCodeHash: hash})
	if err != nil {
		return humanErr(err)
	}
	return c.onSentCode(ctx, phone, sent)
}

func (c *Client) onSentCode(ctx context.Context, phone string, sent tg.AuthSentCodeClass) error {
	switch s := sent.(type) {
	case *tg.AuthSentCode:
		c.mu.Lock()
		c.phone, c.codeHash = phone, s.PhoneCodeHash
		c.mu.Unlock()
		if _, ok := s.Type.(*tg.AuthSentCodeTypeSetUpEmailRequired); ok {
			return errors.New("Telegram требует привязать почту — войдите по QR-коду")
		}
		st := Status{State: StateNeedCode, Phone: phone, CodeVia: codeVia(s.Type), Timeout: s.Timeout}
		if s.NextType != nil {
			st.NextVia = nextVia(s.NextType)
		}
		c.setStatus(st)
	case *tg.AuthSentCodeSuccess:
		c.finishAuth(ctx)
	default:
		return fmt.Errorf("неожиданный ответ Telegram: %T", sent)
	}
	return nil
}

func codeVia(t tg.AuthSentCodeTypeClass) string {
	switch t.(type) {
	case *tg.AuthSentCodeTypeApp:
		return "app"
	case *tg.AuthSentCodeTypeSMS, *tg.AuthSentCodeTypeSMSWord, *tg.AuthSentCodeTypeSMSPhrase:
		return "sms"
	case *tg.AuthSentCodeTypeCall, *tg.AuthSentCodeTypeFlashCall, *tg.AuthSentCodeTypeMissedCall:
		return "call"
	case *tg.AuthSentCodeTypeEmailCode:
		return "email"
	case *tg.AuthSentCodeTypeFragmentSMS:
		return "fragment"
	case *tg.AuthSentCodeTypeFirebaseSMS:
		return "firebase"
	}
	return "unknown"
}

func nextVia(t tg.AuthCodeTypeClass) string {
	switch t.(type) {
	case *tg.AuthCodeTypeSMS:
		return "sms"
	case *tg.AuthCodeTypeCall, *tg.AuthCodeTypeFlashCall, *tg.AuthCodeTypeMissedCall:
		return "call"
	case *tg.AuthCodeTypeFragmentSMS:
		return "fragment"
	}
	return ""
}

// ---------- вход по QR ----------

// StartQR показывает QR (событие tg:status с QRURL) и ждёт подтверждения в
// приложении: Настройки → Устройства → Подключить устройство. Код не нужен.
func (c *Client) StartQR() error {
	c.mu.Lock()
	client, loggedIn := c.client, c.loggedIn
	if client == nil || c.api == nil {
		c.mu.Unlock()
		return errors.New("нет соединения с Telegram, подождите пару секунд")
	}
	if c.qrCancel != nil {
		c.qrCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	c.qrCancel = cancel
	c.mu.Unlock()

	go func() {
		defer cancel()
		_, err := client.QR().Auth(ctx, loggedIn, func(ctx context.Context, t qrlogin.Token) error {
			c.setStatus(Status{State: StateNeedQR, QRURL: t.URL(), QRExpire: t.Expires().Unix()})
			return nil
		})
		switch {
		case err == nil:
			c.finishAuth(context.Background())
		case tgerr.Is(err, "SESSION_PASSWORD_NEEDED"):
			c.setStatus(Status{State: StateNeedPassword})
		case errors.Is(err, context.Canceled):
			// отменили — экран уже сменился
		case errors.Is(err, context.DeadlineExceeded):
			c.setStatus(Status{State: StateNeedPhone, Error: "QR-код не подтвердили за 5 минут"})
		default:
			c.setStatus(Status{State: StateNeedPhone, Error: humanErr(err).Error()})
		}
	}()
	return nil
}

func (c *Client) CancelQR() {
	c.mu.Lock()
	if c.qrCancel != nil {
		c.qrCancel()
		c.qrCancel = nil
	}
	c.mu.Unlock()
	c.setStatus(Status{State: StateNeedPhone})
}

func (c *Client) SignIn(ctx context.Context, code string) error {
	a, err := c.authClient()
	if err != nil {
		return err
	}
	c.mu.Lock()
	phone, hash := c.phone, c.codeHash
	c.mu.Unlock()
	_, err = a.SignIn(ctx, phone, strings.TrimSpace(code), hash)
	if errors.Is(err, auth.ErrPasswordAuthNeeded) {
		c.setStatus(Status{State: StateNeedPassword, Phone: phone})
		return nil
	}
	if err != nil {
		return humanErr(err)
	}
	c.finishAuth(ctx)
	return nil
}

func (c *Client) Password(ctx context.Context, password string) error {
	a, err := c.authClient()
	if err != nil {
		return err
	}
	if _, err := a.Password(ctx, password); err != nil {
		return humanErr(err)
	}
	c.finishAuth(ctx)
	return nil
}

func (c *Client) finishAuth(ctx context.Context) {
	a, err := c.authClient()
	if err != nil {
		return
	}
	st, err := a.Status(ctx)
	if err != nil {
		c.setStatus(Status{State: StateError, Error: err.Error()})
		return
	}
	c.onAuthorized(ctx, st.User)
}

func (c *Client) Logout(ctx context.Context) error {
	if api, err := c.apiClient(); err == nil {
		_, _ = api.AuthLogOut(ctx)
	}
	c.Stop()
	_ = os.Remove(c.sessionPath())
	c.setStatus(Status{State: StateNoKeys})
	return nil
}

// ---------- папки и импорт ----------

func (c *Client) Folders(ctx context.Context) ([]Folder, error) {
	api, err := c.apiClient()
	if err != nil {
		return nil, err
	}
	res, err := api.MessagesGetDialogFilters(ctx)
	if err != nil {
		return nil, humanErr(err)
	}
	out := []Folder{}
	for _, f := range res.Filters {
		switch v := f.(type) {
		case *tg.DialogFilter:
			out = append(out, Folder{
				ID: v.ID, Title: v.Title.Text, Count: len(v.PinnedPeers) + len(v.IncludePeers),
				HasRules: v.Contacts || v.NonContacts || v.Groups || v.Broadcasts || v.Bots,
			})
		case *tg.DialogFilterChatlist:
			out = append(out, Folder{ID: v.ID, Title: v.Title.Text, Count: len(v.PinnedPeers) + len(v.IncludePeers)})
		}
	}
	return out, nil
}

// folderPeers возвращает все чаты папки — как их видит сам Telegram:
// вручную добавленные + подходящие под правила («Контакты», «Группы», «Боты»…),
// минус исключения («Без звука», «Прочитанные», «Архив», исключённые чаты).
// Peer'ы берутся из списка диалогов, поэтому access_hash всегда свежий.
func (c *Client) folderPeers(ctx context.Context, api *tg.Client, folderID int) ([]tg.InputPeerClass, error) {
	members, _, err := c.folderMembers(ctx, api, folderID)
	if err != nil {
		return nil, err
	}
	out := make([]tg.InputPeerClass, 0, len(members))
	for _, m := range members {
		out = append(out, m.peer)
	}
	return out, nil
}

type member struct {
	peer   tg.InputPeerClass
	key    string
	title  string
	byRule bool // попал по правилу папки, а не добавлен вручную
}

func (c *Client) folderMembers(ctx context.Context, api *tg.Client, folderID int) ([]member, tg.DialogFilterClass, error) {
	res, err := api.MessagesGetDialogFilters(ctx)
	if err != nil {
		return nil, nil, humanErr(err)
	}
	var f folderRules
	var raw tg.DialogFilterClass
	found := false
	for _, x := range res.Filters {
		switch v := x.(type) {
		case *tg.DialogFilter:
			if v.ID == folderID {
				f = folderRules{
					include: peerSet(v.PinnedPeers, v.IncludePeers), exclude: peerSet(v.ExcludePeers),
					contacts: v.Contacts, nonContacts: v.NonContacts, groups: v.Groups, broadcasts: v.Broadcasts, bots: v.Bots,
					exMuted: v.ExcludeMuted, exRead: v.ExcludeRead, exArchived: v.ExcludeArchived,
				}
				found, raw = true, v
			}
		case *tg.DialogFilterChatlist:
			if v.ID == folderID {
				f = folderRules{include: peerSet(v.PinnedPeers, v.IncludePeers), exclude: map[string]bool{}}
				found, raw = true, v
			}
		}
	}
	if !found {
		return nil, nil, errors.New("папка не найдена — возможно, её переименовали или удалили")
	}

	now := int(time.Now().Unix())
	seen := map[string]bool{}
	var out []member
	for _, archive := range []int{0, 1} { // основной список и архив
		err := query.GetDialogs(api).FolderID(archive).BatchSize(100).ForEach(ctx, func(ctx context.Context, e dialogs.Elem) error {
			d, ok := e.Dialog.(*tg.Dialog)
			if !ok {
				return nil
			}
			key := extFromPeer(d.Peer)
			if key == "" || seen[key] || !f.match(key, d, e, archive == 1, now) {
				return nil
			}
			seen[key] = true
			out = append(out, member{peer: e.Peer, key: key, title: elemTitle(d.Peer, e), byRule: !f.include[key]})
			return nil
		})
		if err != nil {
			if dur, ok := tgerr.AsFloodWait(err); ok {
				return nil, nil, fmt.Errorf("Telegram попросил подождать %d с — повторите позже", int(dur.Seconds()))
			}
			return nil, nil, humanErr(err)
		}
	}
	return out, raw, nil
}

func elemTitle(p tg.PeerClass, e dialogs.Elem) string {
	switch v := p.(type) {
	case *tg.PeerUser:
		if u, ok := e.Entities.User(v.UserID); ok {
			if t := strings.TrimSpace(u.FirstName + " " + u.LastName); t != "" {
				return t
			}
			if u.Username != "" {
				return "@" + u.Username
			}
			return "+" + u.Phone
		}
	case *tg.PeerChat:
		if ch, ok := e.Entities.Chat(v.ChatID); ok {
			return ch.Title
		}
	case *tg.PeerChannel:
		if ch, ok := e.Entities.Channel(v.ChannelID); ok {
			return ch.Title
		}
	}
	return extFromPeer(p)
}

// ---------- чистка папки ----------

type FolderChat struct {
	ExtID  string `json:"extId"`
	Title  string `json:"title"`
	ByRule bool   `json:"byRule"`
	InCRM  bool   `json:"inCrm"`
	Hidden bool   `json:"hidden"` // скрыт в CRM — при синхронизации пропускается
}

// FolderChats — все чаты папки с названиями (только чтение, папка в Telegram не меняется).
func (c *Client) FolderChats(ctx context.Context, folderID int) ([]FolderChat, error) {
	api, err := c.apiClient()
	if err != nil {
		return nil, err
	}
	members, _, err := c.folderMembers(ctx, api, folderID)
	if err != nil {
		return nil, err
	}
	hidden := c.st.HiddenSet(ctx, "tg")
	out := make([]FolderChat, 0, len(members))
	for _, m := range members {
		_, err := c.st.ChatByExt(ctx, "tg", m.key)
		out = append(out, FolderChat{ExtID: m.key, Title: m.title, ByRule: m.byRule, InCRM: err == nil, Hidden: hidden[m.key]})
	}
	return out, nil
}

type folderRules struct {
	include, exclude                                map[string]bool
	contacts, nonContacts, groups, broadcasts, bots bool
	exMuted, exRead, exArchived                     bool
}

func (f folderRules) match(key string, d *tg.Dialog, e dialogs.Elem, archived bool, now int) bool {
	if f.exclude[key] {
		return false
	}
	if f.include[key] {
		return true // вручную добавленные показываются всегда
	}
	cat := false
	switch p := d.Peer.(type) {
	case *tg.PeerUser:
		u, ok := e.Entities.User(p.UserID)
		if !ok || u.Self {
			return false
		}
		switch {
		case u.Bot:
			cat = f.bots
		case u.Contact:
			cat = f.contacts
		default:
			cat = f.nonContacts
		}
	case *tg.PeerChat:
		cat = f.groups
	case *tg.PeerChannel:
		if ch, ok := e.Entities.Channel(p.ChannelID); ok && ch.Broadcast {
			cat = f.broadcasts
		} else {
			cat = f.groups // супергруппы
		}
	}
	if !cat {
		return false
	}
	if f.exArchived && archived {
		return false
	}
	if f.exRead && d.UnreadCount == 0 && !d.UnreadMark {
		return false
	}
	if mu, ok := d.NotifySettings.GetMuteUntil(); f.exMuted && ok && mu > now {
		return false
	}
	return true
}

func peerSet(lists ...[]tg.InputPeerClass) map[string]bool {
	m := map[string]bool{}
	for _, l := range lists {
		for _, p := range l {
			if r, ok := peerRef(p); ok {
				m[r.ExtID] = true
			}
		}
	}
	return m
}

// Sync выгружает чаты папки и последние `limit` сообщений каждого. Дублей не будет.
func (c *Client) Sync(ctx context.Context, folderID, limit int) (SyncResult, error) {
	var res SyncResult
	c.mu.Lock()
	if c.syncing {
		c.mu.Unlock()
		return res, errors.New("синхронизация уже идёт")
	}
	c.syncing = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.syncing = false; c.mu.Unlock() }()

	api, err := c.apiClient()
	if err != nil {
		return res, err
	}
	peers, err := c.folderPeers(ctx, api, folderID)
	if err != nil {
		return res, err
	}
	if limit <= 0 {
		limit = 200
	}

	hidden := c.st.HiddenSet(ctx, "tg")
	for i, p := range peers {
		ref, ok := peerRef(p)
		if !ok {
			res.Skipped++
			continue
		}
		if hidden[ref.ExtID] {
			res.Hidden++ // скрыт пользователем в CRM — не импортируем
			continue
		}
		c.emit("sync:progress", map[string]any{"done": i, "total": len(peers)})

		msgs, users, chats, err := c.history(ctx, api, p, limit)
		if err != nil {
			res.Skipped++
			c.emit("sync:warn", fmt.Sprintf("Чат %s пропущен: %v", ref.ExtID, humanErr(err)))
			continue
		}
		ref.fill(users, chats)
		if ref.Title == "" {
			ref.resolveTitle(ctx, api)
		}

		id, isNew, err := c.st.UpsertChat(ctx, ref.Chat)
		if err != nil {
			return res, err
		}
		res.Chats++
		if isNew {
			res.NewChats++
		}
		for _, m := range msgs {
			ins, err := c.st.AddMessage(ctx, toStore(id, m), false)
			if err != nil {
				return res, err
			}
			if ins {
				res.NewMessages++
			}
		}
	}
	c.emit("sync:progress", map[string]any{"done": len(peers), "total": len(peers)})
	c.emit("chats:changed", nil)
	return res, nil
}

func (c *Client) history(ctx context.Context, api *tg.Client, p tg.InputPeerClass, limit int) ([]*tg.Message, []tg.UserClass, []tg.ChatClass, error) {
	var (
		out    []*tg.Message
		users  []tg.UserClass
		chats  []tg.ChatClass
		offset int
	)
	for len(out) < limit {
		page := limit - len(out)
		if page > 100 {
			page = 100
		}
		r, err := floodRetry(ctx, c.emit, func() (tg.MessagesMessagesClass, error) {
			return api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: p, OffsetID: offset, Limit: page})
		})
		if err != nil {
			return nil, nil, nil, err
		}
		mod, ok := r.AsModified()
		if !ok {
			break
		}
		users = append(users, mod.GetUsers()...)
		chats = append(chats, mod.GetChats()...)
		batch := mod.GetMessages()
		for _, mc := range batch {
			if m, ok := mc.(*tg.Message); ok {
				out = append(out, m)
				offset = m.ID
			} else if ms, ok := mc.(*tg.MessageService); ok {
				offset = ms.ID
			}
		}
		if len(batch) < page {
			break
		}
		time.Sleep(400 * time.Millisecond) // бережём лимиты
	}
	return out, users, chats, nil
}

// ---------- живые сообщения ----------

// updateHandler дополняет стандартный диспетчер: gotd не разбирает короткие
// обновления, а именно так Telegram присылает личные сообщения.
type updateHandler struct {
	c *Client
	d tg.UpdateDispatcher
}

func (h updateHandler) Handle(ctx context.Context, u tg.UpdatesClass) error {
	switch v := u.(type) {
	case *tg.UpdateShortMessage:
		h.c.onMessage(ctx, &tg.Message{ID: v.ID, Out: v.Out, Date: v.Date, Message: v.Message,
			PeerID: &tg.PeerUser{UserID: v.UserID}})
		return nil
	case *tg.UpdateShortChatMessage:
		h.c.onMessage(ctx, &tg.Message{ID: v.ID, Out: v.Out, Date: v.Date, Message: v.Message,
			PeerID: &tg.PeerChat{ChatID: v.ChatID}})
		return nil
	case *tg.UpdatesTooLong:
		h.c.emit("sync:suggest", nil) // пропустили много — фронт предложит синхронизировать
		return nil
	}
	return h.d.Handle(ctx, u)
}

func (c *Client) onMessage(ctx context.Context, mc tg.MessageClass) {
	m, ok := mc.(*tg.Message)
	if !ok {
		return
	}
	ext := extFromPeer(m.PeerID)
	if ext == "" {
		return
	}
	chat, err := c.st.ChatByExt(ctx, "tg", ext)
	if err != nil {
		return // чат не из папки клиентов — игнорируем
	}
	c.store(ctx, chat.ID, m, true)
}

func (c *Client) store(ctx context.Context, chatID int64, m *tg.Message, live bool) {
	msg := toStore(chatID, m)
	ins, err := c.st.AddMessage(ctx, msg, live)
	if err != nil || !ins {
		return
	}
	c.emit("message:new", msg)
	c.emit("chats:changed", nil)
}

// ---------- отправка ----------

func (c *Client) Send(ctx context.Context, chat store.Chat, text string) error {
	api, err := c.apiClient()
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("пустое сообщение")
	}
	peer, err := inputPeer(chat)
	if err != nil {
		return err
	}
	upd, err := api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer: peer, Message: text, RandomID: randomID(),
	})
	if err != nil {
		return humanErr(err)
	}
	id, date, _ := sentID(upd)
	if id == 0 {
		return nil // сообщение ушло; подтянется при следующей синхронизации
	}
	c.store(ctx, chat.ID, &tg.Message{ID: id, Out: true, Date: date, Message: text}, false)
	return nil
}

// MaxUpload — лимит на отправляемый файл.
const MaxUpload = 1536 << 20 // 1,5 ГБ

// SendFile загружает файл в Telegram и отправляет его в чат.
// asPhoto=true — как фото (Telegram сожмёт), иначе документом.
func (c *Client) SendFile(ctx context.Context, chat store.Chat, path, caption string, asPhoto bool, progress func(done, total int64)) error {
	api, err := c.apiClient()
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
	if st.Size() == 0 {
		return errors.New("файл пустой")
	}
	peer, err := inputPeer(chat)
	if err != nil {
		return err
	}

	up := uploader.NewUploader(api).WithProgress(uploadProgress(progress))
	file, err := up.FromPath(ctx, path)
	if err != nil {
		return humanErr(err)
	}
	name := filepath.Base(path)
	var media tg.InputMediaClass
	if asPhoto {
		media = &tg.InputMediaUploadedPhoto{File: file}
	} else {
		mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if mt == "" {
			mt = "application/octet-stream"
		}
		media = &tg.InputMediaUploadedDocument{
			File: file, MimeType: mt, ForceFile: true,
			Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: name}},
		}
	}
	upd, err := api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Peer: peer, Media: media, Message: strings.TrimSpace(caption), RandomID: randomID(),
	})
	if err != nil {
		if asPhoto && tgerr.Is(err, "PHOTO_INVALID_DIMENSIONS", "PHOTO_EXT_INVALID", "IMAGE_PROCESS_FAILED", "PHOTO_SAVE_FILE_INVALID") {
			return errors.New("Telegram не принял это фото — отправьте его без сжатия (файлом)")
		}
		return humanErr(err)
	}
	if _, _, m := sentID(upd); m != nil {
		c.store(ctx, chat.ID, m, false)
	}
	return nil
}

type uploadProgress func(done, total int64)

func (p uploadProgress) Chunk(ctx context.Context, s uploader.ProgressState) error {
	if p != nil {
		p(s.Uploaded, s.Total)
	}
	return nil
}

// IsImage — можно ли отправить файл как фото.
func IsImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	}
	return false
}

func sentID(u tg.UpdatesClass) (int, int, *tg.Message) {
	now := int(time.Now().Unix())
	var list []tg.UpdateClass
	switch v := u.(type) {
	case *tg.UpdateShortSentMessage:
		return v.ID, v.Date, nil
	case *tg.Updates:
		list = v.Updates
	case *tg.UpdatesCombined:
		list = v.Updates
	case *tg.UpdateShort:
		list = []tg.UpdateClass{v.Update}
	}
	id := 0
	for _, x := range list {
		switch v := x.(type) {
		case *tg.UpdateNewMessage:
			if m, ok := v.Message.(*tg.Message); ok {
				return m.ID, m.Date, m
			}
		case *tg.UpdateNewChannelMessage:
			if m, ok := v.Message.(*tg.Message); ok {
				return m.ID, m.Date, m
			}
		case *tg.UpdateMessageID:
			id = v.ID
		}
	}
	return id, now, nil
}

// ---------- peers ----------

type peer struct{ store.Chat }

func peerRef(p tg.InputPeerClass) (peer, bool) {
	switch v := p.(type) {
	case *tg.InputPeerUser:
		return peer{store.Chat{Channel: "tg", Kind: "user", PeerID: v.UserID, AccessHash: v.AccessHash, ExtID: fmt.Sprintf("u%d", v.UserID)}}, true
	case *tg.InputPeerChat:
		return peer{store.Chat{Channel: "tg", Kind: "chat", PeerID: v.ChatID, ExtID: fmt.Sprintf("c%d", v.ChatID)}}, true
	case *tg.InputPeerChannel:
		return peer{store.Chat{Channel: "tg", Kind: "channel", PeerID: v.ChannelID, AccessHash: v.AccessHash, ExtID: fmt.Sprintf("ch%d", v.ChannelID)}}, true
	}
	return peer{}, false // «Избранное» и прочее пропускаем
}

func extFromPeer(p tg.PeerClass) string {
	switch v := p.(type) {
	case *tg.PeerUser:
		return fmt.Sprintf("u%d", v.UserID)
	case *tg.PeerChat:
		return fmt.Sprintf("c%d", v.ChatID)
	case *tg.PeerChannel:
		return fmt.Sprintf("ch%d", v.ChannelID)
	}
	return ""
}

func inputPeer(c store.Chat) (tg.InputPeerClass, error) {
	switch c.Kind {
	case "user":
		return &tg.InputPeerUser{UserID: c.PeerID, AccessHash: c.AccessHash}, nil
	case "chat":
		return &tg.InputPeerChat{ChatID: c.PeerID}, nil
	case "channel":
		return &tg.InputPeerChannel{ChannelID: c.PeerID, AccessHash: c.AccessHash}, nil
	}
	return nil, fmt.Errorf("неизвестный тип чата %q", c.Kind)
}

func (p *peer) fill(users []tg.UserClass, chats []tg.ChatClass) {
	switch p.Kind {
	case "user":
		for _, uc := range users {
			if u, ok := uc.(*tg.User); ok && u.ID == p.PeerID {
				p.setUser(u)
				return
			}
		}
	default:
		for _, cc := range chats {
			switch ch := cc.(type) {
			case *tg.Chat:
				if ch.ID == p.PeerID {
					p.Title = ch.Title
					return
				}
			case *tg.Channel:
				if ch.ID == p.PeerID {
					p.Title, p.Username = ch.Title, ch.Username
					if ch.AccessHash != 0 {
						p.AccessHash = ch.AccessHash
					}
					return
				}
			}
		}
	}
}

func (p *peer) setUser(u *tg.User) {
	p.Title = strings.TrimSpace(u.FirstName + " " + u.LastName)
	p.Username, p.Phone = u.Username, u.Phone
	if u.AccessHash != 0 {
		p.AccessHash = u.AccessHash
	}
	if p.Title == "" {
		p.Title = u.Username
	}
}

func (p *peer) resolveTitle(ctx context.Context, api *tg.Client) {
	if p.Kind == "user" {
		us, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUser{UserID: p.PeerID, AccessHash: p.AccessHash}})
		if err == nil {
			for _, uc := range us {
				if u, ok := uc.(*tg.User); ok {
					p.setUser(u)
				}
			}
		}
	}
	if p.Title == "" {
		p.Title = p.ExtID
	}
}

// ---------- helpers ----------

func toStore(chatID int64, m *tg.Message) store.Message {
	msg := store.Message{ChatID: chatID, MsgID: int64(m.ID), Out: m.Out, Text: msgText(m), Date: int64(m.Date)}
	msg.MediaKind, msg.MediaRef = mediaOf(m)
	msg.FillFile()
	return msg
}

func msgText(m *tg.Message) string {
	if t := strings.TrimSpace(m.Message); t != "" {
		return t
	}
	switch m.Media.(type) {
	case *tg.MessageMediaPhoto:
		return "" // фото показываем картинкой
	case *tg.MessageMediaDocument:
		if _, ok := docRef(m); ok {
			return "" // файл показываем карточкой
		}
		return "[стикер]"
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive, *tg.MessageMediaVenue:
		return "[геолокация]"
	case *tg.MessageMediaContact:
		return "[контакт]"
	case nil:
		return ""
	}
	return "[вложение]"
}

func randomID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]))
}

func floodRetry[T any](ctx context.Context, emit Emitter, f func() (T, error)) (T, error) {
	for {
		v, err := f()
		if d, ok := tgerr.AsFloodWait(err); ok {
			emit("sync:warn", fmt.Sprintf("Telegram попросил подождать %d с", int(d.Seconds())))
			select {
			case <-time.After(d + time.Second):
				continue
			case <-ctx.Done():
				return v, ctx.Err()
			}
		}
		return v, err
	}
}

func humanErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case tgerr.Is(err, "PHONE_NUMBER_INVALID"):
		return errors.New("неверный номер телефона")
	case tgerr.Is(err, "PHONE_CODE_INVALID"):
		return errors.New("неверный код")
	case tgerr.Is(err, "PHONE_CODE_EXPIRED"):
		return errors.New("код устарел — запросите новый")
	case tgerr.Is(err, "PASSWORD_HASH_INVALID"):
		return errors.New("неверный пароль 2FA")
	case tgerr.Is(err, "API_ID_INVALID"):
		return errors.New("неверный API ID / API Hash")
	case tgerr.Is(err, "PEER_FLOOD"):
		return errors.New("Telegram ограничил отправку (PEER_FLOOD) — сделайте паузу на сутки")
	case tgerr.Is(err, "USER_PRIVACY_RESTRICTED"):
		return errors.New("пользователь запретил писать ему")
	}
	if d, ok := tgerr.AsFloodWait(err); ok {
		return fmt.Errorf("слишком часто — подождите %d с", int(d.Seconds()))
	}
	return err
}

// ---------- удаление ----------

// onDelete: сообщения удалили в Telegram. Свои убираем из CRM,
// сообщения клиента оставляем с пометкой «удалено».
func (c *Client) onDelete(ctx context.Context, channelExt string, ids []int) {
	res, err := c.st.ApplyRemoteDelete(ctx, channelExt, ids)
	if err != nil || len(res) == 0 {
		return
	}
	for _, r := range res {
		c.emit("message:deleted", r)
	}
	c.emit("chats:changed", nil)
}

// DeleteMessage удаляет своё сообщение у обоих собеседников и из CRM.
func (c *Client) DeleteMessage(ctx context.Context, chat store.Chat, msgID int64) error {
	api, err := c.apiClient()
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
	switch chat.Kind {
	case "channel":
		_, err = api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: chat.PeerID, AccessHash: chat.AccessHash}, ID: []int{int(msgID)},
		})
	default:
		_, err = api.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{Revoke: true, ID: []int{int(msgID)}})
	}
	if err != nil && !tgerr.Is(err, "MESSAGE_DELETE_FORBIDDEN") {
		return humanErr(err)
	}
	if err := c.st.DeleteOwnMessage(ctx, chat.ID, msgID); err != nil {
		return err
	}
	c.emit("message:deleted", store.DeleteResult{ChatID: chat.ID, Removed: []int64{msgID}, Marked: []int64{}})
	c.emit("chats:changed", nil)
	return nil
}

// ---------- фото ----------

type mediaRef struct {
	ID    int64  `json:"id"`
	Hash  int64  `json:"hash"`
	Ref   []byte `json:"ref"`
	Size  string `json:"size,omitempty"`  // фото: тип размера
	Name  string `json:"name,omitempty"`  // файл: имя
	Mime  string `json:"mime,omitempty"`  // файл: MIME
	Bytes int64  `json:"bytes,omitempty"` // файл: размер
}

// docRef — ссылка на файл (документ, видео, голосовое). Стикеры пропускаем.
func docRef(m *tg.Message) (string, bool) {
	md, ok := m.Media.(*tg.MessageMediaDocument)
	if !ok {
		return "", false
	}
	d, ok := md.Document.(*tg.Document)
	if !ok {
		return "", false
	}
	name := ""
	for _, a := range d.Attributes {
		switch v := a.(type) {
		case *tg.DocumentAttributeSticker:
			return "", false
		case *tg.DocumentAttributeFilename:
			name = v.FileName
		}
	}
	if name == "" {
		name = fmt.Sprintf("file_%d%s", d.ID, extByMime(d.MimeType))
	}
	b, _ := json.Marshal(mediaRef{ID: d.ID, Hash: d.AccessHash, Ref: d.FileReference, Name: name, Mime: d.MimeType, Bytes: d.Size})
	return string(b), true
}

func extByMime(mime string) string {
	switch mime {
	case "video/mp4":
		return ".mp4"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "application/pdf":
		return ".pdf"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	}
	return ""
}

// mediaOf — тип и ссылка вложения сообщения.
func mediaOf(m *tg.Message) (kind, ref string) {
	if r, ok := photoRef(m); ok {
		return "photo", r
	}
	if r, ok := docRef(m); ok {
		return "file", r
	}
	return "", ""
}

func photoRef(m *tg.Message) (string, bool) {
	mp, ok := m.Media.(*tg.MessageMediaPhoto)
	if !ok {
		return "", false
	}
	p, ok := mp.Photo.(*tg.Photo)
	if !ok {
		return "", false
	}
	size := biggestSize(p.Sizes)
	if size == "" {
		return "", false
	}
	b, _ := json.Marshal(mediaRef{ID: p.ID, Hash: p.AccessHash, Ref: p.FileReference, Size: size})
	return string(b), true
}

// biggestSize — самый крупный вариант, но не больше ~1280px (тип «y»/«x»).
func biggestSize(sizes []tg.PhotoSizeClass) string {
	best, bestArea := "", 0
	for _, s := range sizes {
		var t string
		var w, h int
		switch v := s.(type) {
		case *tg.PhotoSize:
			t, w, h = v.Type, v.W, v.H
		case *tg.PhotoSizeProgressive:
			t, w, h = v.Type, v.W, v.H
		default:
			continue
		}
		if w*h > bestArea {
			best, bestArea = t, w*h
		}
	}
	return best
}

// DownloadMedia скачивает фото или файл сообщения в path. Если ссылка на файл устарела —
// перезапрашивает сообщение у Telegram и пробует ещё раз.
func (c *Client) DownloadMedia(ctx context.Context, chat store.Chat, m store.Message, path string) error {
	api, err := c.apiClient()
	if err != nil {
		return err
	}
	ref := m.MediaRef
	for attempt := 0; attempt < 2; attempt++ {
		var r mediaRef
		if err := json.Unmarshal([]byte(ref), &r); err != nil || r.ID == 0 {
			return errors.New("у сообщения нет вложения")
		}
		var loc tg.InputFileLocationClass = &tg.InputPhotoFileLocation{ID: r.ID, AccessHash: r.Hash, FileReference: r.Ref, ThumbSize: r.Size}
		if m.MediaKind == "file" {
			loc = &tg.InputDocumentFileLocation{ID: r.ID, AccessHash: r.Hash, FileReference: r.Ref}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		tmp := path + ".part"
		_, err = downloader.NewDownloader().Download(api, loc).ToPath(ctx, tmp)
		if err == nil {
			return os.Rename(tmp, path)
		}
		_ = os.Remove(tmp)
		if attempt > 0 || !tgerr.Is(err, "FILE_REFERENCE_EXPIRED", "FILE_REFERENCE_INVALID") {
			return humanErr(err)
		}
		fresh, ferr := c.refetch(ctx, api, chat, int(m.MsgID))
		if ferr != nil {
			return ferr
		}
		kind, newRef := mediaOf(fresh)
		if kind != m.MediaKind || newRef == "" {
			return errors.New("вложение больше недоступно в Telegram")
		}
		ref = newRef
		_ = c.st.SetMediaRef(ctx, m.ID, ref)
	}
	return nil
}

func (c *Client) refetch(ctx context.Context, api *tg.Client, chat store.Chat, msgID int) (*tg.Message, error) {
	ids := []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}}
	var res tg.MessagesMessagesClass
	var err error
	if chat.Kind == "channel" {
		res, err = api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: chat.PeerID, AccessHash: chat.AccessHash}, ID: ids,
		})
	} else {
		res, err = api.MessagesGetMessages(ctx, ids)
	}
	if err != nil {
		return nil, humanErr(err)
	}
	if mod, ok := res.AsModified(); ok {
		for _, mc := range mod.GetMessages() {
			if m, ok := mc.(*tg.Message); ok && m.ID == msgID {
				return m, nil
			}
		}
	}
	return nil, errors.New("сообщение удалено в Telegram")
}
