package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sinaps-crm/internal/store"
	"sinaps-crm/internal/telegram"
)

const version = "0.1.0"

// App — всё, что видит фронт: каждый экспортируемый метод доступен как
// window.go.main.App.<Method>() и возвращает Promise.
type App struct {
	ctx     context.Context
	dataDir string
	st      *store.Store
	tg      *telegram.Client

	autoOnce sync.Once

	bmu      sync.Mutex
	bc       broadcastState
	bcCancel context.CancelFunc
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	a.dataDir = filepath.Join(base, "SINAPS-CRM")
	_ = os.MkdirAll(a.dataDir, 0o700)

	a.st, err = store.Open(filepath.Join(a.dataDir, "crm.db"))
	if err != nil {
		runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type: runtime.ErrorDialog, Title: "База данных", Message: err.Error(),
		})
		runtime.Quit(ctx)
		return
	}
	a.tg = telegram.New(a.st, a.dataDir, a.emit)

	// ключи уже сохранены — подключаемся сразу
	if id, _ := strconv.Atoi(a.st.Get(ctx, "tg_api_id")); id != 0 {
		_ = a.tg.Start(id, a.st.Get(ctx, "tg_api_hash"))
	}
}

func (a *App) shutdown(ctx context.Context) {
	a.StopBroadcast()
	if a.tg != nil {
		a.tg.Stop()
	}
	if a.st != nil {
		_ = a.st.Close()
	}
}

func (a *App) emit(event string, data any) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, event, data)
	// после входа один раз тихо докачиваем выбранную папку
	if s, ok := data.(telegram.Status); ok && event == "tg:status" && s.State == telegram.StateReady {
		a.autoOnce.Do(func() { go a.autoSync() })
	}
}

func (a *App) autoSync() {
	if a.st.Get(a.ctx, "auto_sync") == "0" {
		return
	}
	fid, _ := strconv.Atoi(a.st.Get(a.ctx, "tg_folder_id"))
	if fid == 0 {
		return
	}
	time.Sleep(2 * time.Second)
	res, err := a.tg.Sync(a.ctx, fid, a.historyLimit())
	if err != nil {
		a.emit("sync:warn", "Автосинхронизация: "+err.Error())
		return
	}
	a.emit("sync:done", res)
}

// ---------- общее ----------

type Info struct {
	Version string `json:"version"`
	DataDir string `json:"dataDir"`
}

func (a *App) GetInfo() Info { return Info{Version: version, DataDir: a.dataDir} }

func (a *App) OpenDataDir() error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", a.dataDir)
	case "darwin":
		cmd = exec.Command("open", a.dataDir)
	default:
		cmd = exec.Command("xdg-open", a.dataDir)
	}
	return cmd.Start()
}

type Settings struct {
	TgAPIID      int    `json:"tgApiId"`
	HasAPIHash   bool   `json:"hasApiHash"`
	TgFolderID   int    `json:"tgFolderId"`
	TgFolderName string `json:"tgFolderName"`
	HistoryLimit int    `json:"historyLimit"`
	AutoSync     bool   `json:"autoSync"`
	DailyLimit   int    `json:"dailyLimit"`
	Template     string `json:"template"`
}

const defaultTemplate = "{name}, здравствуйте! Это Тимур, SINAPS. Делаю ботов и ИИ-автоматизацию для бизнеса: запись клиентов, ответы 24/7, заявки сразу в таблицу. Если есть что автоматизировать — подскажу бесплатно. Актуально?"

func (a *App) GetSettings() Settings {
	ctx := a.ctx
	id, _ := strconv.Atoi(a.st.Get(ctx, "tg_api_id"))
	fid, _ := strconv.Atoi(a.st.Get(ctx, "tg_folder_id"))
	tpl := a.st.Get(ctx, "bc_template")
	if tpl == "" {
		tpl = defaultTemplate
	}
	return Settings{
		TgAPIID: id, HasAPIHash: a.st.Get(ctx, "tg_api_hash") != "",
		TgFolderID: fid, TgFolderName: a.st.Get(ctx, "tg_folder_name"),
		HistoryLimit: a.historyLimit(), AutoSync: a.st.Get(ctx, "auto_sync") != "0",
		DailyLimit: a.dailyLimit(), Template: tpl,
	}
}

type SettingsPatch struct {
	HistoryLimit *int  `json:"historyLimit"`
	AutoSync     *bool `json:"autoSync"`
	DailyLimit   *int  `json:"dailyLimit"`
}

func (a *App) SaveSettings(p SettingsPatch) error {
	if p.HistoryLimit != nil {
		_ = a.st.Set(a.ctx, "history_limit", strconv.Itoa(clamp(*p.HistoryLimit, 20, 2000)))
	}
	if p.AutoSync != nil {
		v := "1"
		if !*p.AutoSync {
			v = "0"
		}
		_ = a.st.Set(a.ctx, "auto_sync", v)
	}
	if p.DailyLimit != nil {
		_ = a.st.Set(a.ctx, "daily_limit", strconv.Itoa(clamp(*p.DailyLimit, 1, 50)))
	}
	return nil
}

