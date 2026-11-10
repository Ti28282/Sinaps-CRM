// Package notify — прямое соединение CRM с приложением на телефоне.
//
// CRM слушает TCP-порт. Телефон подключается сам (по адресу из QR-кода) и держит
// соединение в фоне; CRM шлёт по нему события: «клиент написал», «новый клиент» и т.д.
// Посредников в интернете нет, поэтому телефон должен «видеть» компьютер: та же сеть
// Wi-Fi, VPN или проброшенный порт.
//
// Протокол (версия 1):
//
//	телефон → первая строка: "SINAPS1 PAIR\n" или "SINAPS1 HELLO <id телефона>\n"
//	дальше в обе стороны кадры: длина (4 байта, big-endian) + nonce (12) + AES-256-GCM(JSON)
//
// Привязка (PAIR): ключ кадров выводится из одноразового кода с экрана CRM (PBKDF2).
// Телефон шлёт {"t":"pair","name":…}, CRM отвечает {"t":"paired","id":…,"key":…} —
// постоянный случайный ключ. Дальше телефон подключается через HELLO уже с ним.
// Весь обмен зашифрован: в сети текст сообщений клиентов не виден.
package notify

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"sinaps-crm/internal/store"
)

const (
	// DefaultPort — порт по умолчанию; если занят, берём один из следующих.
	DefaultPort = 47815
	portTries   = 10

	pairSalt   = "sinaps-crm-pair-v1"
	pairIter   = 120_000
	pairTTL    = 5 * time.Minute
	pairMaxBad = 5

	maxFrame     = 64 << 10
	pingEvery    = 25 * time.Second
	readTimeout  = 75 * time.Second
	writeTimeout = 15 * time.Second
	replayLimit  = 100
)

// Event — уведомление для телефона.
type Event struct {
	Seq      int64  `json:"seq"`
	Kind     string `json:"kind"` // msg | fresh | reply | silent | wait | alarm | digest | test
	Title    string `json:"title"`
	Text     string `json:"text"`
	ChatID   int64  `json:"chatId,omitempty"`
	Client   string `json:"client,omitempty"`
	Channel  string `json:"channel,omitempty"` // tg | wa
	Username string `json:"username,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Stage    string `json:"stage,omitempty"`
	Msg      string `json:"msg,omitempty"` // текст сообщения клиента — для экрана переписки
	At       int64  `json:"at"`
	Quiet    bool   `json:"quiet,omitempty"` // только в ленту, без звука и всплывающего уведомления
}

// Config — как телефону показывать уведомления.
type Config struct {
	Show  string `json:"show"`  // full | name | none
	Group bool   `json:"group"` // несколько сообщений подряд — одним уведомлением
}

// Device — телефон в списке на странице плагина.
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PairedAt int64  `json:"pairedAt"`
	LastSeen int64  `json:"lastSeen"`
	Online   bool   `json:"online"`
}

// Pairing — одноразовый код привязки.
type Pairing struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expiresAt"`
}

type frame struct {
	T     string `json:"t"`
	Name  string `json:"name,omitempty"`
	Last  int64  `json:"last,omitempty"`
	ID    string `json:"id,omitempty"`
	Key   string `json:"key,omitempty"`
	CRM   string `json:"crm,omitempty"`
	Seq   int64  `json:"seq,omitempty"`
	Show  string `json:"show,omitempty"`
	Group *bool  `json:"group,omitempty"`
	Event *Event `json:"event,omitempty"`
}

// Hub — сервер уведомлений.
type Hub struct {
	st       *store.Store
	name     string
	onChange func() // список телефонов или их статус изменился

	mu      sync.Mutex
	ln      net.Listener
	port    int
	cfg     Config
	pair    *Pairing
	pairBad int
	conns   map[string]map[*conn]bool // id телефона → соединения
	closed  bool
	wg      sync.WaitGroup
}

type conn struct {
	c    net.Conn
	aead cipher.AEAD
	wmu  sync.Mutex
}

func New(st *store.Store, crmName string, onChange func()) *Hub {
	if onChange == nil {
		onChange = func() {}
	}
	return &Hub{st: st, name: crmName, onChange: onChange, cfg: Config{Show: "full", Group: true}, conns: map[string]map[*conn]bool{}}
}

// Start открывает порт (preferred или один из следующих) и начинает принимать телефоны.
func (h *Hub) Start(preferred int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ln != nil {
		return nil
	}
	if preferred <= 0 {
		preferred = DefaultPort
	}
	var ln net.Listener
	var err error
	for i := 0; i < portTries; i++ {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", preferred+i))
		if err == nil {
			h.port = preferred + i
			break
		}
	}
	if err != nil {
		return fmt.Errorf("не удалось открыть порт %d: %w", preferred, err)
	}
	h.ln, h.closed = ln, false
	h.wg.Add(1)
	go h.accept(ln)
	return nil
}

