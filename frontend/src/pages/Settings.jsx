import { useEffect, useState } from 'react'
import { api, applyTheme, errText } from '../api.js'
import { Icon } from '../components/ui.jsx'

const TABS = [
  { id: 'appearance', label: 'Внешний вид' },
  { id: 'sync', label: 'Синхронизация' },
  { id: 'data', label: 'Данные' },
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
        {tab === 'data' && <DataIO toast={toast} />}
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

const EXPORTS = [
  { id: 'xlsx', title: 'Excel (.xlsx)', text: 'Клиенты и вся переписка на двух листах. Фильтры, суммы считаются.', icon: 'file' },
  { id: 'csv', title: 'CSV (.csv)', text: 'Только клиенты. Открывается в Excel, Google Таблицах, любой CRM.', icon: 'file' },
  { id: 'sqlite', title: 'Копия базы (.db)', text: 'Полный бэкап: клиенты, переписка, история. Для восстановления.', icon: 'folder' },
]

function DataIO({ toast }) {
  const [busy, setBusy] = useState('')
  const [preview, setPreview] = useState(null) // { path, ...Preview }
  const [result, setResult] = useState(null)

  async function doExport(fmt) {
    setBusy(fmt)
    try {
      const r = await api.exportData(fmt)
      if (r.path) {
        toast(`Сохранено${r.count ? `: ${r.count} клиентов` : ''} → ${r.path.split(/[\\/]/).pop()}`)
        setResult({ kind: 'export', path: r.path })
      }
    } catch (e) { toast(errText(e), 'err') } finally { setBusy('') }
  }

  async function pick() {
    setResult(null)
    try {
      const path = await api.pickImportFile()
      if (!path) return
      setBusy('preview')
      const p = await api.previewImport(path)
      setPreview({ ...p, path })
    } catch (e) { toast(errText(e), 'err') } finally { setBusy('') }
  }

  async function apply() {
    const p = preview
    if (p.kind === 'sqlite') {
      if (!window.confirm(`Заменить всю базу CRM файлом «${p.file}» (${p.chats} клиентов, ${p.messages} сообщений)? Текущая база сохранится рядом как резервная копия crm.db.bak-<дата>. Приложение перезапустится.`)) return
      setBusy('import')
      try { await api.restoreDatabase(p.path) } catch (e) { toast(errText(e), 'err'); setBusy('') }
      return
    }
    setBusy('import')
    try {
      const r = await api.importTable(p.path)
      setPreview(null)
      setResult({ kind: 'import', ...r })
      toast(`Импорт: новых ${r.created}, обновлено ${r.updated}${r.skipped ? `, пропущено ${r.skipped}` : ''}`)
    } catch (e) { toast(errText(e), 'err') } finally { setBusy('') }
  }

  return (
    <>
      <section className="panel">
        <h2>Экспорт</h2>
        <div className="io-grid">
          {EXPORTS.map((x) => (
            <button key={x.id} type="button" className="io-card" disabled={!!busy} onClick={() => doExport(x.id)}>
              <span className="io-ico"><Icon name={x.icon} size={20} /></span>
              <span className="io-title">{busy === x.id ? 'Сохраняем…' : x.title}</span>
              <span className="hint" style={{ fontSize: 13 }}>{x.text}</span>
            </button>
          ))}
        </div>
        {result?.kind === 'export' && (
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <p className="hint mono" style={{ fontSize: 12, wordBreak: 'break-all' }}>{result.path}</p>
            <button type="button" className="btn btn-sm" onClick={() => api.showInFolder(result.path)}><Icon name="folder" size={16} /> Показать в папке</button>
          </div>
        )}
      </section>

      <section className="panel">
        <h2>Импорт</h2>
        <p className="hint">
          <b>CSV или Excel</b> — клиенты добавятся или обновятся. Совпадение ищется по ID канала, телефону, @username или точному названию;
          новые попадут в «Новые» с отметкой «Из файла». Нужна колонка «Название» (или «Имя»/«Телефон»); также понимаю «Этап», «Сумма», «Теги», «Заметка», «Следующий шаг».
          <br /><b>Копия базы .db</b> — заменяет всю базу (текущая сохраняется резервной копией).
        </p>
        <button type="button" className="btn" style={{ alignSelf: 'flex-start' }} disabled={!!busy} onClick={pick}>
          <Icon name="clip" size={16} /> {busy === 'preview' ? 'Читаем файл…' : 'Выбрать файл…'}
        </button>

        {result?.kind === 'import' && (
          <div className="io-result">
            Новых: <b>{result.created}</b> · обновлено: <b>{result.updated}</b>{result.skipped ? <> · пропущено: <b>{result.skipped}</b></> : null}
            {result.problems?.length > 0 && <ul>{result.problems.map((p, i) => <li key={i}>{p}</li>)}</ul>}
          </div>
        )}
      </section>

      {preview && (
        <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && setPreview(null)}>
          <div className="modal" role="dialog" aria-modal="true" aria-labelledby="imp-title">
            <div className="modal-head">
              <div>
                <h2 id="imp-title">Импорт «{preview.file}»</h2>
                <p className="hint" style={{ marginTop: 4 }}>Пока ничего не изменено. Проверьте и подтвердите.</p>
              </div>
              <button type="button" className="icon-btn" aria-label="Закрыть" onClick={() => setPreview(null)}><Icon name="close" /></button>
            </div>

            {preview.kind === 'sqlite' ? (
              <div className="io-result">
                В файле: <b>{preview.chats}</b> клиентов, <b>{preview.messages}</b> сообщений.<br />
                Текущая база будет <b>заменена целиком</b>. Перед заменой она сохранится как <span className="mono">crm.db.bak-&lt;дата&gt;</span>, приложение перезапустится.
              </div>
            ) : (
              <>
                <div className="stats">
                  <div className="stat" style={{ padding: '12px 16px', flex: '1 1 110px' }}><div className="stat-label">Строк</div><div className="stat-value" style={{ fontSize: 22 }}>{preview.rows}</div></div>
                  <div className="stat" style={{ padding: '12px 16px', flex: '1 1 110px' }}><div className="stat-label">Новых</div><div className="stat-value" style={{ fontSize: 22, color: 'var(--accent-text)' }}>{preview.create}</div></div>
                  <div className="stat" style={{ padding: '12px 16px', flex: '1 1 110px' }}><div className="stat-label">Обновить</div><div className="stat-value" style={{ fontSize: 22 }}>{preview.update}</div></div>
                  {preview.skip > 0 && <div className="stat" style={{ padding: '12px 16px', flex: '1 1 110px' }}><div className="stat-label">Пропуск</div><div className="stat-value" style={{ fontSize: 22 }}>{preview.skip}</div></div>}
                </div>
                {preview.sample.length > 0 && (
                  <table className="io-table">
                    <thead><tr><th>Клиент</th><th>Этап</th><th>Сумма</th><th>Действие</th></tr></thead>
                    <tbody>{preview.sample.map((r, i) => <tr key={i}>{r.map((c, j) => <td key={j}>{c || '—'}</td>)}</tr>)}</tbody>
                  </table>
                )}
                {preview.problems.length > 0 && <ul className="io-problems">{preview.problems.map((p, i) => <li key={i}>{p}</li>)}</ul>}
                <p className="hint" style={{ fontSize: 13 }}>Пустые ячейки не стирают данные в CRM. Telegram не затрагивается.</p>
              </>
            )}

            <div className="row" style={{ justifyContent: 'flex-end' }}>
              <button type="button" className="btn" onClick={() => setPreview(null)}>Отмена</button>
              <button type="button" className={'btn ' + (preview.kind === 'sqlite' ? 'btn-danger' : 'btn-primary')} disabled={busy === 'import' || (preview.kind === 'table' && preview.create + preview.update === 0)} onClick={apply}>
                {busy === 'import' ? 'Импортируем…' : preview.kind === 'sqlite' ? 'Заменить базу' : `Импортировать ${preview.create + preview.update}`}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
