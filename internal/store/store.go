// Package store — локальная база CRM в одном файле SQLite.
//
// Дубли исключены на уровне схемы:
//   - чат уникален по (channel, ext_id)
//   - сообщение уникально по (chat_id, msg_id)
//
// Повторный импорт обновляет имя/username чата, но не трогает данные CRM
// (этап, сумму, заметку, теги), а сообщения просто докачивает.
package store

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // чистый Go, без CGO
)

const schema = `
CREATE TABLE IF NOT EXISTS settings(
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chats(
  id          INTEGER PRIMARY KEY,
  channel     TEXT    NOT NULL,              -- tg | wa
  ext_id      TEXT    NOT NULL,              -- u123 / c45 / ch678
  kind        TEXT    NOT NULL,              -- user | chat | channel
  peer_id     INTEGER NOT NULL,
  access_hash INTEGER NOT NULL DEFAULT 0,
  title       TEXT    NOT NULL DEFAULT '',
  username    TEXT    NOT NULL DEFAULT '',
  phone       TEXT    NOT NULL DEFAULT '',
  stage       TEXT    NOT NULL DEFAULT 'new',
  amount      INTEGER NOT NULL DEFAULT 0,
  next_step   TEXT    NOT NULL DEFAULT '',
  note        TEXT    NOT NULL DEFAULT '',
  tags        TEXT    NOT NULL DEFAULT '',
  unread      INTEGER NOT NULL DEFAULT 0,
  last_msg    TEXT    NOT NULL DEFAULT '',
  last_at     INTEGER NOT NULL DEFAULT 0,
  last_out    INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,
  UNIQUE(channel, ext_id)
);

CREATE TABLE IF NOT EXISTS messages(
  id      INTEGER PRIMARY KEY,
  chat_id INTEGER NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  msg_id  INTEGER NOT NULL,
  out     INTEGER NOT NULL,
  text    TEXT    NOT NULL,
  date    INTEGER NOT NULL,
  media_kind TEXT NOT NULL DEFAULT '',     -- photo | …
  media_ref  TEXT NOT NULL DEFAULT '',     -- JSON: как скачать файл из Telegram
  deleted    INTEGER NOT NULL DEFAULT 0,   -- клиент удалил у себя — у нас остаётся
  ext_msg_id TEXT NOT NULL DEFAULT '',     -- строковый ID сообщения (WhatsApp)
  UNIQUE(chat_id, msg_id)
);
CREATE INDEX IF NOT EXISTS idx_messages_chat_date ON messages(chat_id, date);

CREATE TABLE IF NOT EXISTS events(
  id      INTEGER PRIMARY KEY,
  chat_id INTEGER NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  kind    TEXT    NOT NULL,
  text    TEXT    NOT NULL,
  at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_chat ON events(chat_id, at);

-- чаты, которые пользователь убрал из CRM: синхронизация их пропускает.
-- В Telegram при этом ничего не меняется.
CREATE TABLE IF NOT EXISTS hidden_chats(
  channel TEXT NOT NULL,
  ext_id  TEXT NOT NULL,
  title   TEXT NOT NULL DEFAULT '',
  at      INTEGER NOT NULL,
  PRIMARY KEY(channel, ext_id)
);

-- WhatsApp: все личные чаты, о которых знаем (для окна выбора клиентов),
-- и история, присланная телефоном при подключении (подтягивается при выборе чата).
CREATE TABLE IF NOT EXISTS wa_chats(
  jid      TEXT PRIMARY KEY,
  name     TEXT NOT NULL DEFAULT '',
  last_at  INTEGER NOT NULL DEFAULT 0,
  last_msg TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS wa_history(
  jid        TEXT NOT NULL,
  ext_msg_id TEXT NOT NULL,
  out        INTEGER NOT NULL,
  text       TEXT NOT NULL,
  date       INTEGER NOT NULL,
  media_kind TEXT NOT NULL DEFAULT '',
  media_ref  TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(jid, ext_msg_id)
);

CREATE TABLE IF NOT EXISTS sends(
  id      INTEGER PRIMARY KEY,
  chat_id INTEGER NOT NULL,
  kind    TEXT    NOT NULL,                  -- reply | broadcast
  at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sends_at ON sends(kind, at);
`