func (h *Hub) Stop() {
	h.mu.Lock()
	ln := h.ln
	h.ln, h.closed, h.pair = nil, true, nil
	var all []*conn
	for _, set := range h.conns {
		for c := range set {
			all = append(all, c)
		}
	}
	h.conns = map[string]map[*conn]bool{}
	h.mu.Unlock()
	if ln != nil {
		ln.Close()
	}
	for _, c := range all {
		c.c.Close()
	}
	h.wg.Wait()
}

func (h *Hub) Running() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.ln != nil }
func (h *Hub) Port() int     { h.mu.Lock(); defer h.mu.Unlock(); return h.port }

// SetConfig меняет настройки показа и сообщает их подключённым телефонам.
func (h *Hub) SetConfig(c Config) {
	if c.Show != "name" && c.Show != "none" {
		c.Show = "full"
	}
	h.mu.Lock()
	changed := h.cfg != c
	h.cfg = c
	h.mu.Unlock()
	if changed {
		g := c.Group
		h.broadcast(frame{T: "config", Show: c.Show, Group: &g})
	}
}

// StartPairing выдаёт новый одноразовый код (8 цифр, живёт 5 минут).
func (h *Hub) StartPairing() (Pairing, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100_000_000))
	if err != nil {
		return Pairing{}, err
	}
	p := Pairing{Code: fmt.Sprintf("%08d", n.Int64()), ExpiresAt: time.Now().Add(pairTTL).Unix()}
	h.mu.Lock()
	h.pair, h.pairBad = &p, 0
	h.mu.Unlock()
	return p, nil
}

// Pairing — действующий код привязки, если он есть и не истёк.
func (h *Hub) Pairing() (Pairing, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pair == nil || time.Now().Unix() > h.pair.ExpiresAt {
		h.pair = nil
		return Pairing{}, false
	}
	return *h.pair, true
}

func (h *Hub) CancelPairing() { h.mu.Lock(); h.pair = nil; h.mu.Unlock() }

// Devices — привязанные телефоны и на связи ли они сейчас.
func (h *Hub) Devices(ctx context.Context) ([]Device, error) {
	list, err := h.st.NotifyDevices(ctx)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Device, 0, len(list))
	for _, d := range list {
		out = append(out, Device{ID: d.ID, Name: d.Name, PairedAt: d.PairedAt, LastSeen: d.LastSeen, Online: len(h.conns[d.ID]) > 0})
	}
	return out, nil
}

// Unpair отвязывает телефон и рвёт его соединение.
func (h *Hub) Unpair(ctx context.Context, id string) error {
	if err := h.st.NotifyDeleteDevice(ctx, id); err != nil {
		return err
	}
	h.mu.Lock()
	set := h.conns[id]
	delete(h.conns, id)
	h.mu.Unlock()
	for c := range set {
		c.c.Close()
	}
	h.onChange()
	return nil
}

// Publish кладёт событие в журнал и шлёт всем телефонам на связи.
// Телефон не на связи получит его при следующем подключении.
func (h *Hub) Publish(ctx context.Context, e Event) (Event, error) {
	if e.At == 0 {
		e.At = time.Now().Unix()
	}
	e.Seq = 0
	body, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	seq, err := h.st.NotifyAddEvent(ctx, string(body))
	if err != nil {
		return e, err
	}
	e.Seq = seq
	h.broadcast(frame{T: "event", Event: &e})
	return e, nil
}

func (h *Hub) broadcast(f frame) {
	h.mu.Lock()
	var all []*conn
	for _, set := range h.conns {
		for c := range set {
			all = append(all, c)
		}
	}
	h.mu.Unlock()
	for _, c := range all {
		if err := c.send(f); err != nil {
			c.c.Close()
		}
	}
}

func (h *Hub) accept(ln net.Listener) {
	defer h.wg.Done()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer c.Close()
			h.serve(c)
		}()
	}
}

func (h *Hub) serve(nc net.Conn) {
	_ = nc.SetDeadline(time.Now().Add(20 * time.Second))
	br := bufio.NewReaderSize(nc, 4096)
	line, err := readLine(br, 128)
	if err != nil {
		return
	}
	parts := strings.Fields(line)
	if len(parts) < 2 || parts[0] != "SINAPS1" {
		return
	}
	switch parts[1] {
	case "PAIR":
		h.servePair(nc, br)
	case "HELLO":
		if len(parts) == 3 {
			h.serveDevice(nc, br, parts[2])
		}
	}
}

