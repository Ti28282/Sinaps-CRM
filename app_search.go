package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"sinaps-crm/internal/store"
)

// ---------- поиск по всему (Ctrl+K) ----------
//
// Ищет только в базе CRM на этом компьютере: в Telegram и WhatsApp запросов нет.

const searchLimit = 50

// SearchResult — найденное по разделам. Counts — сколько всего в каждом разделе.
type SearchResult struct {
	Q        string           `json:"q"`
	Clients  []store.Chat     `json:"clients"`
	Messages []store.MsgHit   `json:"messages"`
	Receipts []store.Receipt  `json:"receipts"`
	Files    []store.MsgHit   `json:"files"`
	Events   []store.EventHit `json:"events"`
	Counts   map[string]int   `json:"counts"`
	Amount   int64            `json:"amount"` // запрос — это сумма: «25 000», «25000», «25к»
}

var amountRe = regexp.MustCompile(`^\s*(\d[\d\s]*(?:[.,]\d+)?)\s*(к|k|тыс\.?)?\s*(₽|руб\.?|р\.?|сом|₸|\$)?\s*$`)

// parseAmount: «25 000» → 25000, «25к» / «2,5к» → 25000 / 2500. 0 — запрос не сумма.
func parseAmount(q string) int64 {
	m := amountRe.FindStringSubmatch(strings.ToLower(q))
	if m == nil {
		return 0
	}
	num := strings.ReplaceAll(strings.ReplaceAll(m[1], " ", ""), ",", ".")
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	if m[2] != "" {
		f *= 1000
	}
	if f < 100 {
		return 0 // «25» — скорее число в тексте, чем сумма
	}
	return int64(f + 0.5)
}

// groupThousands: 25000 → «25 000».
func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func digitsOf(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// Search — поиск по клиентам, переписке, чекам, файлам и истории.
func (a *App) Search(q string) (SearchResult, error) {
	res := SearchResult{Q: q, Clients: []store.Chat{}, Messages: []store.MsgHit{}, Receipts: []store.Receipt{},
		Files: []store.MsgHit{}, Events: []store.EventHit{}, Counts: map[string]int{}}
	words := store.Words(q)
	if len(words) == 0 {
		return res, nil
	}
	ctx := a.ctx
	res.Amount = parseAmount(q)
	nq := strings.Join(words, " ")
	dq := digitsOf(q)

	// клиенты: имя, @username, телефон, заметка, следующий шаг, теги, сумма сделки
	chats, err := a.st.Chats(ctx, "all", "")
	if err != nil {
		return res, err
	}
	for _, c := range chats {
		ok := strings.Contains(store.Norm(c.Title), nq) || store.MatchWords(c.Title+" "+c.Username+" "+c.Note+" "+c.NextStep+" "+c.Tags, words)
		if !ok && len(dq) >= 4 && strings.Contains(digitsOf(c.Phone), dq) {
			ok = true
		}
		if !ok && res.Amount > 0 && c.Amount == res.Amount {
			ok = true
		}
		if ok {
			res.Counts["client"]++
			if len(res.Clients) < searchLimit {
				res.Clients = append(res.Clients, c)
			}
		}
	}
	// сначала те, у кого имя начинается с запроса
	sort.SliceStable(res.Clients, func(i, j int) bool {
		return strings.HasPrefix(store.Norm(res.Clients[i].Title), nq) && !strings.HasPrefix(store.Norm(res.Clients[j].Title), nq)
	})

	if res.Messages, res.Counts["msg"], err = a.st.SearchMessages(ctx, words, searchLimit); err != nil {
		return res, err
	}
	// сумма: «25к» ищем в переписке и как «25 000», и как «25000»
	if res.Amount > 0 {
		seen := map[int64]bool{}
		for _, h := range res.Messages {
			seen[h.ID] = true
		}
		for _, alt := range []string{groupThousands(res.Amount), strconv.FormatInt(res.Amount, 10)} {
			hits, _, err := a.st.SearchMessages(ctx, store.Words(alt), searchLimit)
			if err != nil {
				return res, err
			}
			for _, h := range hits {
				if !seen[h.ID] {
					seen[h.ID] = true
					res.Messages = append(res.Messages, h)
					res.Counts["msg"]++
				}
			}
		}
		sort.SliceStable(res.Messages, func(i, j int) bool { return res.Messages[i].Date > res.Messages[j].Date })
		if len(res.Messages) > searchLimit {
			res.Messages = res.Messages[:searchLimit]
		}
	}
	if res.Files, res.Counts["file"], err = a.st.SearchFiles(ctx, words, searchLimit); err != nil {
		return res, err
	}
	if res.Events, res.Counts["event"], err = a.st.SearchEvents(ctx, words, searchLimit); err != nil {
		return res, err
	}

	// чеки — только когда плагин «Чеки» работает
	if a.pluginActive(chekiID) {
		rs, err := a.st.Receipts(ctx, 0, 0)
		if err != nil {
			return res, err
		}
		tm := a.typeMap()
		for _, r := range rs {
			text := r.ChatTitle + " " + r.What + " " + r.FileName + " " + store.ReceiptStatusName(r.Status)
			for _, p := range r.Parts {
				text += " " + typeOf(tm, p.Kind).Label + " " + p.What
			}
			ok := store.MatchWords(text, words) || strings.Contains(store.Norm(r.ChatTitle), nq)
			if res.Amount > 0 {
				ok = r.Amount == res.Amount
				for _, p := range r.Parts {
					ok = ok || p.Amount == res.Amount
				}
			}
			if ok {
				res.Counts["receipt"]++
				if len(res.Receipts) < searchLimit {
					res.Receipts = append(res.Receipts, r)
				}
			}
		}
	}
	return res, nil
}

// ChatAround — переписка с клиентом, начиная немного раньше найденного сообщения.
// Нужна, когда найденное сообщение старше последних 500, которые показывает диалог.
func (a *App) ChatAround(chatID, msgID int64) ([]store.Message, error) {
	return a.st.MessagesAround(a.ctx, chatID, msgID, 30)
}

// SearchContext — найденное сообщение с соседними (по 2 до и после) для предпросмотра.
func (a *App) SearchContext(chatID, msgID int64) ([]store.Message, error) {
	return a.st.MessagesNear(a.ctx, chatID, msgID, 2)
}