var Stages = []string{"new", "sent", "replied", "deal", "lost"}

type Store struct{ db *sql.DB }

type Chat struct {
	ID         int64  `json:"id"`
	Channel    string `json:"channel"`
	ExtID      string `json:"extId"`
	Kind       string `json:"kind"`
	PeerID     int64  `json:"-"`
	AccessHash int64  `json:"-"`
	Title      string `json:"title"`
	Username   string `json:"username"`
	Phone      string `json:"phone"`
	Stage      string `json:"stage"`
	Amount     int64  `json:"amount"`
	NextStep   string `json:"nextStep"`
	Note       string `json:"note"`
	Tags       string `json:"tags"`
	Unread     bool   `json:"unread"`
	LastMsg    string `json:"lastMsg"`
	LastAt     int64  `json:"lastAt"`
	LastOut    bool   `json:"lastOut"`
	CreatedAt  int64  `json:"createdAt"`
}

type Message struct {
	ID        int64  `json:"id"`
	ChatID    int64  `json:"chatId"`
	MsgID     int64  `json:"msgId"`
	Out       bool   `json:"out"`
	Text      string `json:"text"`
	Date      int64  `json:"date"`
	MediaKind string `json:"mediaKind"`
	MediaRef  string `json:"-"`
	Deleted   bool   `json:"deleted"`
	FileName  string `json:"fileName,omitempty"`
	FileSize  int64  `json:"fileSize,omitempty"`
	ExtMsgID  string `json:"-"`
}

// FillFile достаёт имя и размер файла из media_ref для показа в переписке.
func (m *Message) FillFile() {
	if m.MediaKind != "file" || m.MediaRef == "" {
		return
	}
	var r struct {
		Name  string `json:"name"`
		Bytes int64  `json:"bytes"`
	}
	if json.Unmarshal([]byte(m.MediaRef), &r) == nil {
		m.FileName, m.FileSize = r.Name, r.Bytes
	}
}

type Event struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	At   int64  `json:"at"`
}

// ChatPatch — поля CRM, которые меняет пользователь. nil = не трогать.
type ChatPatch struct {
	Stage    *string `json:"stage"`
	Amount   *int64  `json:"amount"`
	NextStep *string `json:"nextStep"`
	Note     *string `json:"note"`
	Tags     *string `json:"tags"`
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: один писатель — никаких "database is locked"
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return st, nil
}