func (a *App) historyLimit() int {
	n, _ := strconv.Atoi(a.st.Get(a.ctx, "history_limit"))
	if n == 0 {
		return 200
	}
	return n
}

func (a *App) dailyLimit() int {
	n, _ := strconv.Atoi(a.st.Get(a.ctx, "daily_limit"))
	if n == 0 {
		return 20
	}
	return n
}

// ---------- Telegram ----------

func (a *App) TgStatus() telegram.Status { return a.tg.Status() }

func (a *App) TgSaveKeys(apiID int, apiHash string) error {
	apiHash = strings.TrimSpace(apiHash)
	if apiHash == "" {
		apiHash = a.st.Get(a.ctx, "tg_api_hash") // оставили поле пустым — берём сохранённый
	}
	if apiID == 0 || apiHash == "" {
		return errors.New("укажите API ID и API Hash с my.telegram.org")
	}
	_ = a.st.Set(a.ctx, "tg_api_id", strconv.Itoa(apiID))
	_ = a.st.Set(a.ctx, "tg_api_hash", apiHash)
	return a.tg.Start(apiID, apiHash)
}

func (a *App) TgSendCode(phone string) error { return a.tg.SendCode(a.ctx, phone) }
func (a *App) TgSignIn(code string) error    { return a.tg.SignIn(a.ctx, code) }
func (a *App) TgResendCode() error           { return a.tg.ResendCode(a.ctx) }
func (a *App) TgStartQR() error              { return a.tg.StartQR() }
func (a *App) TgCancelQR()                   { a.tg.CancelQR() }
func (a *App) TgPassword(pwd string) error   { return a.tg.Password(a.ctx, pwd) }

func (a *App) TgLogout() error {
	a.autoOnce = sync.Once{}
	return a.tg.Logout(a.ctx)
}

func (a *App) TgFolders() ([]telegram.Folder, error) { return a.tg.Folders(a.ctx) }

func (a *App) SyncFolder(folderID int, folderName string) (telegram.SyncResult, error) {
	_ = a.st.Set(a.ctx, "tg_folder_id", strconv.Itoa(folderID))
	_ = a.st.Set(a.ctx, "tg_folder_name", folderName)
	return a.tg.Sync(a.ctx, folderID, a.historyLimit())
}

func (a *App) Resync() (telegram.SyncResult, error) {
	fid, _ := strconv.Atoi(a.st.Get(a.ctx, "tg_folder_id"))
	if fid == 0 {
		return telegram.SyncResult{}, errors.New("сначала выберите папку на экране «Подключения»")
	}
	return a.tg.Sync(a.ctx, fid, a.historyLimit())
}

// ---------- CRM ----------

func (a *App) ListChats(channel, search string) ([]store.Chat, error) {
	return a.st.Chats(a.ctx, channel, search)
}

type ChatView struct {
	Chat     store.Chat      `json:"chat"`
	Messages []store.Message `json:"messages"`
	Events   []store.Event   `json:"events"`
}

func (a *App) GetChat(id int64) (ChatView, error) {
	c, err := a.st.Chat(a.ctx, id)
	if err != nil {
		return ChatView{}, err
	}
	msgs, err := a.st.Messages(a.ctx, id, 500)
	if err != nil {
		return ChatView{}, err
	}
	ev, _ := a.st.Events(a.ctx, id)
	return ChatView{Chat: c, Messages: msgs, Events: ev}, nil
}

func (a *App) UpdateChat(id int64, p store.ChatPatch) error {
	if err := a.st.PatchChat(a.ctx, id, p); err != nil {
		return err
	}
	a.emit("chats:changed", nil)
	return nil
}

func (a *App) MarkRead(id int64) error {
	if err := a.st.MarkRead(a.ctx, id); err != nil {
		return err
	}
	a.emit("chats:changed", nil)
	return nil
}

func (a *App) DeleteChat(id int64) error {
	if err := a.st.DeleteChat(a.ctx, id); err != nil {
		return err
	}
	a.emit("chats:changed", nil)
	return nil
}

func (a *App) SendMessage(id int64, text string) error {
	c, err := a.st.Chat(a.ctx, id)
	if err != nil {
		return err
	}
	if err := a.tg.Send(a.ctx, c, text); err != nil {
		return err
	}
	_ = a.st.LogSend(a.ctx, id, "reply")
	_ = a.st.MarkRead(a.ctx, id)
	if c.Stage == "new" {
		s := "sent"
		_ = a.st.PatchChat(a.ctx, id, store.ChatPatch{Stage: &s})
	}
	a.emit("chats:changed", nil)
	return nil
}

func (a *App) Stats() (store.Stats, error) { return a.st.Stats(a.ctx) }

// ---------- рассылки ----------

