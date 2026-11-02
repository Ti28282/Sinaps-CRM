import { useEffect, useState } from 'react'
import { api, on, applyTheme, errText, money } from '../api.js'
import { Icon } from '../components/ui.jsx'

const TABS = [
  { id: 'appearance', label: 'Внешний вид' },
  { id: 'sync', label: 'Синхронизация' },
  { id: 'data', label: 'Данные' },
  { id: 'plugins', label: 'Плагины' },
]

export default function Settings({ settings, reloadSettings, toast, plugins, setPlugins, openPage }) {
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
        {tab === 'plugins' && <Plugins plugins={plugins} setPlugins={setPlugins} toast={toast} openPage={openPage} />}
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

const STATUS = {
  on: { label: 'Подключён', cls: 'st-on' },
  off: { label: 'Выключен', cls: 'st-off' },
  wait: { label: 'Ждёт', cls: 'st-wait' },
  error: { label: 'Ошибка', cls: 'st-err' },
  invalid: { label: 'Не плагин', cls: 'st-none' },
}

function Plugins({ plugins, setPlugins, toast, openPage }) {
  const [panel, setPanel] = useState(null) // id плагина с открытыми настройками
  const [busy, setBusy] = useState('')

  async function run(kind, fn) {
    setBusy(kind)
    try { const v = await fn(); if (v) setPlugins(v); return v } catch (e) { toast(errText(e), 'err') } finally { setBusy('') }
  }
  const refresh = () => run('scan', () => api.plugins())

  useEffect(() => { refresh() }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // архив .sinaps, брошенный в окно, — копируем в папку плагинов
  useEffect(() => on('files:dropped', async (paths) => {
    const list = (paths || []).filter((p) => /\.sinaps$/i.test(p))
    if (!list.length) return toast('Плагин — это архив с расширением .sinaps', 'err')
    for (const p of list) {
      const v = await run('install', () => api.installPlugin(p))
      if (v) toast(`Плагин «${p.split(/[\\/]/).pop()}» добавлен в папку — включите его в списке`)
    }
  }), []) // eslint-disable-line react-hooks/exhaustive-deps

  async function toggle(p) {
    const v = await run('toggle', () => api.setPluginEnabled(p.id, !p.enabled))
    if (v && !p.enabled) {
      const now = v.items.find((x) => x.id === p.id)
      if (now?.status === 'on') toast(`«${p.name}» подключён`)
    }
    if (p.enabled && panel === p.id) setPanel(null)
  }

  if (!plugins) return <section className="panel"><p className="hint">Загрузка…</p></section>
  const open = plugins.items.find((p) => p.id && p.id === panel && p.status === 'on')

  return (
    <>
      <section className="panel plug-folder" aria-label="Папка плагинов">
        <div className="plug-folder-ico"><Icon name="folder" size={22} /></div>
        <div style={{ flex: '1 1 260px', minWidth: 0 }}>
          <div className="hint" style={{ fontSize: 12 }}>Папка плагинов</div>
          <div className="mono" style={{ fontSize: 14, fontWeight: 500, marginTop: 3, overflowWrap: 'anywhere' }}>{plugins.dir}</div>
          <div className="hint" style={{ fontSize: 12, marginTop: 4 }}>Найдено: {plugins.found}</div>
        </div>
        <div className="row" style={{ gap: 8 }}>
          <button type="button" className="btn btn-sm btn-inv" onClick={() => api.openPluginsDir().catch((e) => toast(errText(e), 'err'))}>Открыть папку</button>
          <button type="button" className="btn btn-sm" onClick={refresh} disabled={!!busy}><Icon name="refresh" size={15} /> {busy === 'scan' ? 'Проверяем…' : 'Обновить'}</button>
          <button type="button" className="btn btn-sm btn-quiet" onClick={() => run('dir', () => api.choosePluginsDir())} disabled={!!busy}>Сменить папку…</button>
        </div>
      </section>

      <div className="plug-list">
        {plugins.items.length === 0 && (
          <div className="plugins-empty">
            <div className="plugins-empty-ico"><Icon name="puzzle" size={26} /></div>
            <div>
              <div style={{ fontWeight: 600, fontSize: 15 }}>В папке нет плагинов</div>
              <p className="hint" style={{ marginTop: 6, maxWidth: 520 }}>
                Плагин — это архив <span className="mono">.sinaps</span>. Положите его в папку плагинов или перетащите сюда, затем включите. Остальные файлы в папке не показываются.
              </p>
            </div>
          </div>
        )}
        {plugins.items.map((p) => <PluginRow key={p.file} p={p} busy={!!busy} toggle={() => toggle(p)} openSettings={() => setPanel(p.id)} reveal={() => api.revealPlugin(p.file)} />)}
        <div className="plug-drop" style={{ '--wails-drop-target': 'drop' }}>
          {busy === 'install' ? 'Копируем…' : <>Перетащите архив <span className="mono">.sinaps</span> сюда — он скопируется в папку и появится выключенным</>}
        </div>
      </div>

      {open && <PluginPanel p={open} toast={toast} close={() => setPanel(null)} openPage={openPage} />}
    </>
  )
}

function PluginRow({ p, busy, toggle, openSettings, reveal }) {
  const st = STATUS[p.status] || STATUS.invalid
  const bad = p.status === 'error' || p.status === 'invalid'
  const icon = bad ? (p.status === 'error' ? 'warn' : 'box') : (p.icon === 'receipt' || p.icon === 'spark' ? p.icon : 'puzzle')
  const detail = p.error || p.description
  return (
    <div className={'plug-row ' + st.cls}>
      <div className={'plug-ico' + (p.status === 'on' ? ' on' : '')}><Icon name={icon} size={22} /></div>
      <div style={{ minWidth: 0, flex: '1 1 220px' }}>
        <div className="row" style={{ gap: 8 }}>
          <span className="plug-name">{p.name || p.file.replace(/\.sinaps$/i, '')}</span>
          {p.version && <span className="mono hint" style={{ fontSize: 12 }}>v{p.version}</span>}
          <span className={'plug-chip ' + st.cls}><i aria-hidden="true" />{p.status === 'wait' ? `Ждёт «${(p.requiresNames || []).join('», «')}»` : st.label}</span>
        </div>
        <div className="mono plug-file">{p.file}</div>
        {detail && <div className="plug-detail">{detail}</div>}
      </div>
      <div className="row" style={{ marginLeft: 'auto', gap: 10 }}>
        {p.status === 'on' && p.fields?.length > 0 && (
          <button type="button" className="btn btn-sm" onClick={openSettings} aria-label={`Настройки плагина ${p.name}`}><Icon name="settings" size={16} /> Настройки</button>
        )}
        {bad ? (
          <button type="button" className="btn btn-sm btn-quiet" onClick={reveal}>Показать в папке</button>
        ) : (
          <button type="button" role="switch" aria-checked={p.enabled} aria-label={`Включить плагин ${p.name}`} className="switch" disabled={busy} onClick={toggle}><span /></button>
        )}
      </div>
    </div>
  )
}

function PluginPanel({ p, toast, close, openPage }) {
  const [vals, setVals] = useState(null)
  const [sum, setSum] = useState(null)
  const isCheki = p.id === 'cheki'

  useEffect(() => {
    api.pluginSettings(p.id).then(setVals).catch((e) => toast(errText(e), 'err'))
    if (isCheki) api.chekiSummary().then(setSum).catch(() => {})
    const onKey = (e) => e.key === 'Escape' && close()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [p.id]) // eslint-disable-line react-hooks/exhaustive-deps

  async function set(k, v) {
    const next = { ...vals, [k]: v }
    setVals(next)
    try { await api.savePluginSettings(p.id, next) } catch (e) { toast(errText(e), 'err') }
  }

  async function exportXlsx() {
    try {
      const r = await api.exportReceipts(0, 'all')
      if (r.path) toast(`Сохранено чеков: ${r.count} → ${r.path.split(/[\\/]/).pop()}`)
    } catch (e) { toast(errText(e), 'err') }
  }

  return (
    <div className="drawer-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <aside className="drawer" role="dialog" aria-modal="true" aria-labelledby="plug-title">
        <div className="row" style={{ gap: 14, flexWrap: 'nowrap' }}>
          <div className="plug-ico on" style={{ width: 48, height: 48 }}><Icon name={p.icon === 'receipt' ? 'receipt' : 'puzzle'} size={22} /></div>
          <div style={{ minWidth: 0 }}>
            <h2 id="plug-title" style={{ margin: 0, fontSize: 19 }}>{p.name} <span className="mono hint" style={{ fontSize: 12 }}>v{p.version}</span></h2>
            <div className="mono plug-file">{p.file}</div>
          </div>
          <button type="button" className="icon-btn" style={{ marginLeft: 'auto', width: 38, height: 38 }} aria-label="Закрыть" onClick={close}><Icon name="close" /></button>
        </div>
        {p.description && <p className="hint" style={{ marginTop: -6 }}>{p.description}</p>}

        {isCheki && sum && (
          <div className="stats" style={{ gap: 10 }}>
            <div className="stat mini"><div className="stat-label">Чеков отмечено</div><div className="stat-value">{sum.count}</div></div>
            <div className="stat mini"><div className="stat-label">Сумма за {sum.year}</div><div className="stat-value accent">{money(sum.sumYear, sum.currency)}</div></div>
            <div className="stat mini"><div className="stat-label">Клиентов с чеками</div><div className="stat-value">{sum.clients}</div></div>
          </div>
        )}

        {vals && (
          <div className="kv plug-form">
            {p.fields.map((f) => (
              <label key={f.key}>
                <span>{f.label}</span>
                {f.type === 'bool' ? (
                  <input type="checkbox" className="cb" checked={!!vals[f.key]} onChange={(e) => set(f.key, e.target.checked)} />
                ) : f.type === 'select' ? (
                  <select className="input input-sm" value={vals[f.key] ?? ''} onChange={(e) => set(f.key, e.target.value)}>
                    {f.options.map((o) => <option key={o} value={o}>{o}</option>)}
                  </select>
                ) : (
                  <input className="input input-sm" type={f.type === 'number' ? 'number' : 'text'} defaultValue={vals[f.key] ?? ''}
                    onBlur={(e) => set(f.key, f.type === 'number' ? Number(e.target.value) : e.target.value)} />
                )}
              </label>
            ))}
          </div>
        )}

        {isCheki && (
          <div className="row">
            <button type="button" className="btn btn-primary" onClick={() => { close(); openPage('cheki') }}>Открыть «Чеки» <Icon name="arrow" size={16} /></button>
            <button type="button" className="btn" onClick={exportXlsx}>Экспорт чеков в Excel</button>
          </div>
        )}
        <p className="hint">
          {isCheki ? 'Плагин работает только внутри CRM: ничего не отправляет клиентам и не меняет переписку в мессенджерах. ' : ''}
          Поля этого окна описаны в settings.json внутри архива.
        </p>
      </aside>
    </div>
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