// migrate добавляет колонки, появившиеся после первой версии базы.
func (s *Store) migrate() error {
	cols := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		cols[name] = true
	}
	rows.Close()
	for _, c := range []struct{ name, ddl string }{
		{"media_kind", `ALTER TABLE messages ADD COLUMN media_kind TEXT NOT NULL DEFAULT ''`},
		{"media_ref", `ALTER TABLE messages ADD COLUMN media_ref TEXT NOT NULL DEFAULT ''`},
		{"deleted", `ALTER TABLE messages ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0`},
		{"ext_msg_id", `ALTER TABLE messages ADD COLUMN ext_msg_id TEXT NOT NULL DEFAULT ''`},
	} {
		if !cols[c.name] {
			if _, err := s.db.Exec(c.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---------- резервные копии и импорт ----------

// BackupTo делает целостную копию базы одним файлом (VACUUM INTO).
func (s *Store) BackupTo(ctx context.Context, path string) error {
	_ = os.Remove(path) // VACUUM INTO не перезаписывает существующий файл
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

// ValidateDB проверяет, что файл — база SINAPS CRM, а не произвольный SQLite.
func ValidateDB(path string) (chats, messages int, err error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()
	if err = db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&chats); err != nil {
		return 0, 0, errors.New("в файле нет таблицы клиентов — это не база SINAPS CRM")
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&messages); err != nil {
		return 0, 0, errors.New("в файле нет таблицы сообщений — это не база SINAPS CRM")
	}
	return chats, messages, nil
}

// EachMessage обходит все сообщения (для выгрузки в Excel).
func (s *Store) EachMessage(ctx context.Context, fn func(chatTitle string, m Message) error) error {
	rows, err := s.db.QueryContext(ctx, `SELECT c.title, m.id, m.chat_id, m.msg_id, m.out, m.text, m.date, m.media_kind, m.media_ref, m.deleted
FROM messages m JOIN chats c ON c.id = m.chat_id ORDER BY c.title, m.date, m.msg_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var title string
		var m Message
		var o, d int
		if err := rows.Scan(&title, &m.ID, &m.ChatID, &m.MsgID, &o, &m.Text, &m.Date, &m.MediaKind, &m.MediaRef, &d); err != nil {
			return err
		}
		m.Out, m.Deleted = o == 1, d == 1
		m.FillFile()
		if err := fn(title, m); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ImportRow — клиент из CSV/Excel. Пустые поля не перезаписывают существующие.
type ImportRow struct {
	Channel  string
	ExtID    string
	Title    string
	Username string
	Phone    string
	Stage    string
	Amount   *int64
	NextStep string
	Note     string
	Tags     string
}

// FindImportTarget ищет, какого клиента обновит строка: по каналу+ID,
// иначе по телефону или @username, иначе по точному названию. 0 — будет создан новый.
func (s *Store) FindImportTarget(ctx context.Context, r ImportRow) int64 {
	var id int64
	if r.Channel != "" && r.ExtID != "" {
		if s.db.QueryRowContext(ctx, `SELECT id FROM chats WHERE channel=? AND ext_id=?`, r.Channel, r.ExtID).Scan(&id) == nil {
			return id
		}
	}
	if p := digits(r.Phone); len(p) >= 7 {
		if s.db.QueryRowContext(ctx, `SELECT id FROM chats WHERE phone<>'' AND replace(replace(replace(replace(phone,'+',''),' ',''),'-',''),'(','') LIKE ? LIMIT 1`, "%"+p).Scan(&id) == nil {
			return id
		}
	}
	if u := strings.TrimPrefix(strings.TrimSpace(r.Username), "@"); u != "" {
		if s.db.QueryRowContext(ctx, `SELECT id FROM chats WHERE lower(username)=lower(?) LIMIT 1`, u).Scan(&id) == nil {
			return id
		}
	}
	if t := strings.TrimSpace(r.Title); t != "" {
		if s.db.QueryRowContext(ctx, `SELECT id FROM chats WHERE title=? LIMIT 1`, t).Scan(&id) == nil {
			return id
		}
	}
	return 0
}

// ApplyImport обновляет найденного клиента или создаёт новый лид (канал manual).
func (s *Store) ApplyImport(ctx context.Context, r ImportRow) (created bool, err error) {
	id := s.FindImportTarget(ctx, r)
	if id == 0 {
		if strings.TrimSpace(r.Title) == "" {
			return false, errors.New("нет названия клиента")
		}
		now := time.Now().Unix()
		ext := "m" + strconv.FormatInt(now, 36) + strconv.FormatInt(int64(len(r.Title))+int64(len(r.Phone)), 36) + randSuffix()
		err = s.db.QueryRowContext(ctx, `INSERT INTO chats(channel,ext_id,kind,peer_id,title,username,phone,last_at,created_at,updated_at)
VALUES('manual',?,'contact',0,?,?,?,?,?,?) RETURNING id`,
			ext, strings.TrimSpace(r.Title), strings.TrimPrefix(strings.TrimSpace(r.Username), "@"), strings.TrimSpace(r.Phone), now, now, now).Scan(&id)
		if err != nil {
			return false, err
		}
		created = true
		_ = s.AddEvent(ctx, id, "import", "Импортирован из файла")
	} else {
		// контактные данные дополняем, только если их не было
		_, err = s.db.ExecContext(ctx, `UPDATE chats SET
  phone    = CASE WHEN phone=''    THEN ? ELSE phone END,
  username = CASE WHEN username='' THEN ? ELSE username END
WHERE id=?`, strings.TrimSpace(r.Phone), strings.TrimPrefix(strings.TrimSpace(r.Username), "@"), id)
		if err != nil {
			return false, err
		}
	}
	p := ChatPatch{}
	if r.Stage != "" {
		p.Stage = &r.Stage
	}
	p.Amount = r.Amount
	if r.NextStep != "" {
		p.NextStep = &r.NextStep
	}
	if r.Note != "" {
		p.Note = &r.Note
	}
	if r.Tags != "" {
		p.Tags = &r.Tags
	}
	return created, s.PatchChat(ctx, id, p)
}

// ParseStage понимает и id этапа, и русское название.
func ParseStage(v string) (string, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "", true
	}
	for _, st := range Stages {
		if v == st || v == strings.ToLower(StageName(st)) {
			return st, true
		}
	}
	switch v {
	case "новый", "новая", "new lead":
		return "new", true
	case "ответила":
		return "replied", true
	case "won", "сделки":
		return "deal", true
	case "потеряна", "отказ":
		return "lost", true
	}
	return "", false
}

func digits(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

func randSuffix() string {
	var b [4]byte
	_, _ = crand.Read(b[:])
	return fmt.Sprintf("%x", b)
}

// ---------- settings ----------

func (s *Store) Get(ctx context.Context, key string) string {
	var v string
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

func (s *Store) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}

// ---------- chats ----------

const chatCols = `id,channel,ext_id,kind,peer_id,access_hash,title,username,phone,stage,amount,
next_step,note,tags,unread,last_msg,last_at,last_out,created_at`

func scanChat(r interface{ Scan(...any) error }) (Chat, error) {
	var c Chat
	var unread, lastOut int
	err := r.Scan(&c.ID, &c.Channel, &c.ExtID, &c.Kind, &c.PeerID, &c.AccessHash, &c.Title,
		&c.Username, &c.Phone, &c.Stage, &c.Amount, &c.NextStep, &c.Note, &c.Tags, &unread,
		&c.LastMsg, &c.LastAt, &lastOut, &c.CreatedAt)
	c.Unread, c.LastOut = unread == 1, lastOut == 1
	return c, err
}

// UpsertChat создаёт чат или обновляет только «системные» поля. CRM-поля не трогает.
// Возвращает id и признак того, что чат новый.
func (s *Store) UpsertChat(ctx context.Context, c Chat) (int64, bool, error) {
	now := time.Now().Unix()
	var existing int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM chats WHERE channel=? AND ext_id=?`, c.Channel, c.ExtID).Scan(&existing)
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		return 0, false, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `
INSERT INTO chats(channel,ext_id,kind,peer_id,access_hash,title,username,phone,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(channel,ext_id) DO UPDATE SET
  access_hash = CASE WHEN excluded.access_hash<>0 THEN excluded.access_hash ELSE chats.access_hash END,
  title       = CASE WHEN excluded.title<>''     THEN excluded.title       ELSE chats.title END,
  username    = excluded.username,
  phone       = CASE WHEN excluded.phone<>''     THEN excluded.phone       ELSE chats.phone END,
  updated_at  = excluded.updated_at
RETURNING id`,
		c.Channel, c.ExtID, c.Kind, c.PeerID, c.AccessHash, c.Title, c.Username, c.Phone, now, now).Scan(&id)
	if err != nil {
		return 0, false, err
	}
	if isNew {
		_ = s.AddEvent(ctx, id, "import", "Импорт из "+channelName(c.Channel))
	}
	return id, isNew, nil
}

func (s *Store) ChatByExt(ctx context.Context, channel, extID string) (Chat, error) {
	return scanChat(s.db.QueryRowContext(ctx, `SELECT `+chatCols+` FROM chats WHERE channel=? AND ext_id=?`, channel, extID))
}

func (s *Store) Chat(ctx context.Context, id int64) (Chat, error) {
	return scanChat(s.db.QueryRowContext(ctx, `SELECT `+chatCols+` FROM chats WHERE id=?`, id))
}

func (s *Store) Chats(ctx context.Context, channel, search string) ([]Chat, error) {
	q := `SELECT ` + chatCols + ` FROM chats WHERE 1=1`
	var args []any
	if channel != "" && channel != "all" {
		q += ` AND channel=?`
		args = append(args, channel)
	}
	if search = strings.TrimSpace(search); search != "" {
		q += ` AND (title LIKE ? OR username LIKE ? OR last_msg LIKE ? OR note LIKE ?)`
		like := "%" + search + "%"
		args = append(args, like, like, like, like)
	}
	q += ` ORDER BY last_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chat{}
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ChatsByStage(ctx context.Context, stage string) ([]Chat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+chatCols+` FROM chats WHERE stage=? ORDER BY last_at DESC`, stage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chat{}
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) PatchChat(ctx context.Context, id int64, p ChatPatch) error {
	cur, err := s.Chat(ctx, id)
	if err != nil {
		return err
	}
	set, args := []string{}, []any{}
	if p.Stage != nil && *p.Stage != cur.Stage {
		if !validStage(*p.Stage) {
			return fmt.Errorf("неизвестный этап %q", *p.Stage)
		}
		set, args = append(set, "stage=?"), append(args, *p.Stage)
		_ = s.AddEvent(ctx, id, "stage", "Этап → "+StageName(*p.Stage))
	}
	if p.Amount != nil {
		set, args = append(set, "amount=?"), append(args, *p.Amount)
	}
	if p.NextStep != nil {
		set, args = append(set, "next_step=?"), append(args, *p.NextStep)
	}
	if p.Note != nil {
		set, args = append(set, "note=?"), append(args, *p.Note)
	}
	if p.Tags != nil {
		set, args = append(set, "tags=?"), append(args, *p.Tags)
	}
	if len(set) == 0 {
		return nil
	}
	set, args = append(set, "updated_at=?"), append(args, time.Now().Unix(), id)
	_, err = s.db.ExecContext(ctx, `UPDATE chats SET `+strings.Join(set, ",")+` WHERE id=?`, args...)
	return err
}

func (s *Store) MarkRead(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET unread=0 WHERE id=?`, id)
	return err
}

// HideChats убирает чаты из CRM (вместе с перепиской в базе) и запоминает,
// чтобы синхронизация их больше не подтягивала. Telegram не трогается.
func (s *Store) HideChats(ctx context.Context, channel string, chats map[string]string) (int, error) {
	now := time.Now().Unix()
	exts := make([]string, 0, len(chats))
	for ext, title := range chats {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO hidden_chats(channel,ext_id,title,at) VALUES(?,?,?,?)
ON CONFLICT(channel,ext_id) DO UPDATE SET title=excluded.title`, channel, ext, title, now); err != nil {
			return 0, err
		}
		exts = append(exts, ext)
	}
	return s.DeleteChatsByExt(ctx, channel, exts)
}

// UnhideChats возвращает чаты: при следующей синхронизации они снова импортируются.
func (s *Store) UnhideChats(ctx context.Context, channel string, exts []string) error {
	for _, e := range exts {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM hidden_chats WHERE channel=? AND ext_id=?`, channel, e); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) HiddenSet(ctx context.Context, channel string) map[string]bool {
	out := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT ext_id FROM hidden_chats WHERE channel=?`, channel)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			out[e] = true
		}
	}
	return out
}

// DeleteChatsByExt убирает чаты из CRM вместе с перепиской (в Telegram ничего не трогает).
func (s *Store) DeleteChatsByExt(ctx context.Context, channel string, extIDs []string) (int, error) {
	n := 0
	for _, e := range extIDs {
		res, err := s.db.ExecContext(ctx, `DELETE FROM chats WHERE channel=? AND ext_id=?`, channel, e)
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, nil
}

func (s *Store) DeleteChat(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chats WHERE id=?`, id)
	return err
}

