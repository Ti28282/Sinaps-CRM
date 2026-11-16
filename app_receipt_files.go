package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sinaps-crm/internal/store"
)

// ---------- плагин «Чеки»: файл чека ----------
//
// К чеку можно приложить PDF или фото: с компьютера или из переписки клиента.
// CRM хранит копию в папке данных (receipts/), поэтому файл остаётся, даже если
// сообщение в переписке удалят или файл на компьютере переместят.

const maxReceiptFile = 30 << 20

func (a *App) receiptsDir() string { return filepath.Join(a.dataDir, "receipts") }

// ReceiptFileSrc — откуда взять файл: Path (с компьютера) или ChatID+MsgID (из переписки).
type ReceiptFileSrc struct {
	Path   string `json:"path"`
	ChatID int64  `json:"chatId"`
	MsgID  int64  `json:"msgId"`
}

// ChatFile — файл или фото в переписке с клиентом.
type ChatFile struct {
	MsgID int64  `json:"msgId"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Date  int64  `json:"date"`
	Kind  string `json:"kind"` // photo | file
	Out   bool   `json:"out"`
}

// PickReceiptFile — выбор файла чека на компьютере. Пустой Path — отмена.
func (a *App) PickReceiptFile() (FileInfo, error) {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Файл чека",
		Filters: []runtime.FileFilter{
			{DisplayName: "PDF и фото", Pattern: "*.pdf;*.jpg;*.jpeg;*.png;*.webp;*.heic"},
			{DisplayName: "Все файлы", Pattern: "*.*"},
		},
	})
	if err != nil || p == "" {
		return FileInfo{}, err
	}
	return a.FileInfo(p)
}

// ReceiptChatFiles — последние файлы и фото из переписки клиента, новые сверху.
func (a *App) ReceiptChatFiles(chatID int64) ([]ChatFile, error) {
	if !a.pluginActive(chekiID) {
		return nil, errChekiOff
	}
	ms, err := a.st.MediaMessages(a.ctx, chatID, 60)
	if err != nil {
		return nil, err
	}
	out := []ChatFile{}
	for _, m := range ms {
		name := m.FileName
		if m.MediaKind == "photo" {
			name = "Фото " + time.Unix(m.Date, 0).Format("02.01.2006") + ".jpg"
		}
		out = append(out, ChatFile{MsgID: m.MsgID, Name: name, Size: m.FileSize, Date: m.Date, Kind: m.MediaKind, Out: m.Out})
	}
	return out, nil
}

// AttachReceiptFile прикладывает (или заменяет) файл чека.
func (a *App) AttachReceiptFile(id int64, src ReceiptFileSrc) (store.Receipt, error) {
	if !a.pluginActive(chekiID) {
		return store.Receipt{}, errChekiOff
	}
	r, err := a.st.ReceiptByID(a.ctx, id)
	if err != nil {
		return r, err
	}
	var from, name, kind string
	var msgID int64
	switch {
	case src.Path != "":
		from, name, kind = src.Path, filepath.Base(src.Path), "pc"
	case src.MsgID != 0:
		chatID := src.ChatID
		if chatID == 0 {
			chatID = r.ChatID
		}
		if from, name, err = a.chatAttachment(chatID, src.MsgID); err != nil {
			return r, err
		}
		c, _ := a.st.Chat(a.ctx, chatID)
		kind, msgID = c.Channel, src.MsgID
	default:
		return r, errors.New("выберите файл")
	}
	copyName, size, err := a.copyReceiptFile(id, from, name)
	if err != nil {
		return r, err
	}
	if err := a.st.SetReceiptFile(a.ctx, id, name, copyName, size, kind, msgID); err != nil {
		_ = os.Remove(filepath.Join(a.receiptsDir(), copyName))
		return r, err
	}
	if r.FilePath != "" && r.FilePath != copyName {
		a.dropReceiptCopy(r.FilePath) // заменили файл — старая копия не нужна
	}
	_ = a.st.AddEvent(a.ctx, r.ChatID, "receipt", "Файл чека · "+name)
	a.emit("receipts:changed", r.ChatID)
	return a.st.ReceiptByID(a.ctx, id)
}

// RemoveReceiptFile убирает файл у чека. Сам чек и переписка не меняются.
func (a *App) RemoveReceiptFile(id int64) (store.Receipt, error) {
	if !a.pluginActive(chekiID) {
		return store.Receipt{}, errChekiOff
	}
	r, err := a.st.ReceiptByID(a.ctx, id)
	if err != nil {
		return r, err
	}
	if err := a.st.ClearReceiptFile(a.ctx, id); err != nil {
		return r, err
	}
	a.dropReceiptCopy(r.FilePath)
	a.emit("receipts:changed", r.ChatID)
	return a.st.ReceiptByID(a.ctx, id)
}

// OpenReceiptFile открывает файл чека программой по умолчанию.
func (a *App) OpenReceiptFile(id int64) error {
	p, err := a.receiptFile(a.ctx, id)
	if err != nil {
		return err
	}
	return openPath(p)
}

// RevealReceiptFile открывает папку, где лежит копия файла чека.
func (a *App) RevealReceiptFile(id int64) error {
	p, err := a.receiptFile(a.ctx, id)
	if err != nil {
		return err
	}
	return openPath(filepath.Dir(p))
}

// receiptFile — путь к копии файла чека. Чеки из 0.2 без копии: скачиваем файл из сообщения один раз.
func (a *App) receiptFile(ctx context.Context, id int64) (string, error) {
	r, err := a.st.ReceiptByID(ctx, id)
	if err != nil {
		return "", err
	}
	if r.FilePath != "" {
		p := filepath.Join(a.receiptsDir(), r.FilePath)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if r.MsgID == 0 {
		if r.FilePath != "" {
			return "", errors.New("файл чека пропал из папки данных CRM — приложите его заново")
		}
		return "", errors.New("у чека нет файла")
	}
	from, name, err := a.chatAttachment(r.ChatID, r.MsgID)
	if err != nil {
		return "", err
	}
	copyName, size, err := a.copyReceiptFile(id, from, name)
	if err != nil {
		return "", err
	}
	src := r.FileSrc
	if src == "" {
		src = r.Channel
	}
	if err := a.st.SetReceiptFile(ctx, id, name, copyName, size, src, r.MsgID); err != nil {
		return "", err
	}
	return filepath.Join(a.receiptsDir(), copyName), nil
}

// chatAttachment скачивает файл или фото из сообщения (если ещё не скачан) и отдаёт путь и имя.
func (a *App) chatAttachment(chatID, msgID int64) (path, name string, err error) {
	m, err := a.st.MessageByMsg(a.ctx, chatID, msgID)
	if err != nil || (m.MediaKind != "file" && m.MediaKind != "photo") {
		return "", "", errors.New("в сообщении нет файла")
	}
	c, err := a.st.Chat(a.ctx, chatID)
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(a.dataDir, "media", strconv.FormatInt(chatID, 10))
	if m.MediaKind == "photo" {
		path = filepath.Join(dir, strconv.FormatInt(msgID, 10)+".jpg")
		name = "Фото " + time.Unix(m.Date, 0).Format("02.01.2006") + ".jpg"
	} else {
		name = safeName(m.FileName)
		path = filepath.Join(dir, fmt.Sprintf("%d_%s", msgID, name))
	}
	if _, err := os.Stat(path); err != nil {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
		defer cancel()
		if err := a.downloadMedia(ctx, c, m, path); err != nil {
			return "", "", fmt.Errorf("не удалось скачать файл из переписки: %w", err)
		}
	}
	return path, name, nil
}

// copyReceiptFile кладёт копию в receipts/<id>-<случайно>-<имя>. Возвращает имя копии и размер.
func (a *App) copyReceiptFile(id int64, from, name string) (string, int64, error) {
	st, err := os.Stat(from)
	if err != nil {
		return "", 0, errors.New("файл не найден")
	}
	if st.IsDir() {
		return "", 0, errors.New("это папка — выберите файл")
	}
	if st.Size() > maxReceiptFile {
		return "", 0, fmt.Errorf("файл больше %d МБ — для чека это слишком много", maxReceiptFile>>20)
	}
	if err := os.MkdirAll(a.receiptsDir(), 0o700); err != nil {
		return "", 0, err
	}
	rnd := make([]byte, 4)
	_, _ = rand.Read(rnd)
	copyName := fmt.Sprintf("%d-%s-%s", id, hex.EncodeToString(rnd), safeName(name))
	in, err := os.Open(from)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	dst := filepath.Join(a.receiptsDir(), copyName)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", 0, err
	}
	return copyName, n, nil
}

// dropReceiptCopy удаляет копию файла, если на неё больше не ссылается ни один чек.
func (a *App) dropReceiptCopy(name string) {
	if name == "" || filepath.Base(name) != name {
		return
	}
	if used, err := a.st.ReceiptFilePaths(a.ctx); err == nil && !used[name] {
		_ = os.Remove(filepath.Join(a.receiptsDir(), name))
	}
}

// serveReceiptFile — /cheki-file/<id>: файл для окна просмотра (картинка или PDF).
func (a *App) serveReceiptFile(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || a.st == nil || !a.pluginActive(chekiID) {
		http.NotFound(w, r)
		return
	}
	p, err := a.receiptFile(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if strings.EqualFold(filepath.Ext(p), ".pdf") {
		w.Header().Set("Content-Type", "application/pdf")
	}
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, p)
}
