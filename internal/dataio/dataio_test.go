package dataio

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"

	"sinaps-crm/internal/store"
)

func seed(t *testing.T) (*store.Store, string) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "crm.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, _, _ := st.UpsertChat(ctx, store.Chat{Channel: "tg", ExtID: "u1", Kind: "user", PeerID: 1, Title: "Фитнес-клуб Pulse", Phone: "79001112233"})
	deal, amt := "deal", int64(60000)
	_ = st.PatchChat(ctx, id, store.ChatPatch{Stage: &deal, Amount: &amt})
	_, _ = st.AddMessage(ctx, store.Message{ChatID: id, MsgID: 1, Text: "Привет; \"кавычки\"", Date: 100}, false)
	return st, dir
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, dir := seed(t)

	// CSV: выгрузили, поменяли сумму и добавили новую строку — загрузили обратно
	csvPath := filepath.Join(dir, "out.csv")
	if n, err := ExportCSV(ctx, st, csvPath); err != nil || n != 1 {
		t.Fatal(err, n)
	}
	rows, err := ReadTable(csvPath)
	if err != nil || len(rows) != 2 || rows[1][3] != "Фитнес-клуб Pulse" || rows[1][6] != "Сделка" {
		t.Fatalf("%v %v", err, rows)
	}
	rows[1][7] = "75 000"
	rows = append(rows, []string{"", "", "", "Кофейня Зерно", "", "+7 (900) 555-44-33", "Новые", "35000", "", "кофе", ""})
	f, _ := os.Create(filepath.Join(dir, "in.csv"))
	f.WriteString("\xef\xbb\xbf")
	for _, r := range rows {
		line := ""
		for i, c := range r {
			if i > 0 {
				line += ";"
			}
			line += "\"" + c + "\""
		}
		f.WriteString(line + "\n")
	}
	f.Close()
	pv, err := PreviewTable(ctx, st, filepath.Join(dir, "in.csv"))
	if err != nil || pv.Rows != 2 || pv.Update != 1 || pv.Create != 1 {
		t.Fatalf("%v %+v", err, pv)
	}
	res, err := ApplyTable(ctx, st, filepath.Join(dir, "in.csv"))
	if err != nil || res.Created != 1 || res.Updated != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	all, _ := st.Chats(ctx, "all", "")
	if len(all) != 2 {
		t.Fatal("chats", len(all))
	}
	for _, c := range all {
		if c.ExtID == "u1" && c.Amount != 75000 {
			t.Fatal("amount not updated", c.Amount)
		}
		if c.Channel == "manual" && (c.Title != "Кофейня Зерно" || c.Amount != 35000 || c.Tags != "кофе") {
			t.Fatalf("%+v", c)
		}
	}
	// повторный импорт того же файла не плодит дубли
	res, _ = ApplyTable(ctx, st, filepath.Join(dir, "in.csv"))
	if res.Created != 0 || res.Updated != 2 {
		t.Fatalf("reimport %+v", res)
	}

	// XLSX
	x := filepath.Join(dir, "out.xlsx")
	if _, err := ExportXLSX(ctx, st, x); err != nil {
		t.Fatal(err)
	}
	xf, err := excelize.OpenFile(x)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := xf.GetRows("Сообщения")
	xf.Close()
	if len(msgs) != 2 || msgs[1][4] != "Привет; \"кавычки\"" {
		t.Fatalf("%v", msgs)
	}
	pv, err = PreviewTable(ctx, st, x)
	if err != nil || pv.Update != 2 || pv.Create != 0 {
		t.Fatalf("xlsx preview %v %+v", err, pv)
	}

	// CSV из русского Excel: Windows-1251 и запятые
	cp, _ := charmap.Windows1251.NewEncoder().String("Имя,Телефон,Статус\nСтоматология Улыбка,+79005551122,Ответил\n")
	os.WriteFile(filepath.Join(dir, "cp.csv"), []byte(cp), 0o600)
	pv, err = PreviewTable(ctx, st, filepath.Join(dir, "cp.csv"))
	if err != nil || pv.Create != 1 || pv.Sample[0][0] != "Стоматология Улыбка" || pv.Sample[0][1] != "Ответил" {
		t.Fatalf("cp1251 %v %+v", err, pv)
	}

	// SQLite: копия валидна, чужой файл — нет
	db := filepath.Join(dir, "copy.db")
	if err := ExportSQLite(ctx, st, db); err != nil {
		t.Fatal(err)
	}
	if p, err := PreviewSQLite(db); err != nil || p.Chats != 2 || p.Messages != 1 {
		t.Fatalf("%v %+v", err, p)
	}
	os.WriteFile(filepath.Join(dir, "junk.db"), []byte("not a db"), 0o600)
	if _, err := PreviewSQLite(filepath.Join(dir, "junk.db")); err == nil {
		t.Fatal("junk accepted")
	}
}