// ---------- messages ----------

// AddMessage вставляет сообщение без дублей. inserted=false — такое уже было.
// live=true — сообщение пришло в реальном времени (не из импорта истории).
func (s *Store) AddMessage(ctx context.Context, m Message, live bool) (inserted bool, err error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages(chat_id,msg_id,out,text,date,media_kind,media_ref,ext_msg_id) VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(chat_id,msg_id) DO NOTHING`,
		m.ChatID, m.MsgID, b2i(m.Out), m.Text, m.Date, m.MediaKind, m.MediaRef, m.ExtMsgID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// сообщение уже есть: докидываем фото, если раньше его не сохраняли
		if m.MediaRef != "" {
			_, err = s.db.ExecContext(ctx, `UPDATE messages SET media_kind=?, media_ref=?,
  text = CASE WHEN text IN ('[фото]','[файл]') THEN ? ELSE text END
WHERE chat_id=? AND msg_id=?`, m.MediaKind, m.MediaRef, m.Text, m.ChatID, m.MsgID)
		}
		return false, err
	}
	// превью последнего сообщения
	_, err = s.db.ExecContext(ctx,
		`UPDATE chats SET last_msg=?, last_at=?, last_out=? WHERE id=? AND last_at<=?`,
		previewOf(m.Text, m.MediaKind), m.Date, b2i(m.Out), m.ChatID, m.Date)
	if err != nil {
		return true, err
	}
	if live && !m.Out {
		_, err = s.db.ExecContext(ctx, `UPDATE chats SET unread=1 WHERE id=?`, m.ChatID)
		if err != nil {
			return true, err
		}
		// клиент ответил на нашу рассылку — двигаем по воронке сам
		res, err := s.db.ExecContext(ctx, `UPDATE chats SET stage='replied' WHERE id=? AND stage='sent'`, m.ChatID)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				_ = s.AddEvent(ctx, m.ChatID, "stage", "Клиент ответил → "+StageName("replied"))
			}
		}
	}
	return true, nil
}

func (s *Store) Messages(ctx context.Context, chatID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+msgCols+` FROM (
  SELECT * FROM messages WHERE chat_id=? ORDER BY date DESC, msg_id DESC LIMIT ?
) ORDER BY date ASC, msg_id ASC`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

const msgCols = `id,chat_id,msg_id,out,text,date,media_kind,media_ref,deleted,ext_msg_id`

func scanMsg(r interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var o, d int
	err := r.Scan(&m.ID, &m.ChatID, &m.MsgID, &o, &m.Text, &m.Date, &m.MediaKind, &m.MediaRef, &d, &m.ExtMsgID)
	m.Out, m.Deleted = o == 1, d == 1
	m.FillFile()
	return m, err
}

func (s *Store) MessageByID(ctx context.Context, id int64) (Message, error) {
	return scanMsg(s.db.QueryRowContext(ctx, `SELECT `+msgCols+` FROM messages WHERE id=?`, id))
}

func (s *Store) MessageByMsg(ctx context.Context, chatID, msgID int64) (Message, error) {
	return scanMsg(s.db.QueryRowContext(ctx, `SELECT `+msgCols+` FROM messages WHERE chat_id=? AND msg_id=?`, chatID, msgID))
}

func (s *Store) SetMediaRef(ctx context.Context, id int64, ref string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET media_ref=? WHERE id=?`, ref, id)
	return err
}

