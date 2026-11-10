package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sinaps-crm/internal/notify"
	"sinaps-crm/internal/store"
	"sinaps-crm/internal/telegram"
	"sinaps-crm/internal/whatsapp"
)

// ---------- плагин «Уведомления на телефон» ----------
//
// CRM держит прямое соединение с приложением на телефоне (internal/notify) и шлёт ему события.
// Клиентам плагин ничего не отправляет и переписку в мессенджерах не меняет.

const (
	notifyID = "notify"
	// страница, откуда скачивается приложение: его собирает GitHub Actions из папки android/
	apkPage = "https://github.com/Ti28282/Sinaps-CRM/releases/tag/apk-latest"

	silentAfter = 3 * 24 * time.Hour // «клиент молчит 3 дня»
	waitAfter   = 5 * 24 * time.Hour // «чек ждёт оплаты больше 5 дней»
	alarmDelay  = time.Minute        // связь пропала и не вернулась за минуту
)

func init() { builtinPlugins[notifyID] = true }

var errNotifyOff = errors.New("плагин «Уведомления» выключен — включите его в «Настройки → Плагины»")

// notifyState — рабочее состояние плагина внутри App.
type notifyState struct {
	hub        *notify.Hub
	winActive  atomic.Bool  // окно CRM открыто и в фокусе
	quitting   atomic.Bool  // пользователь нажал «Выйти из CRM»
	quietCount atomic.Int64 // сколько событий пришло в тихие часы
	ready      map[string]bool
	lastScan   time.Time
}

type notifySettings struct {
	Msg, Fresh, Reply, Silent, Wait, Alarm bool
	Show                                   string // full | name | none
	SkipActive, Quiet, Group               bool
}

func (a *App) notifyCfg() notifySettings {
	s := a.PluginSettings(notifyID)
	flag := func(k string, def bool) bool {
		if v, ok := s[k].(bool); ok {
			return v
		}
		return def
	}
	show := "full"
	if v, ok := s["show"].(string); ok {
		switch {
		case strings.Contains(v, "новое сообщение"):
			show = "none"
		case strings.HasPrefix(v, "Только имя"):
			show = "name"
		}
	}
	return notifySettings{
		Msg: flag("evMsg", true), Fresh: flag("evFresh", true), Reply: flag("evReply", true),
		Silent: flag("evSilent", false), Wait: flag("evWait", false), Alarm: flag("evAlarm", true),
		Show: show, SkipActive: flag("skipActive", true), Quiet: flag("quiet", true), Group: flag("group", true),
	}
}

// syncNotify включает или выключает сервер вслед за плагином и обновляет настройки показа.
func (a *App) syncNotify() {
	on := a.pluginActive(notifyID)
	a.pmu.Lock()
	if a.ntf.hub == nil {
		a.ntf.hub = notify.New(a.st, "SINAPS CRM", func() { a.emit("notify:changed", nil) })
		a.ntf.ready = map[string]bool{}
		go a.notifyLoop()
	}
	hub := a.ntf.hub
	a.pmu.Unlock()
	if !on {
		hub.Stop()
		return
	}
	cfg := a.notifyCfg()
	hub.SetConfig(notify.Config{Show: cfg.Show, Group: cfg.Group})
	if err := hub.Start(notify.DefaultPort); err != nil {
		a.emit("sync:warn", "Уведомления на телефон: "+err.Error())
	}
}

func (a *App) notifyHub() (*notify.Hub, bool) {
	a.pmu.RLock()
	hub := a.ntf.hub
	a.pmu.RUnlock()
	return hub, hub != nil && hub.Running() && a.pluginActive(notifyID)
}

// ---------- события CRM → телефон ----------

// notifyHook смотрит на события, которые CRM и так рассылает интерфейсу.
func (a *App) notifyHook(event string, data any) {
	switch event {
	case "message:new":
		if m, ok := data.(store.Message); ok && !m.Out && m.Date >= time.Now().Unix()-300 {
			go a.notifyMessage(m)
		}
	case "tg:status":
		if s, ok := data.(telegram.Status); ok {
			a.notifyConn("Telegram", s.State == telegram.StateReady)
		}
	case "wa:status":
		if s, ok := data.(whatsapp.Status); ok {
			a.notifyConn("WhatsApp", s.State == whatsapp.StateReady)
		}
	}
}

func quietHours(t time.Time) bool { h := t.Hour(); return h >= 23 || h < 8 }

// publish отправляет событие с учётом тихих часов и открытого окна CRM.
func (a *App) publish(cfg notifySettings, e notify.Event, userFacing bool) {
	hub, ok := a.notifyHub()
	if !ok {
		return
	}
	if cfg.Quiet && quietHours(time.Now()) {
		e.Quiet = true
		a.ntf.quietCount.Add(1)
	}
	if userFacing && cfg.SkipActive && a.ntf.winActive.Load() {
		e.Quiet = true // пользователь и так видит это в CRM — на телефон только в ленту
	}
	_, _ = hub.Publish(a.ctx, e)
}

