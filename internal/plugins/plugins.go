// Package plugins — заготовка под расширения CRM (WhatsApp, ИИ-ответы, экспорт и т.п.).
//
// Плагин регистрируется в init() своего пакета через Register, получает свои
// настройки из config.json и включается/выключается на экране «Настройки → Плагины».
// Пока ни одного плагина нет — реестр пустой.
package plugins

import (
	"context"
	"sort"
	"sync"
)

// Field описывает одно поле настроек плагина — по нему фронт строит форму.
type Field struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"` // text | password | number | bool | select
	Options []string `json:"options,omitempty"`
	Default any      `json:"default,omitempty"`
}

// Info — то, что видно в списке плагинов.
type Info struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Version     string  `json:"version"`
	Fields      []Field `json:"fields"`
}

// Plugin — контракт расширения.
type Plugin interface {
	Info() Info
	// Start вызывается при включении плагина и при запуске приложения, если он включён.
	Start(ctx context.Context, settings map[string]any) error
	// Stop — при выключении и закрытии приложения.
	Stop() error
}

var (
	mu       sync.RWMutex
	registry = map[string]Plugin{}
)

func Register(p Plugin) {
	mu.Lock()
	registry[p.Info().ID] = p
	mu.Unlock()
}

func Get(id string) (Plugin, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[id]
	return p, ok
}

// List — все доступные плагины по алфавиту.
func List() []Info {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Info, 0, len(registry))
	for _, p := range registry {
		out = append(out, p.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
