package store

import (
	"context"
	"database/sql"
	"strings"
	"unicode"
)

// Поиск по всему: клиенты, переписка, чеки, файлы, история.
//
// Текст сообщений ищется по полнотекстовому индексу SQLite (FTS5): слова запроса
// совпадают с началом слов сообщения, регистр и «ё/е» не важны. Если FTS5 в сборке
// SQLite нет, ищем перебором — медленнее, но результат тот же.

// Norm — строка для сравнения: нижний регистр, «ё» → «е».
func Norm(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "ё", "е")
}

// Words — слова запроса для поиска (без знаков препинания, @ и т.п.).
func Words(q string) []string {
	return strings.FieldsFunc(Norm(q), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// MatchWords — все слова запроса встречаются в тексте как начало слова.
func MatchWords(text string, words []string) bool {
	if len(words) == 0 {
		return false
	}
	tw := Words(text)
	for _, w := range words {
		ok := false
		for _, t := range tw {
			if strings.HasPrefix(t, w) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// Snippet — кусок текста вокруг первого найденного слова (для длинных сообщений).
func Snippet(text string, words []string, width int) string {
	r := []rune(strings.Join(strings.Fields(text), " "))
	if len(r) <= width || len(words) == 0 {
		return string(r)
	}
	low := []rune(Norm(string(r)))
	at := 0
	for i := range low {
		if i == 0 || !(unicode.IsLetter(low[i-1]) || unicode.IsDigit(low[i-1])) {
			if strings.HasPrefix(string(low[i:]), words[0]) {
				at = i
				break
			}
		}
	}
	from := at - width/3
	if from < 0 {
		from = 0
	}
	to := from + width
	if to > len(r) {
		to = len(r)
		if from = to - width; from < 0 {
			from = 0
		}
	}
	out := string(r[from:to])
	if from > 0 {
		out = "…" + out
	}
	if to < len(r) {
		out += "…"
	}
	return out
}

// В индекс кладётся текст с «ё» → «е» (сам FTS5 снимает диакритику только с латиницы),
// регистр кириллицы FTS5 сворачивает сам.
//
// initSearch создаёт индекс переписки и триггеры, которые держат его в актуальном виде.
// Первый раз индекс строится по всем сообщениям (на больших базах — несколько секунд).
func (s *Store) initSearch() {
	ctx := context.Background()
	_, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(text, content='messages', content_rowid='id', tokenize='unicode61 remove_diacritics 2')`)
	if err != nil {
		return // в этой сборке SQLite нет FTS5 — будем искать перебором
	}
	for _, q := range []string{
		`CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, text) VALUES (new.id, replace(replace(new.text,'ё','е'),'Ё','Е'));
END`,
		`CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.id, replace(replace(old.text,'ё','е'),'Ё','Е'));
END`,
		`CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE OF text ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.id, replace(replace(old.text,'ё','е'),'Ё','Е'));
  INSERT INTO messages_fts(rowid, text) VALUES (new.id, replace(replace(new.text,'ё','е'),'Ё','Е'));
END`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return
		}
	}
	if s.Get(ctx, "search_fts_v1") != "1" {
		if _, err := s.db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES ('delete-all')`); err != nil {
			return
		}
		if _, err := s.db.Exec(`INSERT INTO messages_fts(rowid, text) SELECT id, replace(replace(text,'ё','е'),'Ё','Е') FROM messages`); err != nil {
			return
		}
		_ = s.Set(ctx, "search_fts_v1", "1")
	}
	s.fts = true
}

// MsgHit — найденное сообщение вместе с клиентом.
type MsgHit struct {
	Message
	ChatTitle string `json:"chatTitle"`
	Channel   string `json:"channel"`
	Snippet   string `json:"snippet"`
}