// DeleteResult — что поменялось в чате после удаления в Telegram.
type DeleteResult struct {
	ChatID  int64   `json:"chatId"`
	Removed []int64 `json:"removed"` // наши сообщения — убраны из CRM
	Marked  []int64 `json:"marked"`  // сообщения клиента — остались с пометкой «удалено»
}

// ApplyRemoteDelete: Telegram сообщил, что сообщения удалены.
// Свои (out) удаляем и из CRM, сообщения клиента оставляем с пометкой.
// channelExt == "" — личка и обычные группы (id сообщений общие на аккаунт),
// иначе ext_id канала/супергруппы ("ch123").
func (s *Store) ApplyRemoteDelete(ctx context.Context, channelExt string, ids []int) ([]DeleteResult, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{}
	q := `SELECT m.id, m.chat_id, m.msg_id, m.out FROM messages m JOIN chats c ON c.id=m.chat_id
WHERE c.channel='tg' AND m.deleted=0 AND `
	if channelExt == "" {
		q += `c.kind IN ('user','chat')`
	} else {
		q += `c.ext_id=?`
		args = append(args, channelExt)
	}
	q += ` AND m.msg_id IN (` + ph + `)`
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	type hit struct {
		row, chat, msg int64
		out            bool
	}
	var hits []hit
	for rows.Next() {
		var h hit
		var o int
		if err := rows.Scan(&h.row, &h.chat, &h.msg, &o); err != nil {
			rows.Close()
			return nil, err
		}
		h.out = o == 1
		hits = append(hits, h)
	}
	rows.Close()

	byChat := map[int64]*DeleteResult{}
	var order []int64
	for _, h := range hits {
		r := byChat[h.chat]
		if r == nil {
			r = &DeleteResult{ChatID: h.chat, Removed: []int64{}, Marked: []int64{}}
			byChat[h.chat] = r
			order = append(order, h.chat)
		}
		if h.out {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE id=?`, h.row); err != nil {
				return nil, err
			}
			r.Removed = append(r.Removed, h.msg)
		} else {
			if _, err := s.db.ExecContext(ctx, `UPDATE messages SET deleted=1 WHERE id=?`, h.row); err != nil {
				return nil, err
			}
			r.Marked = append(r.Marked, h.msg)
		}
	}
	out := make([]DeleteResult, 0, len(order))
	for _, id := range order {
		if len(byChat[id].Removed) > 0 {
			_ = s.refreshLast(ctx, id)
		}
		out = append(out, *byChat[id])
	}
	return out, nil
}

// DeleteOwnMessage — удаление своего сообщения из CRM (в Telegram уже удалено).
func (s *Store) DeleteOwnMessage(ctx context.Context, chatID, msgID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE chat_id=? AND msg_id=? AND out=1`, chatID, msgID); err != nil {
		return err
	}
	return s.refreshLast(ctx, chatID)
}

func (s *Store) refreshLast(ctx context.Context, chatID int64) error {
	var text, kind string
	var date int64
	var out int
	err := s.db.QueryRowContext(ctx, `SELECT text, media_kind, date, out FROM messages WHERE chat_id=?
ORDER BY date DESC, msg_id DESC LIMIT 1`, chatID).Scan(&text, &kind, &date, &out)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.db.ExecContext(ctx, `UPDATE chats SET last_msg='', last_out=0 WHERE id=?`, chatID)
		return err
	}
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE chats SET last_msg=?, last_at=?, last_out=? WHERE id=?`,
		previewOf(text, kind), date, out, chatID)
	return err
}

// ---------- events / sends ----------

func (s *Store) AddEvent(ctx context.Context, chatID int64, kind, text string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO events(chat_id,kind,text,at) VALUES(?,?,?,?)`,
		chatID, kind, text, time.Now().Unix())
	return err
}

