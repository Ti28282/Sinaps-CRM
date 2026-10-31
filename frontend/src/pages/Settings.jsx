import { useEffect, useState } from 'react'
import { api, applyTheme, errText } from '../api.js'
import { Icon } from '../components/ui.jsx'

const TABS = [
  { id: 'appearance', label: 'Внешний вид' },
  { id: 'sync', label: 'Синхронизация' },
  { id: 'plugins', label: 'Плагины' },
]

export default function Settings({ settings, reloadSettings, toast }) {
  const [tab, setTab] = useState('appearance')
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Настройки</h1>
          <p>Хранятся в config.json рядом с базой</p>
        </div>
      </header>

      <div className="seg" role="tablist" aria-label="Разделы настроек" style={{ alignSelf: 'flex-start' }}>
        {TABS.map((t) => (
          <button key={t.id} type="button" role="tab" aria-selected={tab === t.id} aria-pressed={tab === t.id} onClick={() => setTab(t.id)}>{t.label}</button>
        ))}
      </div>

      <div className="narrow" style={{ margin: 0, display: 'flex', flexDirection: 'column', gap: 20 }}>
        {tab === 'appearance' && <Appearance toast={toast} />}
        {tab === 'sync' && <Sync settings={settings} reloadSettings={reloadSettings} toast={toast} />}
        {tab === 'plugins' && <Plugins toast={toast} />}
      </div>
    </div>
  )
}

function Appearance({ toast }) {
  const [theme, setTheme] = useState(document.documentElement.dataset.theme || 'dark')

  async function pick(t) {
    setTheme(t)
    applyTheme(t)
    try { await api.setTheme(t) } catch (e) { toast(errText(e), 'err') }
  }

  return (
    <section className="panel">
      <h2>Тема</h2>
      <div className="theme-grid" role="radiogroup" aria-label="Тема оформления">
        {[
          { id: 'dark', label: 'Тёмная', icon: 'moon' },
          { id: 'light', label: 'Светлая', icon: 'sun' },
        ].map((t) => (
          <button key={t.id} type="button" role="radio" aria-checked={theme === t.id} className="theme-card" onClick={() => pick(t.id)}>
            <ThemePreview kind={t.id} />
            <span className="theme-label">
              <span className="radio" aria-hidden="true" />
              <Icon name={t.icon} size={16} />
              {t.label}
            </span>
          </button>
        ))}
      </div>
    </section>
  )
}

// мини-макет окна в цветах темы
function ThemePreview({ kind }) {
  const c = kind === 'light'
    ? { bg: '#F5F6F8', side: '#FFFFFF', card: '#FFFFFF', line: '#E3E6EA', text: '#C5CAD1' }
    : { bg: '#0D0E11', side: '#111216', card: '#181A20', line: '#23262D', text: '#33363F' }
  return (
    <div className="theme-preview" style={{ background: c.bg, borderColor: c.line }} aria-hidden="true">
      <div style={{ width: '26%', background: c.side, borderRight: `1px solid ${c.line}`, padding: 8, display: 'flex', flexDirection: 'column', gap: 6 }}>
        <div style={{ width: 14, height: 14, borderRadius: 4, background: '#C8F25A' }} />
        <div style={{ height: 5, borderRadius: 3, background: c.text, width: '80%' }} />
        <div style={{ height: 5, borderRadius: 3, background: c.text, width: '60%' }} />
        <div style={{ height: 5, borderRadius: 3, background: c.text, width: '70%' }} />
      </div>
      <div style={{ flex: 1, padding: 8, display: 'flex', gap: 6 }}>
        {[0, 1, 2].map((i) => (
          <div key={i} style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 5 }}>
            {[0, 1].slice(0, i === 1 ? 1 : 2).map((j) => (
              <div key={j} style={{ height: 26, borderRadius: 5, background: c.card, border: `1px solid ${c.line}` }} />
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}

function Sync({ settings, reloadSettings, toast }) {
  const [info, setInfo] = useState(null)
  useEffect(() => { api.info().then(setInfo) }, [])
  if (!settings) return null

  const save = async (p) => {
    try { await api.saveSettings(p); reloadSettings() } catch (e) { toast(errText(e), 'err') }
  }

  return (
    <section className="panel">
      <h2>Синхронизация и рассылки</h2>
      <div className="row" style={{ gap: 24, alignItems: 'flex-end' }}>
        <label className="field" style={{ width: 220 }}>
          <span>Сообщений истории на чат</span>
          <select className="input" value={settings.historyLimit} onChange={(e) => save({ historyLimit: Number(e.target.value) })}>
            {[50, 100, 200, 500, 1000].map((n) => <option key={n} value={n}>{n}</option>)}
          </select>
        </label>
        <label className="field" style={{ width: 220 }}>
          <span>Лимит рассылки в день</span>
          <select className="input" value={settings.dailyLimit} onChange={(e) => save({ dailyLimit: Number(e.target.value) })}>
            {[10, 15, 20, 30, 50].map((n) => <option key={n} value={n}>{n}</option>)}
          </select>
        </label>
        <label className="check" style={{ height: 44 }}>
          <input type="checkbox" checked={settings.autoSync} onChange={(e) => save({ autoSync: e.target.checked })} />
          Синхронизировать при запуске
        </label>
      </div>
      {info && (
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <p className="hint">Данные: <span className="mono">{info.dataDir}</span> · версия {info.version}</p>
          <button type="button" className="btn btn-sm" onClick={() => api.openDataDir()}><Icon name="folder" size={16} /> Открыть папку</button>
        </div>
      )}
    </section>
  )
}

function Plugins({ toast }) {
  const [list, setList] = useState(null)
  useEffect(() => { api.plugins().then(setList).catch((e) => toast(errText(e), 'err')) }, []) // eslint-disable-line react-hooks/exhaustive-deps

  async function toggle(p) {
    try {
      await api.setPluginEnabled(p.id, !p.enabled)
      setList((l) => l.map((x) => (x.id === p.id ? { ...x, enabled: !x.enabled } : x)))
    } catch (e) { toast(errText(e), 'err') }
  }

  if (!list) return <section className="panel"><p className="hint">Загрузка…</p></section>

  return (
    <section className="panel">
      <h2>Плагины</h2>
      {list.length === 0 ? (
        <div className="plugins-empty">
          <div className="plugins-empty-ico"><Icon name="puzzle" size={26} /></div>
          <div>
            <div style={{ fontWeight: 600, fontSize: 15 }}>Плагинов пока нет</div>
            <p className="hint" style={{ marginTop: 6, maxWidth: 520 }}>
              Здесь будут расширения: WhatsApp, ИИ-черновики ответов, экспорт в таблицы. Каждый можно будет включить и настроить отдельно.
            </p>
          </div>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {list.map((p) => (
            <div key={p.id} className="account">
              <Icon name="puzzle" />
              <div style={{ minWidth: 0 }}>
                <div style={{ fontWeight: 600 }}>{p.name} <span className="hint mono" style={{ fontSize: 12 }}>v{p.version}</span></div>
                <div className="hint" style={{ fontSize: 13 }}>{p.description}</div>
              </div>
              <label className="check" style={{ marginLeft: 'auto' }}>
                <input type="checkbox" checked={p.enabled} onChange={() => toggle(p)} />
                <span className="sr">Включить {p.name}</span>
              </label>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