// SearchMessages — сообщения, где есть все слова запроса. Новые сверху.
// total — сколько всего нашлось (не больше 1000).
func (s *Store) SearchMessages(ctx context.Context, words []string, limit int) (hits []MsgHit, total int, err error) {
	hits = []MsgHit{}
	if len(words) == 0 {
		return hits, 0, nil
	}
	cols := `m.id,m.chat_id,m.msg_id,m.out,m.text,m.date,m.media_kind,m.media_ref,m.deleted,m.ext_msg_id, c.title, c.channel`
	add := func(rows *sql.Rows) error {
		defer rows.Close()
		for rows.Next() {
			var h MsgHit
			var o, d int
			if err := rows.Scan(&h.ID, &h.ChatID, &h.MsgID, &o, &h.Text, &h.Date, &h.MediaKind, &h.MediaRef, &d, &h.ExtMsgID, &h.ChatTitle, &h.Channel); err != nil {
				return err
			}
			h.Out, h.Deleted = o == 1, d == 1
			h.FillFile()
			if s.fts || MatchWords(h.Text, words) {
				total++
				if len(hits) < limit {
					h.Snippet = Snippet(h.Text, words, 160)
					hits = append(hits, h)
				}
			}
		}
		return rows.Err()
	}
	if s.fts {
		parts := make([]string, len(words))
		for i, w := range words {
			parts[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
		}
		rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM messages_fts f
JOIN messages m ON m.id=f.rowid JOIN chats c ON c.id=m.chat_id
WHERE messages_fts MATCH ? ORDER BY m.date DESC, m.msg_id DESC LIMIT 1000`, strings.Join(parts, " AND "))
		if err != nil {
			return hits, 0, err
		}
		return hits, total, add(rows)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM messages m JOIN chats c ON c.id=m.chat_id
WHERE m.text<>'' ORDER BY m.date DESC, m.msg_id DESC LIMIT 200000`)
	if err != nil {
		return hits, 0, err
	}
	err = add(rows)
	if total > 1000 {
		total = 1000
	}
	return hits, total, err
}

// SearchFiles — файлы из переписки, в имени которых есть все слова запроса.
func (s *Store) SearchFiles(ctx context.Context, words []string, limit int) (hits []MsgHit, total int, err error) {
	hits = []MsgHit{}
	if len(words) == 0 {
		return hits, 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.chat_id,m.msg_id,m.out,m.text,m.date,m.media_kind,m.media_ref,m.deleted,m.ext_msg_id, c.title, c.channel
FROM messages m JOIN chats c ON c.id=m.chat_id
WHERE m.media_kind='file' AND m.media_ref<>'' ORDER BY m.date DESC LIMIT 50000`)
	if err != nil {
		return hits, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var h MsgHit
		var o, d int
		if err := rows.Scan(&h.ID, &h.ChatID, &h.MsgID, &o, &h.Text, &h.Date, &h.MediaKind, &h.MediaRef, &d, &h.ExtMsgID, &h.ChatTitle, &h.Channel); err != nil {
			return hits, total, err
		}
		h.Out, h.Deleted = o == 1, d == 1
		h.FillFile()
		if strings.Contains(Norm(h.FileName), strings.Join(words, " ")) || MatchWords(h.FileName, words) {
			total++
			if len(hits) < limit {
				hits = append(hits, h)
			}
		}
	}
	return hits, total, rows.Err()
}

// EventHit — запись из истории клиента.
type EventHit struct {
	ChatID    int64  `json:"chatId"`
	ChatTitle string `json:"chatTitle"`
	Channel   string `json:"channel"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	At        int64  `json:"at"`
}

// SearchEvents — история клиентов (этапы, заметки, чеки, проекты).
func (s *Store) SearchEvents(ctx context.Context, words []string, limit int) (hits []EventHit, total int, err error) {
	hits = []EventHit{}
	if len(words) == 0 {
		return hits, 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.chat_id, c.title, c.channel, e.kind, e.text, e.at
FROM events e JOIN chats c ON c.id=e.chat_id WHERE e.kind<>'import' ORDER BY e.at DESC, e.id DESC LIMIT 50000`)
	if err != nil {
		return hits, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var h EventHit
		if err := rows.Scan(&h.ChatID, &h.ChatTitle, &h.Channel, &h.Kind, &h.Text, &h.At); err != nil {
			return hits, total, err
		}
		if MatchWords(h.Text, words) {
			total++
			if len(hits) < limit {
				hits = append(hits, h)
			}
		}
	}
	return hits, total, rows.Err()
}

// MessagesAround — переписка начиная за `before` сообщений до msgID и до конца (не больше 5000).
// Нужна, чтобы открыть найденное старое сообщение, которого нет среди последних 500.
func (s *Store) MessagesAround(ctx context.Context, chatID, msgID int64, before int) ([]Message, error) {
	var date int64
	if err := s.db.QueryRowContext(ctx, `SELECT date FROM messages WHERE chat_id=? AND msg_id=?`, chatID, msgID).Scan(&date); err != nil {
		return nil, err
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE chat_id=? AND (date>? OR (date=? AND msg_id>=?))`,
		chatID, date, date, msgID).Scan(&n); err != nil {
		return nil, err
	}
	n += before
	if n > 5000 {
		n = 5000
	}
	if n < 500 {
		n = 500
	}
	return s.Messages(ctx, chatID, n)
}

// MessagesNear — найденное сообщение и по n соседних до и после (для предпросмотра).
func (s *Store) MessagesNear(ctx context.Context, chatID, msgID int64, n int) ([]Message, error) {
	var date int64
	if err := s.db.QueryRowContext(ctx, `SELECT date FROM messages WHERE chat_id=? AND msg_id=?`, chatID, msgID).Scan(&date); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+msgCols+` FROM (SELECT * FROM messages WHERE chat_id=? AND (date<? OR (date=? AND msg_id<?)) ORDER BY date DESC, msg_id DESC LIMIT ?)
UNION ALL
SELECT `+msgCols+` FROM (SELECT * FROM messages WHERE chat_id=? AND (date>? OR (date=? AND msg_id>=?)) ORDER BY date ASC, msg_id ASC LIMIT ?)
ORDER BY date ASC, msg_id ASC`, chatID, date, date, msgID, n, chatID, date, date, msgID, n+1)
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
