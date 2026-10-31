// Package config — настройки приложения в config.json рядом с базой.
// Только то, что относится к самому приложению (тема, плагины);
// данные CRM и состояние синхронизации живут в SQLite.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

type Config struct {
	Theme   string                  `json:"theme"`
	Plugins map[string]PluginConfig `json:"plugins"`
}

// PluginConfig — включён ли плагин и его собственные настройки.
type PluginConfig struct {
	Enabled  bool           `json:"enabled"`
	Settings map[string]any `json:"settings,omitempty"`
}

func Default() Config {
	return Config{Theme: ThemeDark, Plugins: map[string]PluginConfig{}}
}

type Store struct {
	path string
	mu   sync.Mutex
	cfg  Config
}

// Load читает config.json; если файла нет — создаёт с настройками по умолчанию.
func Load(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, "config.json"), cfg: Default()}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, s.save()
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &s.cfg); err != nil {
		// битый файл не должен ронять приложение — откатываемся к умолчаниям
		_ = os.Rename(s.path, s.path+".bad")
		s.cfg = Default()
		return s, s.save()
	}
	s.normalize()
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cfg
	c.Plugins = make(map[string]PluginConfig, len(s.cfg.Plugins))
	for k, v := range s.cfg.Plugins {
		c.Plugins[k] = v
	}
	return c
}

// Update меняет конфиг и сразу пишет файл.
func (s *Store) Update(f func(*Config)) (Config, error) {
	s.mu.Lock()
	f(&s.cfg)
	s.normalize()
	err := s.save()
	s.mu.Unlock()
	return s.Get(), err
}

func (s *Store) normalize() {
	if s.cfg.Theme != ThemeLight {
		s.cfg.Theme = ThemeDark
	}
	if s.cfg.Plugins == nil {
		s.cfg.Plugins = map[string]PluginConfig{}
	}
}

// save пишет атомарно: во временный файл, потом rename.
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
