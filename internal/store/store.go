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
	"database/sql"
	"errors"
	"fmt"
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
	ID     int64  `json:"id"`
	ChatID int64  `json:"chatId"`
	MsgID  int64  `json:"msgId"`
	Out    bool   `json:"out"`
	Text   string `json:"text"`
	Date   int64  `json:"date"`
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
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

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

func (s *Store) DeleteChat(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chats WHERE id=?`, id)
	return err
}

// ---------- messages ----------

// AddMessage вставляет сообщение без дублей. inserted=false — такое уже было.
// live=true — сообщение пришло в реальном времени (не из импорта истории).
func (s *Store) AddMessage(ctx context.Context, m Message, live bool) (inserted bool, err error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages(chat_id,msg_id,out,text,date) VALUES(?,?,?,?,?)
		 ON CONFLICT(chat_id,msg_id) DO NOTHING`,
		m.ChatID, m.MsgID, b2i(m.Out), m.Text, m.Date)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	// превью последнего сообщения
	_, err = s.db.ExecContext(ctx,
		`UPDATE chats SET last_msg=?, last_at=?, last_out=? WHERE id=? AND last_at<=?`,
		preview(m.Text), m.Date, b2i(m.Out), m.ChatID, m.Date)
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
SELECT id,chat_id,msg_id,out,text,date FROM (
  SELECT * FROM messages WHERE chat_id=? ORDER BY date DESC, msg_id DESC LIMIT ?
) ORDER BY date ASC, msg_id ASC`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		var o int
		if err := rows.Scan(&m.ID, &m.ChatID, &m.MsgID, &o, &m.Text, &m.Date); err != nil {
			return nil, err
		}
		m.Out = o == 1
		out = append(out, m)
	}
	return out, rows.Err()
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