func (s *Store) Events(ctx context.Context, chatID int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind,text,at FROM events WHERE chat_id=? ORDER BY at DESC, id DESC LIMIT 50`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Kind, &e.Text, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) LogSend(ctx context.Context, chatID int64, kind string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sends(chat_id,kind,at) VALUES(?,?,?)`, chatID, kind, time.Now().Unix())
	return err
}

// SentToday — сколько сообщений такого типа ушло с начала локальных суток.
func (s *Store) SentToday(ctx context.Context, kind string) int {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sends WHERE kind=? AND at>=?`, kind, start).Scan(&n)
	return n
}

// ---------- stats ----------

type Stats struct {
	Total    int            `json:"total"`
	Unread   int            `json:"unread"`
	Pipeline int64          `json:"pipeline"`
	Won      int            `json:"won"`
	ByStage  map[string]int `json:"byStage"`
	ByChan   map[string]int `json:"byChannel"`
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	st := Stats{ByStage: map[string]int{}, ByChan: map[string]int{}}
	rows, err := s.db.QueryContext(ctx, `SELECT stage, channel, unread, amount FROM chats`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var stage, ch string
		var unread int
		var amount int64
		if err := rows.Scan(&stage, &ch, &unread, &amount); err != nil {
			return st, err
		}
		st.Total++
		st.ByStage[stage]++
		st.ByChan[ch]++
		st.Unread += unread
		if stage == "deal" {
			st.Won++
		}
		if stage != "lost" && stage != "deal" {
			st.Pipeline += amount
		}
	}
	return st, rows.Err()
}

// ---------- helpers ----------

func StageName(s string) string {
	switch s {
	case "new":
		return "Новые"
	case "sent":
		return "Написал"
	case "replied":
		return "Ответил"
	case "deal":
		return "Сделка"
	case "lost":
		return "Потерян"
	}
	return s
}

func validStage(s string) bool {
	for _, x := range Stages {
		if x == s {
			return true
		}
	}
	return false
}

func channelName(c string) string {
	if c == "wa" {
		return "WhatsApp"
	}
	return "Telegram"
}

func previewOf(text, media string) string {
	if p := preview(text); p != "" {
		return p
	}
	switch media {
	case "photo":
		return "Фото"
	case "file":
		return "Файл"
	}
	return ""
}

func preview(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	r := []rune(t)
	if len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return t
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------- WhatsApp: кэш чатов и истории ----------

// MsgIDFromExt — числовой ID для строкового ID сообщения (уникальность внутри чата).
func MsgIDFromExt(ext string) int64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(ext); i++ {
		h ^= uint64(ext[i])
		h *= 1099511628211
	}
	return int64(h >> 1)
}

type WAKnown struct {
	JID     string `json:"jid"`
	Name    string `json:"name"`
	LastAt  int64  `json:"lastAt"`
	LastMsg string `json:"lastMsg"`
}

// WAUpsertKnown запоминает чат: имя не затирается пустым, дата — только вперёд.
func (s *Store) WAUpsertKnown(ctx context.Context, k WAKnown) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO wa_chats(jid,name,last_at,last_msg) VALUES(?,?,?,?)
ON CONFLICT(jid) DO UPDATE SET
  name     = CASE WHEN excluded.name<>'' THEN excluded.name ELSE wa_chats.name END,
  last_msg = CASE WHEN excluded.last_at>=wa_chats.last_at AND excluded.last_msg<>'' THEN excluded.last_msg ELSE wa_chats.last_msg END,
  last_at  = MAX(wa_chats.last_at, excluded.last_at)`, k.JID, k.Name, k.LastAt, preview(k.LastMsg))
	return err
}

