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
// Чек — один платёж клиента, разбитый на части: «В счёт проекта» уменьшает остаток
// по проекту клиента, остальные части (правки, поддержка…) — сверх проекта.
// Работает только внутри CRM: ничего не отправляет клиентам и не меняет переписку.

var errChekiOff = errors.New("плагин «Чеки» выключен — включите его в «Настройки → Плагины»")

// chekiSettings — настройки плагина с запасными значениями.
type chekiSettings struct {
	Currency    string // символ: ₽, сом, ₸, $
	SumToDeal   bool
	FirstToDeal bool
	CustomTypes []string // свои типы частей, добавленные на странице «Чеки»
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
	var custom []string
	if list, ok := s["customTypes"].([]any); ok {
		for _, v := range list {
			if t, ok := v.(string); ok && strings.TrimSpace(t) != "" {
				custom = append(custom, strings.TrimSpace(t))
			}
		}
	}
	return chekiSettings{
		Currency:    currencySymbol(str("currency", "₽ рубль")),
		SumToDeal:   flag("sumToDeal", true),
		FirstToDeal: flag("firstToDeal", true),
		CustomTypes: custom,
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

// ---------- типы частей ----------

// ReceiptType — тип части чека.
type ReceiptType struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Short     string `json:"short"`
	Color     string `json:"color"`
	InProject bool   `json:"inProject"` // идёт в оплату проекта клиента
}

var baseTypes = []ReceiptType{
	{ID: store.PartProject, Label: "В счёт проекта", Short: "Проект", Color: "#C8F25A", InProject: true},
	{ID: "edits", Label: "Правки", Short: "Правки", Color: "#F5B65C"},
	{ID: "site", Label: "Доп. работы по сайту", Short: "Сайт", Color: "#8CCDF5"},
	{ID: "support", Label: "Поддержка", Short: "Поддержка", Color: "#B39DFF"},
}

var customColors = []string{"#F59BB0", "#7FE0AE", "#E8A0F0", "#9DB4FF", "#F0C987", "#7CD6D0"}

// ReceiptTypes — типы частей: встроенные и свои.
func (a *App) ReceiptTypes() []ReceiptType {
	out := append([]ReceiptType{}, baseTypes...)
	for i, t := range a.cheki().CustomTypes {
		out = append(out, ReceiptType{ID: "c:" + t, Label: t, Short: t, Color: customColors[i%len(customColors)]})
	}
	return out
}

func (a *App) typeMap() map[string]ReceiptType {
	m := map[string]ReceiptType{}
	for _, t := range a.ReceiptTypes() {
		m[t.ID] = t
	}
	return m
}

// typeOf — тип по id; удалённый свой тип показывается по названию из id.
func typeOf(m map[string]ReceiptType, id string) ReceiptType {
	if t, ok := m[id]; ok {
		return t
	}
	label := strings.TrimPrefix(id, "c:")
	return ReceiptType{ID: id, Label: label, Short: label, Color: "#8A8F98"}
}

// AddReceiptType — свой тип части (например «Консультация»). Идёт сверх проекта.
func (a *App) AddReceiptType(label string) ([]ReceiptType, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return a.ReceiptTypes(), errors.New("введите название типа")
	}
	if len([]rune(label)) > 40 {
		return a.ReceiptTypes(), errors.New("название типа — не длиннее 40 символов")
	}
	for _, t := range a.ReceiptTypes() {
		if strings.EqualFold(t.Label, label) || strings.EqualFold(t.Short, label) {
			return a.ReceiptTypes(), nil // уже есть
		}
	}
	settings := a.PluginSettings(chekiID)
	list := []any{}
	for _, t := range a.cheki().CustomTypes {
		list = append(list, t)
	}
	settings["customTypes"] = append(list, label)
	if err := a.SavePluginSettings(chekiID, settings); err != nil {
		return a.ReceiptTypes(), err
	}
	return a.ReceiptTypes(), nil
}

// ---------- проект клиента ----------

