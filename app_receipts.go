package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sinaps-crm/internal/dataio"
	"sinaps-crm/internal/store"
)

// ---------- плагин «Чеки» ----------
//
// Работает только внутри CRM: ничего не отправляет клиентам и не меняет переписку.

var errChekiOff = errors.New("плагин «Чеки» выключен — включите его в «Настройки → Плагины»")

// chekiSettings — настройки плагина с запасными значениями.
type chekiSettings struct {
	Currency    string // символ: ₽, сом, ₸, $
	SumToDeal   bool
	FirstToDeal bool
}

func (a *App) cheki() chekiSettings {
	s := a.PluginSettings(chekiID)
	str := func(k, def string) string {
		if v, ok := s[k].(string); ok && v != "" {
			return v
		}
		return def
	}
	flag := func(k string, def bool) bool {
		if v, ok := s[k].(bool); ok {
			return v
		}
		return def
	}
	return chekiSettings{
		Currency:    currencySymbol(str("currency", "₽ рубль")),
		SumToDeal:   flag("sumToDeal", true),
		FirstToDeal: flag("firstToDeal", true),
	}
}

func currencySymbol(label string) string {
	switch {
	case strings.Contains(label, "сом"):
		return "сом"
	case strings.Contains(label, "₸"):
		return "₸"
	case strings.Contains(label, "$"):
		return "$"
	}
	return "₽"
}

// ReceiptReq — форма «Новый чек» на странице плагина.
type ReceiptReq struct {
	ChatID int64  `json:"chatId"`
	Amount int64  `json:"amount"`
	Date   string `json:"date"`   // 2026-11-07; пусто — сегодня
	Status string `json:"status"` // paid | wait | refund
	What   string `json:"what"`
}

func (r ReceiptReq) paidAt() (int64, error) {
	if r.Date == "" {
		return time.Now().Unix(), nil
	}
	t, err := time.ParseInLocation("2006-01-02", r.Date, time.Local)
	if err != nil {
		return 0, errors.New("дата оплаты в формате ДД.ММ.ГГГГ")
	}
	return t.Add(12 * time.Hour).Unix(), nil // середина дня — без сдвига дат между поясами
}

func (r ReceiptReq) check() error {
	if r.Amount <= 0 && r.Status != "wait" {
		return errors.New("укажите сумму чека")
	}
	return nil
}

// CreateReceipt — новый чек: клиент из воронки, сумма, за что, дата, статус.
func (a *App) CreateReceipt(req ReceiptReq) (store.Receipt, error) {
	if !a.pluginActive(chekiID) {
		return store.Receipt{}, errChekiOff
	}
	if req.ChatID == 0 {
		return store.Receipt{}, errors.New("выберите клиента")
	}
	if err := req.check(); err != nil {
		return store.Receipt{}, err
	}
	paidAt, err := req.paidAt()
	if err != nil {
		return store.Receipt{}, err
	}
	cfg := a.cheki()
	r, err := a.st.CreateReceipt(a.ctx, store.Receipt{
		ChatID: req.ChatID, Amount: req.Amount, Currency: cfg.Currency, Status: req.Status, What: req.What, PaidAt: paidAt,
	})
	if err != nil {
		return r, err
	}
	_ = a.st.AddEvent(a.ctx, r.ChatID, "receipt", receiptEvent("Чек", r))
	a.afterReceipts(r.ChatID, cfg, r.Status == "paid")
	return r, nil
}

// UpdateReceipt — правка чека из истории.
func (a *App) UpdateReceipt(id int64, req ReceiptReq) (store.Receipt, error) {
	if !a.pluginActive(chekiID) {
		return store.Receipt{}, errChekiOff
	}
	old, err := a.st.ReceiptByID(a.ctx, id)
	if err != nil {
		return old, err
	}
	if err := req.check(); err != nil {
		return old, err
	}
	paidAt, err := req.paidAt()
	if err != nil {
		return old, err
	}
	r, err := a.st.UpdateReceipt(a.ctx, store.Receipt{
		ID: id, Amount: req.Amount, Currency: old.Currency, Status: req.Status, What: req.What, PaidAt: paidAt,
	})
	if err != nil {
		return r, err
	}
	_ = a.st.AddEvent(a.ctx, r.ChatID, "receipt", receiptEvent("Чек изменён", r))
	a.afterReceipts(r.ChatID, a.cheki(), r.Status == "paid" && old.Status != "paid")
	return r, nil
}

