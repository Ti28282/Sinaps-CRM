// Package plugins — плагины CRM в виде архивов .sinaps в отдельной папке.
//
// Плагин — это zip-архив с расширением .sinaps:
//
//	manifest.json   id, название, версия, зависимости, нужная версия CRM
//	settings.json   поля окна «Настройки» плагина (необязательно)
//	icon.svg        иконка (необязательно)
//	plugin.wasm     свой код плагина (поддержка — в следующих версиях)
//
// Положили файл в папку — плагин появился в списке. Удалили — пропал,
// его данные в базе остаются. Включён плагин или нет, хранится в config.json.
// Остальные файлы в папке не показываются.
package plugins

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Ext — расширение файла плагина.
const Ext = ".sinaps"

// maxArchive — архив больше этого размера не читаем (защита от мусора в папке).
const maxArchive = 64 << 20

// Статусы плагина в списке.
const (
	StatusOn      = "on"      // подключён: включён и работает
	StatusOff     = "off"     // выключен
	StatusWait    = "wait"    // включён, но ждёт другой плагин (например «Чеки»)
	StatusError   = "error"   // это плагин, но запустить нельзя
	StatusInvalid = "invalid" // не плагин: нет manifest.json или битый архив
)

// Field — одно поле окна настроек плагина (из settings.json).
type Field struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"` // select | bool | text | number
	Options []string `json:"options,omitempty"`
	Default any      `json:"default,omitempty"`
}

// Manifest — manifest.json внутри архива.
type Manifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Requires    []string `json:"requires"`
	CRM         string   `json:"crm"`  // ">=0.2" — минимальная версия CRM
	Icon        string   `json:"icon"` // имя встроенной иконки: receipt | puzzle | spark
}

