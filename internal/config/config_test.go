package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSave(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil || s.Get().Theme != ThemeDark {
		t.Fatal(err, s.Get())
	}
	if _, err := s.Update(func(c *Config) { c.Theme = ThemeLight }); err != nil {
		t.Fatal(err)
	}
	s2, _ := Load(dir)
	if s2.Get().Theme != ThemeLight {
		t.Fatal("theme not persisted")
	}
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte("{oops"), 0o600)
	s3, err := Load(dir)
	if err != nil || s3.Get().Theme != ThemeDark {
		t.Fatal("bad file not recovered", err)
	}
	s3.Update(func(c *Config) { c.Theme = "neon" })
	if s3.Get().Theme != ThemeDark {
		t.Fatal("unknown theme accepted")
	}
}