// DeleteReceipt удаляет чек из истории.
func (a *App) DeleteReceipt(id int64) error {
	if !a.pluginActive(chekiID) {
		return errChekiOff
	}
	r, err := a.st.ReceiptByID(a.ctx, id)
	if err != nil {
		return nil // уже удалён
	}
	if err := a.st.DeleteReceipt(a.ctx, id); err != nil {
		return err
	}
	_ = a.st.AddEvent(a.ctx, r.ChatID, "receipt", "Чек удалён · "+fmtMoney(r.Amount, r.Currency))
	a.afterReceipts(r.ChatID, a.cheki(), false)
	return nil
}

func receiptEvent(verb string, r store.Receipt) string {
	s := fmt.Sprintf("%s · %s · %s", verb, fmtMoney(r.Amount, r.Currency), store.ReceiptStatusName(r.Status))
	if r.What != "" {
		s += " · " + r.What
	}
	return s
}

// afterReceipts — «Сумма чеков → Сумма сделки» и «Первый чек → Сделка».
func (a *App) afterReceipts(chatID int64, cfg chekiSettings, paidNow bool) {
	c, err := a.st.Chat(a.ctx, chatID)
	if err == nil {
		p := store.ChatPatch{}
		changed := false
		if cfg.SumToDeal {
			if sum, err := a.st.PaidSum(a.ctx, chatID); err == nil && sum != c.Amount {
				p.Amount, changed = &sum, true
			}
		}
		if cfg.FirstToDeal && paidNow && c.Stage != "deal" {
			s := "deal"
			p.Stage, changed = &s, true
		}
		if changed {
			_ = a.st.PatchChat(a.ctx, chatID, p)
		}
	}
	a.emit("receipts:changed", chatID)
	a.emit("chats:changed", nil)
}

func fmtMoney(n int64, cur string) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 && s[0] != '-' {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	if cur == "" {
		cur = "₽"
	}
	return b.String() + " " + cur
}

// ---------- страница «Чеки» ----------

type ReceiptsQuery struct {
	ChatID int64  `json:"chatId"` // 0 — все клиенты из воронки
	Period string `json:"period"` // quarter | year | all
}

type MonthSum struct {
	Label string `json:"label"`
	Sum   int64  `json:"sum"`
}

type ClientSum struct {
	ChatID  int64  `json:"chatId"`
	Title   string `json:"title"`
	Channel string `json:"channel"`
	Sum     int64  `json:"sum"`
}

type ReceiptsReport struct {
	Rows     []store.Receipt `json:"rows"`
	Total    int64           `json:"total"` // оплачено
	Wait     int64           `json:"wait"`  // ожидают оплаты
	Avg      int64           `json:"avg"`   // средний оплаченный чек
	Count    int             `json:"count"`
	Months   []MonthSum      `json:"months"` // последние 12 месяцев, только оплаченные
	Top      []ClientSum     `json:"top"`    // топ клиентов за период
	Clients  []ClientSum     `json:"clients"`
	Currency string          `json:"currency"`
	Year     int             `json:"year"`
}

var monthNames = []string{"янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

func periodStart(p string, now time.Time) int64 {
	switch p {
	case "quarter":
		return time.Date(now.Year(), now.Month()-2, 1, 0, 0, 0, 0, time.Local).Unix()
	case "all":
		return 0
	}
	return time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.Local).Unix()
}