// Entry — строка в списке плагинов.
type Entry struct {
	File     string   `json:"file"`
	Status   string   `json:"status"`
	Error    string   `json:"error,omitempty"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Desc     string   `json:"description"`
	Icon     string   `json:"icon"`
	Requires []string `json:"requires"`
	// RequiresNames — названия плагинов из requires, для подписи «Ждёт «Чеки»».
	RequiresNames []string `json:"requiresNames"`
	Fields        []Field  `json:"fields"`
	Enabled       bool     `json:"enabled"`
}

// Usable — архив прочитан и плагин может работать в этой CRM.
func (e Entry) Usable() bool {
	return e.Status == StatusOn || e.Status == StatusOff || e.Status == StatusWait
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Scan читает папку и возвращает плагины в порядке имён файлов.
//
// crm — версия CRM ("0.2.0"); builtin — id плагинов, которые эта версия CRM
// умеет выполнять; enabled — что включено в config.json.
func Scan(dir, crm string, builtin map[string]bool, enabled map[string]bool) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, de := range des {
		if de.IsDir() || !strings.EqualFold(filepath.Ext(de.Name()), Ext) {
			continue // смотрим только архивы .sinaps
		}
		names = append(names, de.Name())
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })

	out := make([]Entry, 0, len(names))
	seen := map[string]string{} // id → файл
	for _, name := range names {
		e := read(filepath.Join(dir, name))
		e.File = name
		if e.Status == "" {
			switch {
			case seen[e.ID] != "":
				e.Status, e.Error = StatusError, fmt.Sprintf("Плагин с id «%s» уже есть: %s", e.ID, seen[e.ID])
			case !versionOK(crm, e.crmReq()):
				e.Status, e.Error = StatusError, "Нужна CRM v"+strings.TrimLeft(e.crmReq(), ">= ")+" или новее"
			case !builtin[e.ID] && e.hasWasm:
				e.Status, e.Error = StatusError, "Плагины со своим кодом (plugin.wasm) заработают в следующей версии CRM"
			case !builtin[e.ID]:
				e.Status, e.Error = StatusError, "Эта версия CRM не умеет запускать плагин «"+e.ID+"»"
			}
		}
		if e.ID != "" && seen[e.ID] == "" {
			seen[e.ID] = name
		}
		out = append(out, e.Entry)
	}
	resolve(out, enabled)
	return out, nil
}

type scanned struct {
	Entry
	crm     string
	hasWasm bool
}

func (s scanned) crmReq() string { return s.crm }

// read открывает один архив. Пустой Status — архив годный, решение дальше.
func read(path string) scanned {
	bad := func(msg string) scanned {
		return scanned{Entry: Entry{Status: StatusInvalid, Error: msg, Requires: []string{}, RequiresNames: []string{}, Fields: []Field{}}}
	}
	st, err := os.Stat(path)
	if err != nil {
		return bad("Не удалось прочитать файл")
	}
	if st.Size() > maxArchive {
		return bad("Файл слишком большой для плагина")
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return bad("Файл повреждён или это не zip-архив")
	}
	defer zr.Close()

	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[strings.TrimPrefix(filepath.ToSlash(f.Name), "./")] = f
	}
	mf := files["manifest.json"]
	if mf == nil {
		return bad("В архиве нет manifest.json")
	}
	var m Manifest
	if err := readJSON(mf, &m); err != nil {
		return bad("manifest.json не читается: " + err.Error())
	}
	m.ID = strings.TrimSpace(m.ID)
	m.Name = strings.TrimSpace(m.Name)
	if !idRe.MatchString(m.ID) {
		return bad("В manifest.json неверный id (латиница, цифры, - и _)")
	}
	if m.Name == "" {
		return bad("В manifest.json не указано название (name)")
	}
	s := scanned{
		Entry: Entry{
			ID: m.ID, Name: m.Name, Version: m.Version, Desc: m.Description, Icon: m.Icon,
			Requires: []string{}, RequiresNames: []string{}, Fields: []Field{},
		},
		crm:     m.CRM,
		hasWasm: files["plugin.wasm"] != nil,
	}
	for _, r := range m.Requires {
		if r = strings.TrimSpace(r); r != "" && r != m.ID {
			s.Requires = append(s.Requires, r)
		}
	}
	if sf := files["settings.json"]; sf != nil {
		var doc struct {
			Fields []Field `json:"fields"`
		}
		if err := readJSON(sf, &doc); err != nil {
			s.Status, s.Error = StatusError, "settings.json не читается: "+err.Error()
			return s
		}
		for _, f := range doc.Fields {
			if f.Key != "" {
				if f.Options == nil {
					f.Options = []string{}
				}
				s.Fields = append(s.Fields, f)
			}
		}
	}
	return s
}

func readJSON(f *zip.File, v any) error {
	if f.UncompressedSize64 > 1<<20 {
		return errors.New("файл слишком большой")
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	if err != nil {
		return err
	}
	b = []byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")) // BOM из Блокнота
	return json.Unmarshal(b, v)
}

// resolve проставляет on/off/wait с учётом зависимостей.
func resolve(list []Entry, enabled map[string]bool) {
	byID := map[string]int{}
	for i, e := range list {
		if e.Usable() || e.Status == "" {
			if _, dup := byID[e.ID]; !dup {
				byID[e.ID] = i
			}
		}
	}
	for i := range list {
		e := &list[i]
		if e.Status != "" && e.Status != StatusOn && e.Status != StatusOff && e.Status != StatusWait {
			continue
		}
		e.Enabled = enabled[e.ID]
		e.RequiresNames = e.RequiresNames[:0]
		for _, r := range e.Requires {
			if j, ok := byID[r]; ok {
				e.RequiresNames = append(e.RequiresNames, list[j].Name)
			} else {
				e.RequiresNames = append(e.RequiresNames, r)
			}
		}
	}
	// активен = годный + включён + все зависимости активны (до устойчивого состояния)
	active := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, e := range list {
			if e.Status != "" && !e.Usable() || !e.Enabled || active[e.ID] {
				continue
			}
			ok := true
			for _, r := range e.Requires {
				if !active[r] {
					ok = false
					break
				}
			}
			if ok {
				active[e.ID], changed = true, true
			}
		}
	}
	for i := range list {
		e := &list[i]
		if e.Status != "" && !e.Usable() {
			continue
		}
		switch {
		case !e.Enabled:
			e.Status, e.Error = StatusOff, ""
		case active[e.ID]:
			e.Status, e.Error = StatusOn, ""
		default:
			e.Status, e.Error = StatusWait, "Включите «"+strings.Join(e.RequiresNames, "», «")+"», чтобы плагин заработал"
		}
	}
}

// Defaults — значения полей по умолчанию из settings.json.
func Defaults(fields []Field) map[string]any {
	out := map[string]any{}
	for _, f := range fields {
		if f.Default != nil {
			out[f.Key] = f.Default
		}
	}
	return out
}

// versionOK: удовлетворяет ли версия CRM требованию ">=X.Y[.Z]" (пусто — любая).
func versionOK(have, req string) bool {
	req = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req), ">="))
	if req == "" {
		return true
	}
	return compare(parse(have), parse(req)) >= 0
}

func parse(v string) [3]int {
	var out [3]int
	for i, p := range strings.SplitN(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".", 3) {
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out[i] = n
	}
	return out
}

func compare(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Install копирует архив в папку плагинов. Имя файла сохраняется;
// если такой файл уже есть — заменяется (обновление плагина).
func Install(dir, src string) (string, error) {
	if !strings.EqualFold(filepath.Ext(src), Ext) {
		return "", errors.New("плагин — это файл с расширением .sinaps")
	}
	if e := read(src); e.Status == StatusInvalid {
		return "", errors.New(e.Error)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(src))
	if same, _ := filepath.Abs(src); same == dst {
		return filepath.Base(dst), nil
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return filepath.Base(dst), os.Rename(tmp, dst)
}
