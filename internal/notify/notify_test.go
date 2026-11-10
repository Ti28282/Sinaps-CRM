package notify

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"sinaps-crm/internal/store"
)

// phone — телефон в тестах: тот же протокол, что в приложении.
type phone struct {
	c  *conn
	br *bufio.Reader
}

func dial(t *testing.T, port int, first string, key []byte) *phone {
	t.Helper()
	nc, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { nc.Close() })
	if _, err := nc.Write([]byte(first + "\n")); err != nil {
		t.Fatal(err)
	}
	aead, err := newAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	_ = nc.SetDeadline(time.Now().Add(5 * time.Second))
	return &phone{c: &conn{c: nc, aead: aead}, br: bufio.NewReader(nc)}
}

func (p *phone) next(t *testing.T, want string) frame {
	t.Helper()
	for {
		f, err := p.c.recv(p.br)
		if err != nil {
			t.Fatalf("ждали %q: %v", want, err)
		}
		if f.T == "ping" {
			continue
		}
		if f.T != want {
			t.Fatalf("ждали %q, пришло %q", want, f.T)
		}
		return f
	}
}

func TestPairHelloReplay(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatal(err)
	}
	changes := 0
	h := New(st, "SINAPS CRM", func() { changes++ })
	if err := h.Start(DefaultPort + 200); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()

	// событие до привязки — телефон получит его при первом подключении
	if _, err := h.Publish(ctx, Event{Kind: "msg", Title: "Клиент написал · Pulse", Text: "раз", ChatID: 5}); err != nil {
		t.Fatal(err)
	}

	// неверный код: кадр не расшифруется, устройства не появится
	pr, err := h.StartPairing()
	if err != nil || len(pr.Code) != 8 {
		t.Fatalf("pairing: %v %+v", err, pr)
	}
	bad := dial(t, h.Port(), "SINAPS1 PAIR", PairKey("00000000"))
	_ = bad.c.send(frame{T: "pair", Name: "чужой"})
	if _, err := bad.c.recv(bad.br); err == nil {
		t.Fatal("неверный код не должен привязывать")
	}
	if ds, _ := h.Devices(ctx); len(ds) != 0 {
		t.Fatal("устройство привязалось с неверным кодом")
	}

	// привязка верным кодом
	p := dial(t, h.Port(), "SINAPS1 PAIR", PairKey(pr.Code))
	if err := p.c.send(frame{T: "pair", Name: "Samsung A54"}); err != nil {
		t.Fatal(err)
	}
	paired := p.next(t, "paired")
	key, err := base64.StdEncoding.DecodeString(paired.Key)
	if err != nil || len(key) != 32 || paired.ID == "" || paired.CRM != "SINAPS CRM" {
		t.Fatalf("paired: %+v", paired)
	}
	// код одноразовый
	again := dial(t, h.Port(), "SINAPS1 PAIR", PairKey(pr.Code))
	_ = again.c.send(frame{T: "pair", Name: "второй"})
	if _, err := again.c.recv(again.br); err == nil {
		t.Fatal("код сработал второй раз")
	}

	// подключение с постоянным ключом: welcome + пропущенное событие
	d := dial(t, h.Port(), "SINAPS1 HELLO "+paired.ID, key)
	if err := d.c.send(frame{T: "hello", Name: "Samsung A54", Last: 0}); err != nil {
		t.Fatal(err)
	}
	w := d.next(t, "welcome")
	if w.Show != "full" || w.Group == nil || !*w.Group || w.Seq != 1 {
		t.Fatalf("welcome: %+v", w)
	}
	ev := d.next(t, "event")
	if ev.Event == nil || ev.Event.Seq != 1 || ev.Event.Text != "раз" || ev.Event.ChatID != 5 {
		t.Fatalf("replay: %+v", ev.Event)
	}
	// ждём, пока сервер зарегистрирует соединение, и шлём живое событие
	deadline := time.Now().Add(2 * time.Second)
	for {
		ds, _ := h.Devices(ctx)
		if len(ds) == 1 && ds[0].Online && ds[0].Name == "Samsung A54" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("телефон не на связи: %+v", ds)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := h.Publish(ctx, Event{Kind: "fresh", Title: "Новый клиент", Text: "два", Quiet: true}); err != nil {
		t.Fatal(err)
	}
	live := d.next(t, "event")
	if live.Event.Seq != 2 || !live.Event.Quiet || live.Event.Kind != "fresh" {
		t.Fatalf("live: %+v", live.Event)
	}
	h.SetConfig(Config{Show: "name", Group: false})
	cf := d.next(t, "config")
	if cf.Show != "name" || cf.Group == nil || *cf.Group {
		t.Fatalf("config: %+v", cf)
	}

	// чужой ключ не проходит
	x := dial(t, h.Port(), "SINAPS1 HELLO "+paired.ID, PairKey("11111111"))
	_ = x.c.send(frame{T: "hello"})
	if _, err := x.c.recv(x.br); err == nil {
		t.Fatal("чужой ключ принят")
	}

	// отвязка рвёт соединение, телефон больше не подключится
	if err := h.Unpair(ctx, paired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.c.recv(d.br); err == nil {
		t.Fatal("соединение не закрыто после отвязки")
	}
	z := dial(t, h.Port(), "SINAPS1 HELLO "+paired.ID, key)
	_ = z.c.send(frame{T: "hello"})
	if _, err := z.c.recv(z.br); err == nil {
		t.Fatal("отвязанный телефон подключился")
	}
	if changes == 0 {
		t.Fatal("onChange не вызывался")
	}
}
