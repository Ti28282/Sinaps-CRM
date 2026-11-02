package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sinaps-crm/internal/config"
	"sinaps-crm/internal/plugins"
)

// Плагины, которые эта версия CRM умеет выполнять сама. Архив .sinaps с таким id
// включает возможность; архивы со своим кодом (plugin.wasm) — в следующих версиях.
const chekiID = "cheki"

var builtinPlugins = map[string]bool{chekiID: true}

// PluginsView — экран «Настройки → Плагины».
type PluginsView struct {
	Dir   string          `json:"dir"`
	Found int             `json:"found"`
	Items []plugins.Entry `json:"items"`
}

func (a *App) pluginsDir() string {
	if d := a.cfg.Get().PluginsDir; d != "" {
		return d
	}
	return filepath.Join(a.dataDir, "plugins")
}

// loadPlugins перечитывает папку и сообщает фронту, если что-то поменялось.
func (a *App) loadPlugins() PluginsView {
	dir := a.pluginsDir()
	_ = os.MkdirAll(dir, 0o700)
	enabled := map[string]bool{}
	for id, pc := range a.cfg.Get().Plugins {
		enabled[id] = pc.Enabled
	}
	list, err := plugins.Scan(dir, version, builtinPlugins, enabled)
	if err != nil {
		a.emit("sync:warn", "Папка плагинов: "+err.Error())
		list = []plugins.Entry{}
	}
	a.pmu.Lock()
	a.plugins = list
	a.pmu.Unlock()
	v := PluginsView{Dir: dir, Found: len(list), Items: list}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "plugins:changed", v)
	}
	return v
}

// pluginActive — плагин есть в папке, включён и все его зависимости работают.
func (a *App) pluginActive(id string) bool {
	a.pmu.RLock()
	defer a.pmu.RUnlock()
	for _, e := range a.plugins {
		if e.ID == id && e.Status == plugins.StatusOn {
			return true
		}
	}
	return false
}

func (a *App) pluginEntry(id string) (plugins.Entry, bool) {
	a.pmu.RLock()
	defer a.pmu.RUnlock()
	for _, e := range a.plugins {
		if e.ID == id && e.Usable() {
			return e, true
		}
	}
	return plugins.Entry{}, false
}

// Plugins — список плагинов (папка перечитывается).
func (a *App) Plugins() PluginsView { return a.loadPlugins() }

// SetPluginEnabled включает/выключает плагин. Данные плагина при выключении остаются.
func (a *App) SetPluginEnabled(id string, enabled bool) (PluginsView, error) {
	if _, ok := a.pluginEntry(id); !ok {
		return a.loadPlugins(), fmt.Errorf("плагин %q не найден в папке или не может работать", id)
	}
	if _, err := a.cfg.Update(func(c *config.Config) {
		pc := c.Plugins[id]
		pc.Enabled = enabled
		c.Plugins[id] = pc
	}); err != nil {
		return a.loadPlugins(), err
	}
	v := a.loadPlugins()
	a.emit("chats:changed", nil) // диалоги перерисуют отметки чеков
	return v, nil
}

// PluginSettings — сохранённые настройки плагина поверх значений по умолчанию.
func (a *App) PluginSettings(id string) map[string]any {
	out := map[string]any{}
	if e, ok := a.pluginEntry(id); ok {
		out = plugins.Defaults(e.Fields)
	}
	for k, v := range a.cfg.Get().Plugins[id].Settings {
		out[k] = v
	}
	return out
}

func (a *App) SavePluginSettings(id string, settings map[string]any) error {
	if _, ok := a.pluginEntry(id); !ok {
		return fmt.Errorf("плагин %q не найден", id)
	}
	_, err := a.cfg.Update(func(c *config.Config) {
		pc := c.Plugins[id]
		pc.Settings = settings
		c.Plugins[id] = pc
	})
	if err == nil {
		a.emit("plugin:settings", map[string]any{"id": id, "settings": a.PluginSettings(id)})
	}
	return err
}

func (a *App) OpenPluginsDir() error {
	dir := a.pluginsDir()
	_ = os.MkdirAll(dir, 0o700)
	return openPath(dir)
}

// ChoosePluginsDir — выбрать другую папку плагинов. Пустой Dir в ответе не бывает.
func (a *App) ChoosePluginsDir() (PluginsView, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Папка плагинов SINAPS CRM",
		DefaultDirectory: a.pluginsDir(),
	})
	if err != nil || dir == "" {
		return a.loadPlugins(), err
	}
	if dir == filepath.Join(a.dataDir, "plugins") {
		dir = "" // вернули папку по умолчанию
	}
	if _, err := a.cfg.Update(func(c *config.Config) { c.PluginsDir = dir }); err != nil {
		return a.loadPlugins(), err
	}
	return a.loadPlugins(), nil
}

// InstallPlugin копирует перетащенный архив .sinaps в папку плагинов.
// Новый плагин появляется выключенным.
func (a *App) InstallPlugin(path string) (PluginsView, error) {
	if _, err := plugins.Install(a.pluginsDir(), path); err != nil {
		return a.loadPlugins(), err
	}
	return a.loadPlugins(), nil
}

// RevealPlugin открывает папку плагинов (файл в ней пользователь найдёт по имени).
func (a *App) RevealPlugin(file string) error {
	if filepath.Base(file) != file {
		return errors.New("неверное имя файла")
	}
	return a.OpenPluginsDir()
}