func (s *Store) WAKnownChats(ctx context.Context) ([]WAKnown, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT jid,name,last_at,last_msg FROM wa_chats ORDER BY last_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WAKnown{}
	for rows.Next() {
		var k WAKnown
		if err := rows.Scan(&k.JID, &k.Name, &k.LastAt, &k.LastMsg); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// WACacheHistory сохраняет сообщения истории (без дублей) до выбора чата.
func (s *Store) WACacheHistory(ctx context.Context, jid string, msgs []Message) error {
	for _, m := range msgs {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO wa_history(jid,ext_msg_id,out,text,date,media_kind,media_ref)
VALUES(?,?,?,?,?,?,?) ON CONFLICT(jid,ext_msg_id) DO NOTHING`,
			jid, m.ExtMsgID, b2i(m.Out), m.Text, m.Date, m.MediaKind, m.MediaRef); err != nil {
			return err
		}
	}
	return nil
}

// WAHistory — последние limit сообщений из кэша истории для чата.
func (s *Store) WAHistory(ctx context.Context, jid string, limit int) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ext_msg_id,out,text,date,media_kind,media_ref FROM wa_history
WHERE jid=? ORDER BY date DESC LIMIT ?`, jid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var o int
		if err := rows.Scan(&m.ExtMsgID, &o, &m.Text, &m.Date, &m.MediaKind, &m.MediaRef); err != nil {
			return nil, err
		}
		m.Out = o == 1
		m.MsgID = MsgIDFromExt(m.ExtMsgID)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ApplyExtDelete: сообщение удалили в мессенджере. Своё — убираем, чужое — помечаем.
func (s *Store) ApplyExtDelete(ctx context.Context, chatID int64, ext string) (DeleteResult, error) {
	r := DeleteResult{ChatID: chatID, Removed: []int64{}, Marked: []int64{}}
	m, err := s.MessageByMsg(ctx, chatID, MsgIDFromExt(ext))
	if err != nil {
		return r, nil // такого сообщения у нас нет
	}
	if m.Out {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE id=?`, m.ID); err != nil {
			return r, err
		}
		r.Removed = append(r.Removed, m.MsgID)
		return r, s.refreshLast(ctx, chatID)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE messages SET deleted=1 WHERE id=?`, m.ID); err != nil {
		return r, err
	}
	r.Marked = append(r.Marked, m.MsgID)
	return r, nil
}