// ProjectView — проект клиента: стоимость, сколько оплачено частями «В счёт проекта»
// и сколько клиент заплатил сверх проекта.
type ProjectView struct {
	store.Project
	Paid  int64 `json:"paid"`
	Left  int64 `json:"left"`
	Extra int64 `json:"extra"`
}

func (a *App) ChekiProject(chatID int64) (ProjectView, error) {
	if !a.pluginActive(chekiID) {
		return ProjectView{}, errChekiOff
	}
	p, err := a.st.Project(a.ctx, chatID)
	if err != nil {
		return ProjectView{}, err
	}
	v := ProjectView{Project: p}
	list, err := a.st.Receipts(a.ctx, chatID, 0)
	if err != nil {
		return v, err
	}
	for _, r := range list {
		if r.Status != "paid" {
			continue
		}
		for _, part := range r.Parts {
			if part.Kind == store.PartProject {
				v.Paid += part.Amount
			} else {
				v.Extra += part.Amount
			}
		}
	}
	if p.Price > v.Paid {
		v.Left = p.Price - v.Paid
	}
	return v, nil
}

// SetChekiProject — название и полная стоимость проекта клиента.
func (a *App) SetChekiProject(chatID int64, name string, price int64) (ProjectView, error) {
	if !a.pluginActive(chekiID) {
		return ProjectView{}, errChekiOff
	}
	if _, err := a.st.Chat(a.ctx, chatID); err != nil {
		return ProjectView{}, errors.New("клиент не найден")
	}
	if err := a.st.SetProject(a.ctx, store.Project{ChatID: chatID, Name: name, Price: price}); err != nil {
		return ProjectView{}, err
	}
	_ = a.st.AddEvent(a.ctx, chatID, "receipt", fmt.Sprintf("Проект: %s · %s", strings.TrimSpace(name), fmtMoney(price, a.cheki().Currency)))
	a.emit("receipts:changed", chatID)
	return a.ChekiProject(chatID)
}

// ---------- чеки ----------

// ReceiptReq — форма «Новый чек» / «Изменить чек».
type ReceiptReq struct {
	ChatID int64               `json:"chatId"`
	Date   string              `json:"date"`   // 2026-11-09; пусто — сегодня
	Status string              `json:"status"` // paid | wait | refund
	Parts  []store.ReceiptPart `json:"parts"`
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
	var sum int64
	for _, p := range r.Parts {
		sum += p.Amount
	}
	if sum <= 0 && r.Status != "wait" {
		return errors.New("укажите сумму хотя бы в одной части чека")
	}
	return nil
}

// CreateReceipt — новый чек: клиент из воронки, части, дата, статус.
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
		ChatID: req.ChatID, Currency: cfg.Currency, Status: req.Status, PaidAt: paidAt, Parts: req.Parts,
	})
	if err != nil {
		return r, err
	}
	_ = a.st.AddEvent(a.ctx, r.ChatID, "receipt", a.receiptEvent("Чек", r))
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
	r, err := a.st.UpdateReceipt(a.ctx, store.Receipt{ID: id, Status: req.Status, PaidAt: paidAt, Parts: req.Parts, Currency: old.Currency})
	if err != nil {
		return r, err
	}
	_ = a.st.AddEvent(a.ctx, r.ChatID, "receipt", a.receiptEvent("Чек изменён", r))
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

// receiptEvent — запись в историю клиента: «Чек · 7 200 ₽ · оплачен · Проект 5 000 + Правки 2 200».
func (a *App) receiptEvent(verb string, r store.Receipt) string {
	s := fmt.Sprintf("%s · %s · %s", verb, fmtMoney(r.Amount, r.Currency), store.ReceiptStatusName(r.Status))
	if len(r.Parts) > 1 {
		tm := a.typeMap()
		var ps []string
		for _, p := range r.Parts {
			ps = append(ps, typeOf(tm, p.Kind).Short+" "+fmtMoney(p.Amount, ""))
		}
		s += " · " + strings.Join(ps, " + ")
	} else if r.What != "" {
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

// fmtMoney: 7200, "₽" → «7 200 ₽»; cur "" — без валюты.
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
		return b.String()
	}
	return b.String() + " " + cur
}