// Receipts — данные страницы «Чеки».
func (a *App) Receipts(q ReceiptsQuery) (ReceiptsReport, error) {
	if !a.pluginActive(chekiID) {
		return ReceiptsReport{}, errChekiOff
	}
	now := time.Now()
	all, err := a.st.Receipts(a.ctx, 0, 0)
	if err != nil {
		return ReceiptsReport{}, err
	}
	rep := ReceiptsReport{Rows: []store.Receipt{}, Months: []MonthSum{}, Top: []ClientSum{}, Clients: []ClientSum{},
		Currency: a.cheki().Currency, Year: now.Year()}
	from := periodStart(q.Period, now)

	// 12 месяцев, заканчивая текущим
	first := time.Date(now.Year(), now.Month()-11, 1, 0, 0, 0, 0, time.Local)
	idx := map[string]int{}
	for i := 0; i < 12; i++ {
		m := first.AddDate(0, i, 0)
		idx[m.Format("2006-01")] = i
		rep.Months = append(rep.Months, MonthSum{Label: monthNames[m.Month()-1]})
	}

	clients := map[int64]*ClientSum{}
	top := map[int64]*ClientSum{}
	paid := 0
	for _, r := range all {
		if clients[r.ChatID] == nil {
			clients[r.ChatID] = &ClientSum{ChatID: r.ChatID, Title: r.ChatTitle, Channel: r.Channel}
		}
		if r.Status == "paid" {
			clients[r.ChatID].Sum += r.Amount
		}
		inPeriod := r.PaidAt >= from
		if inPeriod && r.Status == "paid" {
			if top[r.ChatID] == nil {
				top[r.ChatID] = &ClientSum{ChatID: r.ChatID, Title: r.ChatTitle, Channel: r.Channel}
			}
			top[r.ChatID].Sum += r.Amount
		}
		if q.ChatID != 0 && r.ChatID != q.ChatID {
			continue
		}
		if r.Status == "paid" {
			if i, ok := idx[time.Unix(r.PaidAt, 0).Format("2006-01")]; ok {
				rep.Months[i].Sum += r.Amount
			}
		}
		if !inPeriod {
			continue
		}
		rep.Rows = append(rep.Rows, r)
		switch r.Status {
		case "paid":
			rep.Total += r.Amount
			paid++
		case "wait":
			rep.Wait += r.Amount
		}
	}
	rep.Count = len(rep.Rows)
	if paid > 0 {
		rep.Avg = rep.Total / int64(paid)
	}
	for _, c := range top {
		rep.Top = append(rep.Top, *c)
	}
	sort.Slice(rep.Top, func(i, j int) bool { return rep.Top[i].Sum > rep.Top[j].Sum })
	if len(rep.Top) > 5 {
		rep.Top = rep.Top[:5]
	}
	for _, c := range clients {
		rep.Clients = append(rep.Clients, *c)
	}
	sort.Slice(rep.Clients, func(i, j int) bool { return rep.Clients[i].Title < rep.Clients[j].Title })
	return rep, nil
}

// ChekiSummary — цифры в окне настроек плагина.
type ChekiSummary struct {
	Count    int    `json:"count"`
	SumYear  int64  `json:"sumYear"`
	Clients  int    `json:"clients"`
	Year     int    `json:"year"`
	Currency string `json:"currency"`
}

func (a *App) ChekiSummary() (ChekiSummary, error) {
	now := time.Now()
	out := ChekiSummary{Year: now.Year(), Currency: a.cheki().Currency}
	all, err := a.st.Receipts(a.ctx, 0, 0)
	if err != nil {
		return out, err
	}
	from := periodStart("year", now)
	seen := map[int64]bool{}
	for _, r := range all {
		seen[r.ChatID] = true
		if r.Status == "paid" && r.PaidAt >= from {
			out.SumYear += r.Amount
		}
	}
	out.Count, out.Clients = len(all), len(seen)
	return out, nil
}

// ExportReceipts — выгрузка чеков в Excel. Пустой Path — отмена.
func (a *App) ExportReceipts(q ReceiptsQuery) (ExportResult, error) {
	rep, err := a.Receipts(q)
	if err != nil {
		return ExportResult{}, err
	}
	home, _ := os.UserHomeDir()
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "Экспорт чеков",
		DefaultDirectory: home,
		DefaultFilename:  "sinaps-cheki-" + time.Now().Format("2006-01-02") + ".xlsx",
		Filters:          []runtime.FileFilter{{DisplayName: "Excel (*.xlsx)", Pattern: "*.xlsx"}},
	})
	if err != nil || path == "" {
		return ExportResult{}, err
	}
	if filepath.Ext(path) == "" {
		path += ".xlsx"
	}
	return ExportResult{Path: path, Count: len(rep.Rows)}, dataio.ExportReceiptsXLSX(rep.Rows, path)
}