func chatEvent(c store.Chat) notify.Event {
	return notify.Event{ChatID: c.ID, Client: c.Title, Channel: c.Channel, Username: c.Username, Phone: c.Phone, Stage: store.StageName(c.Stage)}
}

func (a *App) notifyMessage(m store.Message) {
	if _, ok := a.notifyHub(); !ok {
		return
	}
	cfg := a.notifyCfg()
	c, err := a.st.Chat(a.ctx, m.ChatID)
	if err != nil {
		return
	}
	text := strings.TrimSpace(m.Text)
	switch {
	case m.MediaKind == "photo" && (text == "" || text == "[фото]"):
		text = "Фото"
	case m.MediaKind == "file" && (text == "" || text == "[файл]"):
		text = "Файл: " + m.FileName
	}
	if r := []rune(text); len(r) > 400 {
		text = string(r[:400]) + "…"
	}
	now := time.Now().Unix()
	e := chatEvent(c)
	e.Text, e.Msg, e.At = text, text, m.Date
	switch {
	case c.CreatedAt >= now-180 && cfg.Fresh:
		e.Kind, e.Title = "fresh", "Новый клиент · "+c.Title
	case a.justReplied(c.ID, now) && cfg.Reply:
		e.Kind, e.Title = "reply", "Ответил на рассылку · "+c.Title
	case cfg.Msg:
		e.Kind, e.Title = "msg", "Клиент написал · "+c.Title
	default:
		return
	}
	a.publish(cfg, e, true)
}

// justReplied — store только что сам перевёл клиента из «Написал» в «Ответил».
func (a *App) justReplied(chatID, now int64) bool {
	ev, err := a.st.Events(a.ctx, chatID)
	return err == nil && len(ev) > 0 && ev[0].Kind == "stage" && ev[0].At >= now-10 && strings.HasPrefix(ev[0].Text, "Клиент ответил")
}

// notifyConn: мессенджер был подключён и отвалился дольше минуты — «CRM потеряла связь».
func (a *App) notifyConn(name string, ready bool) {
	a.pmu.Lock()
	if a.ntf.ready == nil {
		a.ntf.ready = map[string]bool{}
	}
	was := a.ntf.ready[name]
	a.ntf.ready[name] = ready
	a.pmu.Unlock()
	if !was || ready {
		return
	}
	time.AfterFunc(alarmDelay, func() {
		a.pmu.RLock()
		back := a.ntf.ready[name]
		a.pmu.RUnlock()
		cfg := a.notifyCfg()
		if back || !cfg.Alarm {
			return
		}
		a.publish(cfg, notify.Event{Kind: "alarm", Title: "CRM потеряла связь",
			Text: name + " отключился — сообщения не приходят. Открой CRM на компьютере."}, false)
	})
}

// notifyLoop раз в минуту: утренняя сводка после тихих часов; раз в полчаса — «молчит» и «чек ждёт».
func (a *App) notifyLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		if a.ntf.quitting.Load() {
			return
		}
		if a.ctx == nil {
			continue
		}
		if _, ok := a.notifyHub(); !ok {
			continue
		}
		cfg := a.notifyCfg()
		now := time.Now()
		if !quietHours(now) {
			if n := a.ntf.quietCount.Swap(0); n > 0 {
				hub, _ := a.notifyHub()
				_, _ = hub.Publish(a.ctx, notify.Event{Kind: "digest", Title: "Пока было тихо",
					Text: fmt.Sprintf("Событий за ночь: %d — они в ленте", n)})
			}
		}
		if now.Sub(a.ntf.lastScan) < 30*time.Minute {
			continue
		}
		a.ntf.lastScan = now
		a.notifyScan(cfg, now)
	}
}

// notifyScan ищет клиентов, которые только что перешли порог «молчит 3 дня», и чеки, что ждут оплаты.
// Берём только тех, кто пересёк порог за последние сутки: давние не заваливают телефон при первом включении.
func (a *App) notifyScan(cfg notifySettings, now time.Time) {
	if cfg.Silent {
		edge := now.Add(-silentAfter).Unix()
		chats, _ := a.st.SilentChats(a.ctx, edge)
		for _, c := range chats {
			if c.LastAt < edge-86400 {
				continue
			}
			if first, _ := a.st.NotifyMark(a.ctx, "silent", fmt.Sprintf("%d:%d", c.ID, c.LastAt)); !first {
				continue
			}
			e := chatEvent(c)
			e.Kind, e.Title, e.Text = "silent", "Молчит 3 дня · "+c.Title, "Последним писал ты — напомнить о себе?"
			a.publish(cfg, e, false)
		}
	}
	if cfg.Wait && a.pluginActive(chekiID) {
		edge := now.Add(-waitAfter).Unix()
		list, _ := a.st.WaitingReceipts(a.ctx, edge)
		for _, r := range list {
			if r.PaidAt < edge-86400 {
				continue
			}
			if first, _ := a.st.NotifyMark(a.ctx, "wait", fmt.Sprint(r.ID)); !first {
				continue
			}
			e := notify.Event{Kind: "wait", Title: "Чек ждёт оплаты · " + r.ChatTitle, ChatID: r.ChatID, Client: r.ChatTitle, Channel: r.Channel,
				Text: fmtMoney(r.Amount, r.Currency) + " — больше 5 дней"}
			if c, err := a.st.Chat(a.ctx, r.ChatID); err == nil {
				e.Username, e.Phone, e.Stage = c.Username, c.Phone, store.StageName(c.Stage)
			}
			a.publish(cfg, e, false)
		}
	}
}