// ---------- страница «Чеки» ----------

type ReceiptsQuery struct {
	ChatID int64  `json:"chatId"` // 0 — все клиенты из воронки
	Period string `json:"period"` // quarter | year | all
	Kind   string `json:"kind"`   // тип части; пусто — все
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

// TypeSum — блок «По типам»: оплачено частями этого типа.
type TypeSum struct {
	ReceiptType
	Sum   int64 `json:"sum"`
	Count int   `json:"count"`
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
	Types    []TypeSum       `json:"types"`
	AllTypes []ReceiptType   `json:"allTypes"`
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

// sumOf — сумма чека с учётом фильтра по типу части.
func sumOf(r store.Receipt, kind string) (int64, bool) {
	if kind == "" {
		return r.Amount, true
	}
	var s int64
	found := false
	for _, p := range r.Parts {
		if p.Kind == kind {
			s += p.Amount
			found = true
		}
	}
	return s, found
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
	types := a.ReceiptTypes()
	tm := a.typeMap()
	rep := ReceiptsReport{Rows: []store.Receipt{}, Months: []MonthSum{}, Top: []ClientSum{}, Clients: []ClientSum{},
		Types: []TypeSum{}, AllTypes: types, Currency: a.cheki().Currency, Year: now.Year()}
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
	byType := map[string]*TypeSum{}
	var typeOrder []string
	for _, t := range types {
		byType[t.ID] = &TypeSum{ReceiptType: t}
		typeOrder = append(typeOrder, t.ID)
	}
	paid := 0
	for _, r := range all {
		if clients[r.ChatID] == nil {
			clients[r.ChatID] = &ClientSum{ChatID: r.ChatID, Title: r.ChatTitle, Channel: r.Channel}
		}
		inPeriod := r.PaidAt >= from
		sum, has := sumOf(r, q.Kind)
		if r.Status == "paid" {
			clients[r.ChatID].Sum += r.Amount
			if inPeriod && has {
				if top[r.ChatID] == nil {
					top[r.ChatID] = &ClientSum{ChatID: r.ChatID, Title: r.ChatTitle, Channel: r.Channel}
				}
				top[r.ChatID].Sum += sum
			}
		}
		if q.ChatID != 0 && r.ChatID != q.ChatID {
			continue
		}
		// «По типам» — по клиенту из фильтра и периоду, без фильтра по типу
		if inPeriod && r.Status == "paid" {
			for _, p := range r.Parts {
				t := byType[p.Kind]
				if t == nil {
					t = &TypeSum{ReceiptType: typeOf(tm, p.Kind)}
					byType[p.Kind] = t
					typeOrder = append(typeOrder, p.Kind)
				}
				t.Sum += p.Amount
				t.Count++
			}
		}
		if !has {
			continue
		}
		if r.Status == "paid" {
			if i, ok := idx[time.Unix(r.PaidAt, 0).Format("2006-01")]; ok {
				rep.Months[i].Sum += sum
			}
		}
		if !inPeriod {
			continue
		}
		rep.Rows = append(rep.Rows, r)
		switch r.Status {
		case "paid":
			rep.Total += sum
			paid++
		case "wait":
			rep.Wait += sum
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
	for _, id := range typeOrder {
		if t := byType[id]; t.Sum > 0 || !strings.HasPrefix(id, "c:") {
			rep.Types = append(rep.Types, *t)
		}
	}
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

// ExportReceipts — выгрузка чеков в Excel: лист «Чеки» и лист «Части». Пустой Path — отмена.
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
	tm := a.typeMap()
	label := func(kind string) (string, bool) { t := typeOf(tm, kind); return t.Label, t.InProject }
	return ExportResult{Path: path, Count: len(rep.Rows)}, dataio.ExportReceiptsXLSX(rep.Rows, label, path)
}
