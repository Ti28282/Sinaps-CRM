// Package dataio — экспорт и импорт клиентов: CSV, Excel (XLSX) и копия базы SQLite.
// Работает только с локальной базой CRM, Telegram не трогает.
package dataio

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"

	"sinaps-crm/internal/store"
)

// Колонки выгрузки клиентов. Порядок = порядок в файле.
var clientCols = []string{
	"ID", "Канал", "ID в канале", "Название", "Username", "Телефон",
	"Этап", "Сумма", "Следующий шаг", "Теги", "Заметка",
	"Последнее сообщение", "Дата последнего", "Добавлен",
}

func channelTitle(c string) string {
	switch c {
	case "tg":
		return "Telegram"
	case "wa":
		return "WhatsApp"
	case "mail":
		return "Gmail"
	case "manual":
		return "Вручную"
	}
	return c
}

func channelID(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "telegram", "tg", "телеграм":
		return "tg"
	case "whatsapp", "wa", "ватсап":
		return "wa"
	case "gmail", "mail", "почта", "email":
		return "mail"
	case "вручную", "manual", "":
		return "manual"
	}
	return strings.ToLower(strings.TrimSpace(v))
}

func ts(unix int64) string {
	if unix == 0 {
		return ""
	}
	return time.Unix(unix, 0).Format("2006-01-02 15:04")
}

func clientRow(c store.Chat) []string {
	return []string{
		strconv.FormatInt(c.ID, 10), channelTitle(c.Channel), c.ExtID, c.Title, c.Username, c.Phone,
		store.StageName(c.Stage), strconv.FormatInt(c.Amount, 10), c.NextStep, c.Tags, c.Note,
		c.LastMsg, ts(c.LastAt), ts(c.CreatedAt),
	}
}

// ---------- экспорт ----------

// ExportCSV: UTF-8 с BOM и разделителем «;» — так файл сразу правильно
// открывается в русском Excel и в Google Таблицах.
func ExportCSV(ctx context.Context, st *store.Store, path string) (int, error) {
	chats, err := st.Chats(ctx, "all", "")
	if err != nil {
		return 0, err
	}
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	_ = w.Write(clientCols)
	for _, c := range chats {
		_ = w.Write(clientRow(c))
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return 0, err
	}
	return len(chats), writeAtomic(path, buf.Bytes())
}