// ---------- страница плагина ----------

// NotifyView — состояние для страницы «Уведомления».
type NotifyView struct {
	Running bool            `json:"running"`
	Port    int             `json:"port"`
	Addrs   []string        `json:"addrs"`
	Devices []notify.Device `json:"devices"`
	Pairing *notify.Pairing `json:"pairing"`
	PairURL string          `json:"pairUrl"`
	ApkURL  string          `json:"apkUrl"`
}

func (a *App) NotifyState() (NotifyView, error) {
	v := NotifyView{Addrs: []string{}, Devices: []notify.Device{}, ApkURL: apkPage}
	hub, ok := a.notifyHub()
	if !ok {
		return v, errNotifyOff
	}
	v.Running, v.Port, v.Addrs = true, hub.Port(), notify.LocalAddrs()
	if v.Addrs == nil {
		v.Addrs = []string{}
	}
	ds, err := hub.Devices(a.ctx)
	if err != nil {
		return v, err
	}
	v.Devices = ds
	if p, ok := hub.Pairing(); ok {
		v.Pairing = &p
		q := url.Values{}
		q.Set("h", strings.Join(v.Addrs, ","))
		q.Set("p", fmt.Sprint(v.Port))
		q.Set("c", p.Code)
		v.PairURL = "sinaps://pair?" + q.Encode()
	}
	return v, nil
}

// NotifyStartPairing показывает новый QR-код (действует 5 минут, срабатывает один раз).
func (a *App) NotifyStartPairing() (NotifyView, error) {
	hub, ok := a.notifyHub()
	if !ok {
		return NotifyView{}, errNotifyOff
	}
	if _, err := hub.StartPairing(); err != nil {
		return NotifyView{}, err
	}
	return a.NotifyState()
}

func (a *App) NotifyCancelPairing() (NotifyView, error) {
	if hub, ok := a.notifyHub(); ok {
		hub.CancelPairing()
	}
	return a.NotifyState()
}

// NotifyUnpair отвязывает телефон: он больше не сможет подключиться.
func (a *App) NotifyUnpair(id string) (NotifyView, error) {
	hub, ok := a.notifyHub()
	if !ok {
		return NotifyView{}, errNotifyOff
	}
	if err := hub.Unpair(a.ctx, id); err != nil {
		return NotifyView{}, err
	}
	return a.NotifyState()
}

// NotifyTest шлёт тестовое уведомление. Ошибка, если ни один телефон не на связи.
func (a *App) NotifyTest() error {
	hub, ok := a.notifyHub()
	if !ok {
		return errNotifyOff
	}
	ds, err := hub.Devices(a.ctx)
	if err != nil {
		return err
	}
	online := false
	for _, d := range ds {
		online = online || d.Online
	}
	if !online {
		return errors.New("телефон сейчас не на связи: проверь, что он в той же сети Wi-Fi и приложение запущено")
	}
	_, err = hub.Publish(a.ctx, notify.Event{Kind: "test", Title: "Проверка связи", Text: "Уведомления из SINAPS CRM работают"})
	return err
}

// OpenApkPage открывает страницу, с которой скачивается приложение.
func (a *App) OpenApkPage() { runtime.BrowserOpenURL(a.ctx, apkPage) }

// SetWindowActive — интерфейс сообщает, смотрит ли пользователь сейчас в CRM.
func (a *App) SetWindowActive(active bool) { a.ntf.winActive.Store(active) }

// ---------- работа в фоне ----------

// keepInBackground — закрытие окна не должно выключать CRM: есть привязанный телефон.
func (a *App) keepInBackground() bool {
	hub, ok := a.notifyHub()
	if !ok {
		return false
	}
	ds, err := hub.Devices(a.ctx)
	return err == nil && len(ds) > 0
}

// beforeClose: пока привязан телефон, крестик прячет окно, а CRM продолжает
// получать сообщения и слать уведомления. Совсем выйти — «Выйти из CRM».
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.ntf.quitting.Load() || !a.keepInBackground() {
		return false
	}
	a.ntf.winActive.Store(false)
	runtime.WindowHide(ctx)
	return true
}

// showWindow — CRM запустили ещё раз, пока она работает в фоне: показываем окно.
func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// QuitApp полностью выключает CRM (уведомления на телефон перестанут приходить).
func (a *App) QuitApp() {
	a.ntf.quitting.Store(true)
	runtime.Quit(a.ctx)
}