type broadcastState struct {
	Running   bool   `json:"running"`
	Total     int    `json:"total"`
	Done      int    `json:"done"`
	Failed    int    `json:"failed"`
	Current   string `json:"current"`
	NextAt    int64  `json:"nextAt"`
	LastError string `json:"lastError"`
}

type BroadcastInfo struct {
	broadcastState
	SentToday  int `json:"sentToday"`
	DailyLimit int `json:"dailyLimit"`
}

type BroadcastReq struct {
	Template string  `json:"template"`
	ChatIDs  []int64 `json:"chatIds"`
}

func (a *App) BroadcastStatus() BroadcastInfo {
	a.bmu.Lock()
	s := a.bc
	a.bmu.Unlock()
	return BroadcastInfo{broadcastState: s, SentToday: a.st.SentToday(a.ctx, "broadcast"), DailyLimit: a.dailyLimit()}
}

// PreviewTemplate — как сообщение увидит конкретный клиент.
func (a *App) PreviewTemplate(tpl string, chatID int64) string {
	c, err := a.st.Chat(a.ctx, chatID)
	if err != nil {
		return render(tpl, store.Chat{})
	}
	return render(tpl, c)
}

func (a *App) StartBroadcast(req BroadcastReq) error {
	if strings.TrimSpace(req.Template) == "" {
		return errors.New("шаблон пустой")
	}
	if len(req.ChatIDs) == 0 {
		return errors.New("выберите получателей")
	}
	if a.tg.Status().State != telegram.StateReady {
		return errors.New("Telegram не подключён")
	}
	_ = a.st.Set(a.ctx, "bc_template", req.Template)

	left := a.dailyLimit() - a.st.SentToday(a.ctx, "broadcast")
	if left <= 0 {
		return fmt.Errorf("дневной лимит %d сообщений исчерпан — продолжим завтра", a.dailyLimit())
	}
	ids := req.ChatIDs
	if len(ids) > left {
		ids = ids[:left]
	}

	a.bmu.Lock()
	if a.bc.Running {
		a.bmu.Unlock()
		return errors.New("рассылка уже идёт")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.bcCancel = cancel
	a.bc = broadcastState{Running: true, Total: len(ids)}
	a.bmu.Unlock()
	a.emitBroadcast()

	go a.runBroadcast(ctx, req.Template, ids)
	return nil
}

func (a *App) StopBroadcast() {
	a.bmu.Lock()
	if a.bcCancel != nil {
		a.bcCancel()
	}
	a.bmu.Unlock()
}

func (a *App) runBroadcast(ctx context.Context, tpl string, ids []int64) {
	defer func() {
		a.bmu.Lock()
		a.bc.Running, a.bc.Current, a.bc.NextAt = false, "", 0
		a.bcCancel = nil
		a.bmu.Unlock()
		a.emitBroadcast()
	}()

	for i, id := range ids {
		if ctx.Err() != nil {
			return
		}
		c, err := a.st.Chat(ctx, id)
		if err != nil {
			continue
		}
		a.setBC(func(s *broadcastState) { s.Current, s.NextAt = c.Title, 0 })

		err = a.tg.Send(ctx, c, render(tpl, c))
		if err != nil {
			a.setBC(func(s *broadcastState) { s.Failed++; s.LastError = c.Title + ": " + err.Error() })
			_ = a.st.AddEvent(ctx, id, "broadcast", "Рассылка не ушла: "+err.Error())
			if strings.Contains(err.Error(), "PEER_FLOOD") {
				return // Telegram недоволен — останавливаемся сразу
			}
		} else {
			_ = a.st.LogSend(ctx, id, "broadcast")
			_ = a.st.AddEvent(ctx, id, "broadcast", "Отправлена рассылка")
			if c.Stage == "new" {
				s := "sent"
				_ = a.st.PatchChat(ctx, id, store.ChatPatch{Stage: &s})
			}
			a.setBC(func(s *broadcastState) { s.Done++ })
			a.emit("chats:changed", nil)
		}

		if i == len(ids)-1 {
			return
		}
		// случайная пауза 40–90 с — не похоже на бота
		wait := time.Duration(40+rand.Intn(51)) * time.Second
		a.setBC(func(s *broadcastState) { s.Current, s.NextAt = "", time.Now().Add(wait).Unix() })
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
	}
}

func (a *App) setBC(f func(*broadcastState)) {
	a.bmu.Lock()
	f(&a.bc)
	a.bmu.Unlock()
	a.emitBroadcast()
}

func (a *App) emitBroadcast() { runtime.EventsEmit(a.ctx, "broadcast:status", a.BroadcastStatus()) }

func render(tpl string, c store.Chat) string {
	first := strings.TrimSpace(c.Title)
	if f := strings.Fields(first); len(f) > 0 {
		first = f[0]
	}
	if first == "" {
		first = "Здравствуйте"
	}
	return strings.NewReplacer("{name}", first, "{title}", c.Title, "{username}", c.Username).Replace(tpl)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
