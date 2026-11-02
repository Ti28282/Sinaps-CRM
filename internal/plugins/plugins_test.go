package plugins

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "cheki.sinaps"), map[string]string{
		"manifest.json": `{"id":"cheki","name":"Чеки","version":"0.1","crm":">=0.2"}`,
		"settings.json": `{"fields":[{"key":"currency","label":"Валюта","type":"select","options":["₽ рубль","сом"],"default":"₽ рубль"}]}`,
	})
	writeZip(t, filepath.Join(dir, "moy-nalog.sinaps"), map[string]string{
		"manifest.json": `{"id":"moy-nalog","name":"Мой налог","requires":["cheki"]}`,
	})
	writeZip(t, filepath.Join(dir, "new.sinaps"), map[string]string{
		"manifest.json": `{"id":"future","name":"Будущий","crm":">=9.0"}`,
	})
	writeZip(t, filepath.Join(dir, "photos.sinaps"), map[string]string{"a.jpg": "x"})
	writeZip(t, filepath.Join(dir, "dup.sinaps"), map[string]string{
		"manifest.json": `{"id":"cheki","name":"Чеки 2"}`,
	})
	os.WriteFile(filepath.Join(dir, "broken.sinaps"), []byte("not a zip"), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o600)

	builtin := map[string]bool{"cheki": true, "moy-nalog": true, "future": true}
	got, err := Scan(dir, "0.2.0", builtin, map[string]bool{"moy-nalog": true})
	if err != nil {
		t.Fatal(err)
	}
	st := map[string]Entry{}
	for _, e := range got {
		st[e.File] = e
	}
	if len(got) != 6 {
		t.Fatalf("want 6 .sinaps entries, got %d", len(got))
	}
	if _, ok := st["notes.txt"]; ok {
		t.Fatal("txt must be ignored")
	}
	check := func(file, status string) {
		t.Helper()
		if st[file].Status != status {
			t.Errorf("%s: want %s, got %s (%s)", file, status, st[file].Status, st[file].Error)
		}
	}
	check("cheki.sinaps", StatusOff)
	check("moy-nalog.sinaps", StatusWait)
	check("new.sinaps", StatusError)
	check("photos.sinaps", StatusInvalid)
	check("broken.sinaps", StatusInvalid)
	check("dup.sinaps", StatusError) // id «cheki» уже занят файлом cheki.sinaps

	if f := st["cheki.sinaps"].Fields; len(f) != 1 || f[0].Default != "₽ рубль" {
		t.Errorf("fields not parsed: %+v", f)
	}

	got, _ = Scan(dir, "0.2.0", builtin, map[string]bool{"cheki": true, "moy-nalog": true})
	for _, e := range got {
		if e.File == "moy-nalog.sinaps" && e.Status != StatusOn {
			t.Errorf("moy-nalog should be on when cheki on, got %s", e.Status)
		}
	}
}

func TestVersion(t *testing.T) {
	for _, c := range []struct {
		have, req string
		ok        bool
	}{
		{"0.2.0", ">=0.2", true}, {"0.2.0", ">=0.3", false}, {"1.10.0", ">=1.9", true}, {"0.2.0", "", true},
	} {
		if versionOK(c.have, c.req) != c.ok {
			t.Errorf("%s %s", c.have, c.req)
		}
	}
}