// ExportXLSX: лист «Клиенты» и лист «Сообщения».
func ExportXLSX(ctx context.Context, st *store.Store, path string) (int, error) {
	chats, err := st.Chats(ctx, "all", "")
	if err != nil {
		return 0, err
	}
	f := excelize.NewFile()
	defer f.Close()

	const sh = "Клиенты"
	if err := f.SetSheetName("Sheet1", sh); err != nil {
		return 0, err
	}
	sw, err := f.NewStreamWriter(sh)
	if err != nil {
		return 0, err
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	_ = sw.SetColWidth(1, 1, 8)
	_ = sw.SetColWidth(4, 4, 32)
	_ = sw.SetColWidth(9, 13, 28)
	if err := sw.SetRow("A1", cells(clientCols, bold)); err != nil {
		return 0, err
	}
	for i, c := range chats {
		row := clientRow(c)
		vals := make([]any, len(row))
		for j, v := range row {
			vals[j] = v
		}
		vals[0], vals[7] = c.ID, c.Amount // числа — числами, чтобы суммировались
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := sw.SetRow(cell, vals); err != nil {
			return 0, err
		}
	}
	if err := sw.Flush(); err != nil {
		return 0, err
	}
	_ = f.SetPanes(sh, &excelize.Panes{Freeze: true, Split: false, XSplit: 0, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	if len(chats) > 0 {
		last, _ := excelize.CoordinatesToCellName(len(clientCols), len(chats)+1)
		_ = f.AutoFilter(sh, "A1:"+last, nil)
	}

	const ms = "Сообщения"
	if _, err := f.NewSheet(ms); err != nil {
		return 0, err
	}
	mw, err := f.NewStreamWriter(ms)
	if err != nil {
		return 0, err
	}
	_ = mw.SetColWidth(1, 1, 30)
	_ = mw.SetColWidth(5, 5, 80)
	if err := mw.SetRow("A1", cells([]string{"Клиент", "Дата", "Кто", "Тип", "Текст", "Файл", "Удалено клиентом"}, bold)); err != nil {
		return 0, err
	}
	r := 2
	err = st.EachMessage(ctx, func(title string, m store.Message) error {
		if r > 1_048_576 { // предел строк Excel
			return nil
		}
		who := "Клиент"
		if m.Out {
			who = "Я"
		}
		kind := map[string]string{"": "текст", "photo": "фото", "file": "файл"}[m.MediaKind]
		del := ""
		if m.Deleted {
			del = "да"
		}
		cell, _ := excelize.CoordinatesToCellName(1, r)
		r++
		return mw.SetRow(cell, []any{title, ts(m.Date), who, kind, m.Text, m.FileName, del})
	})
	if err != nil {
		return 0, err
	}
	if err := mw.Flush(); err != nil {
		return 0, err
	}
	f.SetActiveSheet(0)

	// excelize проверяет расширение, поэтому временный файл тоже .xlsx
	tmp := strings.TrimSuffix(path, filepath.Ext(path)) + ".tmp.xlsx"
	if err := f.SaveAs(tmp); err != nil {
		return 0, err
	}
	return len(chats), os.Rename(tmp, path)
}

// ExportSQLite — полная копия базы (клиенты, переписка, настройки).
func ExportSQLite(ctx context.Context, st *store.Store, path string) error {
	return st.BackupTo(ctx, path)
}

func cells(names []string, style int) []any {
	out := make([]any, len(names))
	for i, n := range names {
		out[i] = excelize.Cell{StyleID: style, Value: n}
	}
	return out
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------- импорт ----------

// Preview — что произойдёт при импорте файла. Ничего не меняет.
type Preview struct {
	Kind     string     `json:"kind"` // table | sqlite
	File     string     `json:"file"`
	Rows     int        `json:"rows"`
	Create   int        `json:"create"`
	Update   int        `json:"update"`
	Skip     int        `json:"skip"`
	Problems []string   `json:"problems"`
	Sample   [][]string `json:"sample"` // первые строки: название, этап, сумма, действие
	Chats    int        `json:"chats"`  // для sqlite
	Messages int        `json:"messages"`
}

type Result struct {
	Created  int      `json:"created"`
	Updated  int      `json:"updated"`
	Skipped  int      `json:"skipped"`
	Problems []string `json:"problems"`
}

// Kind по расширению: table (csv/xlsx) или sqlite.
func Kind(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv", ".tsv", ".txt":
		return "csv", nil
	case ".xlsx", ".xlsm":
		return "xlsx", nil
	case ".db", ".sqlite", ".sqlite3":
		return "sqlite", nil
	case ".xls":
		return "", errors.New("старый формат .xls не поддерживается — сохраните файл как .xlsx или .csv")
	}
	return "", errors.New("поддерживаются .csv, .xlsx и .db / .sqlite")
}

// ReadTable читает CSV или первый лист Excel в строки (первая — заголовок).
func ReadTable(path string) ([][]string, error) {
	k, err := Kind(path)
	if err != nil {
		return nil, err
	}
	if k == "xlsx" {
		f, err := excelize.OpenFile(path)
		if err != nil {
			return nil, fmt.Errorf("не удалось открыть Excel: %w", err)
		}
		defer f.Close()
		sheet := f.GetSheetName(0)
		for _, n := range f.GetSheetList() { // своя выгрузка — берём лист «Клиенты»
			if n == "Клиенты" {
				sheet = n
			}
		}
		return f.GetRows(sheet)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(raw) { // русский Excel сохраняет CSV в Windows-1251
		if dec, err := charmap.Windows1251.NewDecoder().Bytes(raw); err == nil {
			raw = dec
		}
	}
	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = sniffComma(raw)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var out [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ошибка в CSV: %w", err)
		}
		out = append(out, rec)
	}
	return out, nil
}

func sniffComma(b []byte) rune {
	line := b
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		line = b[:i]
	}
	best, n := ',', bytes.Count(line, []byte(","))
	for _, c := range []rune{';', '\t'} {
		if k := bytes.Count(line, []byte(string(c))); k > n {
			best, n = c, k
		}
	}
	return best
}

// заголовки, которые понимаем (в нижнем регистре)
var headerMap = map[string]string{
	"канал": "channel", "channel": "channel",
	"id в канале": "ext", "ext_id": "ext", "extid": "ext",
	"название": "title", "имя": "title", "клиент": "title", "компания": "title", "title": "title", "name": "title",
	"username": "username", "юзернейм": "username", "логин": "username", "telegram": "username",
	"телефон": "phone", "phone": "phone", "тел": "phone", "номер": "phone",
	"этап": "stage", "stage": "stage", "статус": "stage",
	"сумма": "amount", "amount": "amount", "бюджет": "amount", "чек": "amount",
	"следующий шаг": "next", "next_step": "next", "next step": "next",
	"теги": "tags", "tags": "tags", "метки": "tags",
	"заметка": "note", "note": "note", "комментарий": "note", "примечание": "note",
}

type mapped struct {
	row  store.ImportRow
	line int
	err  string
}

func mapRows(rows [][]string) ([]mapped, error) {
	if len(rows) < 2 {
		return nil, errors.New("в файле нет строк с данными")
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		if k, ok := headerMap[strings.ToLower(strings.TrimSpace(h))]; ok {
			if _, dup := idx[k]; !dup {
				idx[k] = i
			}
		}
	}
	if _, ok := idx["title"]; !ok {
		if _, ok2 := idx["phone"]; !ok2 {
			return nil, errors.New("не нашёл колонку «Название» (или «Имя», «Телефон») в первой строке")
		}
	}
	get := func(r []string, k string) string {
		if i, ok := idx[k]; ok && i < len(r) {
			return strings.TrimSpace(r[i])
		}
		return ""
	}
	var out []mapped
	for n, r := range rows[1:] {
		empty := true
		for _, v := range r {
			if strings.TrimSpace(v) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		m := mapped{line: n + 2}
		m.row = store.ImportRow{
			Channel: channelID(get(r, "channel")), ExtID: get(r, "ext"),
			Title: get(r, "title"), Username: get(r, "username"), Phone: get(r, "phone"),
			NextStep: get(r, "next"), Note: get(r, "note"), Tags: get(r, "tags"),
		}
		if m.row.Title == "" {
			m.row.Title = m.row.Phone
		}
		if st, ok := store.ParseStage(get(r, "stage")); ok {
			m.row.Stage = st
		} else {
			m.err = fmt.Sprintf("строка %d: неизвестный этап «%s» — этап не изменится", m.line, get(r, "stage"))
		}
		if a := get(r, "amount"); a != "" {
			d := strings.Map(func(c rune) rune {
				if c >= '0' && c <= '9' {
					return c
				}
				if c == ',' || c == '.' {
					return '.'
				}
				return -1
			}, a)
			if i := strings.IndexByte(d, '.'); i >= 0 {
				d = d[:i]
			}
			if v, err := strconv.ParseInt(d, 10, 64); err == nil {
				m.row.Amount = &v
			}
		}
		if m.row.Title == "" {
			m.err = fmt.Sprintf("строка %d: нет названия — пропущена", m.line)
		}
		out = append(out, m)
	}
	return out, nil
}

func PreviewTable(ctx context.Context, st *store.Store, path string) (Preview, error) {
	p := Preview{Kind: "table", File: filepath.Base(path), Problems: []string{}, Sample: [][]string{}}
	rows, err := ReadTable(path)
	if err != nil {
		return p, err
	}
	ms, err := mapRows(rows)
	if err != nil {
		return p, err
	}
	p.Rows = len(ms)
	for _, m := range ms {
		action := ""
		switch {
		case m.row.Title == "":
			p.Skip++
			action = "пропуск"
		case st.FindImportTarget(ctx, m.row) != 0:
			p.Update++
			action = "обновить"
		default:
			p.Create++
			action = "новый"
		}
		if m.err != "" && len(p.Problems) < 20 {
			p.Problems = append(p.Problems, m.err)
		}
		if len(p.Sample) < 8 {
			amount := ""
			if m.row.Amount != nil {
				amount = strconv.FormatInt(*m.row.Amount, 10)
			}
			p.Sample = append(p.Sample, []string{m.row.Title, store.StageName(m.row.Stage), amount, action})
		}
	}
	return p, nil
}

func ApplyTable(ctx context.Context, st *store.Store, path string) (Result, error) {
	res := Result{Problems: []string{}}
	rows, err := ReadTable(path)
	if err != nil {
		return res, err
	}
	ms, err := mapRows(rows)
	if err != nil {
		return res, err
	}
	for _, m := range ms {
		if m.row.Title == "" {
			res.Skipped++
			continue
		}
		created, err := st.ApplyImport(ctx, m.row)
		if err != nil {
			res.Skipped++
			if len(res.Problems) < 20 {
				res.Problems = append(res.Problems, fmt.Sprintf("строка %d: %v", m.line, err))
			}
			continue
		}
		if created {
			res.Created++
		} else {
			res.Updated++
		}
	}
	return res, nil
}

func PreviewSQLite(path string) (Preview, error) {
	c, m, err := store.ValidateDB(path)
	if err != nil {
		return Preview{}, err
	}
	return Preview{Kind: "sqlite", File: filepath.Base(path), Chats: c, Messages: m, Problems: []string{}, Sample: [][]string{}}, nil
}