func (h *Hub) servePair(nc net.Conn, br *bufio.Reader) {
	h.mu.Lock()
	p := h.pair
	h.mu.Unlock()
	if p == nil || time.Now().Unix() > p.ExpiresAt {
		return
	}
	aead, err := newAEAD(PairKey(p.Code))
	if err != nil {
		return
	}
	c := &conn{c: nc, aead: aead}
	f, err := c.recv(br)
	if err != nil || f.T != "pair" {
		// неверный код → кадр не расшифровался; после нескольких попыток код сгорает
		h.mu.Lock()
		if h.pair == p {
			if h.pairBad++; h.pairBad >= pairMaxBad {
				h.pair = nil
			}
		}
		h.mu.Unlock()
		return
	}
	key := make([]byte, 32)
	idb := make([]byte, 8)
	if _, err := rand.Read(key); err != nil {
		return
	}
	if _, err := rand.Read(idb); err != nil {
		return
	}
	d := store.NotifyDevice{ID: hex.EncodeToString(idb), Name: cleanName(f.Name), Key: key, PairedAt: time.Now().Unix()}
	if err := h.st.NotifySaveDevice(context.Background(), d); err != nil {
		return
	}
	h.mu.Lock()
	if h.pair == p {
		h.pair = nil // код одноразовый
	}
	h.mu.Unlock()
	_ = c.send(frame{T: "paired", ID: d.ID, Key: base64.StdEncoding.EncodeToString(key), CRM: h.name})
	h.onChange()
}

func (h *Hub) serveDevice(nc net.Conn, br *bufio.Reader, id string) {
	ctx := context.Background()
	list, err := h.st.NotifyDevices(ctx)
	if err != nil {
		return
	}
	var dev *store.NotifyDevice
	for i := range list {
		if list[i].ID == id {
			dev = &list[i]
		}
	}
	if dev == nil {
		return // телефон отвязан
	}
	aead, err := newAEAD(dev.Key)
	if err != nil {
		return
	}
	c := &conn{c: nc, aead: aead}
	f, err := c.recv(br)
	if err != nil || f.T != "hello" {
		return
	}
	_ = h.st.NotifyTouchDevice(ctx, id, cleanName(f.Name))

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	cfg := h.cfg
	if h.conns[id] == nil {
		h.conns[id] = map[*conn]bool{}
	}
	h.conns[id][c] = true
	h.mu.Unlock()
	h.onChange()
	defer func() {
		h.mu.Lock()
		if set := h.conns[id]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(h.conns, id)
			}
		}
		h.mu.Unlock()
		_ = h.st.NotifyTouchDevice(ctx, id, "")
		h.onChange()
	}()

	g := cfg.Group
	if err := c.send(frame{T: "welcome", CRM: h.name, Seq: h.st.NotifyLastSeq(ctx), Show: cfg.Show, Group: &g}); err != nil {
		return
	}
	// что телефон пропустил, пока был не на связи
	seqs, bodies, err := h.st.NotifyEventsAfter(ctx, f.Last, replayLimit)
	if err != nil {
		return
	}
	for i, b := range bodies {
		var e Event
		if json.Unmarshal([]byte(b), &e) != nil {
			continue
		}
		e.Seq = seqs[i]
		if err := c.send(frame{T: "event", Event: &e}); err != nil {
			return
		}
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(pingEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if c.send(frame{T: "ping"}) != nil {
					nc.Close()
					return
				}
			}
		}
	}()
	for {
		_ = nc.SetReadDeadline(time.Now().Add(readTimeout))
		if _, err := c.recv(br); err != nil {
			return
		}
	}
}

// ---------- кадры и шифрование ----------

// PairKey — ключ кадров привязки из одноразового кода.
func PairKey(code string) []byte {
	k, _ := pbkdf2.Key(sha256.New, code, []byte(pairSalt), pairIter, 32)
	return k
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func (c *conn) send(f frame) error {
	plain, err := json.Marshal(f)
	if err != nil {
		return err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	body := c.aead.Seal(nonce, nonce, plain, nil)
	buf := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(buf, uint32(len(body)))
	copy(buf[4:], body)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.c.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err = c.c.Write(buf)
	return err
}

func (c *conn) recv(br *bufio.Reader) (frame, error) {
	var f frame
	var hdr [4]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return f, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	ns := c.aead.NonceSize()
	if n < uint32(ns+c.aead.Overhead()) || n > maxFrame {
		return f, errors.New("notify: неверная длина кадра")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(br, body); err != nil {
		return f, err
	}
	plain, err := c.aead.Open(nil, body[:ns], body[ns:], nil)
	if err != nil {
		return f, errors.New("notify: кадр не расшифрован")
	}
	return f, json.Unmarshal(plain, &f)
}

func readLine(br *bufio.Reader, max int) (string, error) {
	var b []byte
	for len(b) <= max {
		c, err := br.ReadByte()
		if err != nil {
			return "", err
		}
		if c == '\n' {
			return strings.TrimRight(string(b), "\r"), nil
		}
		b = append(b, c)
	}
	return "", errors.New("notify: слишком длинная строка")
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return s
}

// LocalAddrs — адреса этого компьютера в локальной сети (для QR-кода), частные IPv4 первыми.
func LocalAddrs() []string {
	var out []string
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, in := range ifs {
		if in.Flags&net.FlagUp == 0 || in.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := in.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || ip.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ip.String())
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := net.ParseIP(out[i]).IsPrivate(), net.ParseIP(out[j]).IsPrivate()
		return pi && !pj
	})
	return out
}
