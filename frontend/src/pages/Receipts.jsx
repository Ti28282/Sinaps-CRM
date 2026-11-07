import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, on, errText, money, STAGES } from '../api.js'
import { Avatar, Icon } from '../components/ui.jsx'

// Страница плагина «Чеки»: новый чек → статистика → история.
const PERIODS = [
  { id: 'quarter', label: 'Квартал' },
  { id: 'year', label: 'Год' },
  { id: 'all', label: 'Всё время' },
]
const STATUS = { paid: 'оплачен', wait: 'ожидает', refund: 'возврат' }
const stageTitle = (id) => STAGES.find((s) => s.id === id)?.title || ''
const short = (n) => (n >= 1_000_000 ? (n / 1_000_000).toLocaleString('ru-RU', { maximumFractionDigits: 1 }) + 'м' : n >= 1000 ? Math.round(n / 1000) + 'к' : String(n))
const isoDay = (unix) => {
  const d = unix ? new Date(unix * 1000) : new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
const digits = (v) => String(v).replace(/\D/g, '')
const spaced = (v) => (digits(v) ? Number(digits(v)).toLocaleString('ru-RU') : '')

export default function Receipts({ toast, chats, openChat }) {
  const [chatId, setChatId] = useState(0)
  const [period, setPeriod] = useState('year')
  const [rep, setRep] = useState(null)
  const [busy, setBusy] = useState(false)
  const [edit, setEdit] = useState(null) // чек, который правим

  const load = useCallback(() => api.receipts(chatId, period).then(setRep).catch((e) => toast(errText(e), 'err')), [chatId, period, toast])
  useEffect(() => { load() }, [load])
  useEffect(() => on('receipts:changed', () => load()), [load])

  async function exportXlsx() {
    setBusy(true)
    try {
      const r = await api.exportReceipts(chatId, period)
      if (r.path) toast(`Сохранено чеков: ${r.count} → ${r.path.split(/[\\/]/).pop()}`)
    } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }

  async function remove(r) {
    if (!window.confirm(`Удалить чек «${r.chatTitle}» на ${money(r.amount, r.currency)}?`)) return
    try { await api.deleteReceipt(r.id); toast('Чек удалён') } catch (e) { toast(errText(e), 'err') }
  }

  if (!rep) return <div className="page"><p className="hint">Загрузка…</p></div>
  const cur = rep.currency
  const max = Math.max(1, ...rep.months.map((m) => m.sum))
  const topMax = Math.max(1, ...rep.top.map((t) => t.sum))
  const client = rep.clients.find((c) => c.chatId === chatId)

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <div className="row" style={{ gap: 10 }}>
            <h1>Чеки</h1>
            <span className="plugin-badge">плагин</span>
          </div>
          <p>Выберите клиента из воронки, укажите сумму и за что — чек попадёт в статистику и историю</p>
        </div>
        <button type="button" className="btn" onClick={exportXlsx} disabled={busy}>{busy ? 'Сохраняем…' : 'Экспорт в Excel'}</button>
      </header>

      <NewReceipt chats={chats} cur={cur} toast={toast} />

      <div className="row rc-stats-head">
        <h2>Статистика</h2>
        <label className="select-pill" style={{ marginLeft: 'auto' }}>
          <span>Клиент</span>
          <select value={chatId} onChange={(e) => setChatId(Number(e.target.value))}>
            <option value={0}>Все из воронки</option>
            {rep.clients.map((c) => <option key={c.chatId} value={c.chatId}>{c.title}</option>)}
          </select>
        </label>
        <div className="seg" role="group" aria-label="Период">
          {PERIODS.map((p) => (
            <button key={p.id} type="button" aria-pressed={period === p.id} onClick={() => setPeriod(p.id)}>{p.id === 'year' ? rep.year : p.label}</button>
          ))}
        </div>
      </div>

      <div className="stats">
        <div className="stat"><div className="stat-label">Получено</div><div className="stat-value accent">{money(rep.total, cur)}</div></div>
        <div className="stat"><div className="stat-label">Чеков</div><div className="stat-value">{rep.count}</div></div>
        <div className="stat"><div className="stat-label">Средний чек</div><div className="stat-value">{rep.avg ? money(rep.avg, cur) : '—'}</div></div>
        <div className="stat"><div className="stat-label">Ожидают оплаты</div><div className="stat-value warn">{money(rep.wait, cur)}</div></div>
      </div>

      <div className="rc-grid">
        <section className="panel rc-chart">
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <h2>По месяцам</h2>
            <span className="hint">{client ? client.title : 'все клиенты'}, только оплаченные · 12 мес.</span>
          </div>
          <div className="bars" role="img" aria-label="Оплаты по месяцам">
            {rep.months.map((m, i) => (
              <div key={i} className="bar-col" title={`${m.label}: ${money(m.sum, cur)}`}>
                <span className="bar-val">{m.sum ? short(m.sum) : ''}</span>
                <div className={'bar' + (i === rep.months.length - 1 ? ' now' : '')} style={{ height: `${Math.max(m.sum ? 4 : 2, (m.sum / max) * 100)}%` }} />
                <span className="bar-lbl">{m.label}</span>
              </div>
            ))}
          </div>
        </section>

        <section className="panel rc-top">
          <h2>Топ клиентов</h2>
          {rep.top.length === 0 && <p className="hint">За период оплат нет</p>}
          {rep.top.map((t) => (
            <button key={t.chatId} type="button" className="top-row" aria-pressed={chatId === t.chatId} onClick={() => setChatId(chatId === t.chatId ? 0 : t.chatId)}>
              <span className="row" style={{ justifyContent: 'space-between', flexWrap: 'nowrap' }}>
                <span className="top-name">{t.title} <span className={'ch-mini ' + t.channel}>{t.channel === 'wa' ? 'WA' : 'TG'}</span></span>
                <span className="mono">{money(t.sum, cur)}</span>
              </span>
              <span className="top-bar"><i style={{ width: `${(t.sum / topMax) * 100}%` }} /></span>
            </button>
          ))}
        </section>
      </div>

      <section className="panel" style={{ padding: 0, gap: 0, overflow: 'hidden' }} aria-label="История чеков">
        <div className="row rc-table-head">
          <h2>История</h2>
          <span className="hint">{chatId ? 'фильтр: ' + (client?.title || '') : 'все чеки за период, новые сверху'}</span>
          <span className="hint" style={{ marginLeft: 'auto', fontSize: 12 }}>
            {rep.rows.length > 7 ? `${rep.rows.length} записей · видно 7, остальные — прокруткой` : `${rep.rows.length} записей`}
          </span>
        </div>
        {rep.rows.length === 0 ? (
          <div className="empty" style={{ padding: 32 }}>
            <div><Icon name="receipt" size={30} /><h3>Чеков пока нет</h3><p>Создайте первый чек в блоке «Новый чек» выше.</p></div>
          </div>
        ) : (
          <div className="rc-scroll">
            <table className="rc-table">
              <thead>
                <tr><th>Дата</th><th>Клиент</th><th>За что</th><th>Статус</th><th className="num">Сумма</th><th className="act"><span className="sr">Действия</span></th></tr>
              </thead>
              <tbody>
                {rep.rows.map((r) => (
                  <tr key={r.id}>
                    <td className="muted nowrap">{new Date(r.paidAt * 1000).toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit', year: '2-digit' })}</td>
                    <td className="nowrap">
                      <button type="button" className="link-btn" onClick={() => openChat(r.chatId)}>{r.chatTitle}</button>
                      <span className={'ch-mini ' + r.channel}>{r.channel === 'wa' ? 'WA' : 'TG'}</span>
                    </td>
                    <td className="muted">{r.what || r.fileName || '—'}</td>
                    <td><span className={'rc-st st-' + r.status}>{STATUS[r.status]}</span></td>
                    <td className="num mono nowrap">{money(r.amount, r.currency)}</td>
                    <td className="act nowrap">
                      <button type="button" className="msg-act" aria-label="Изменить чек" title="Изменить" onClick={() => setEdit(r)}><Icon name="edit" size={14} /></button>
                      <button type="button" className="msg-act" aria-label="Удалить чек" title="Удалить" onClick={() => remove(r)}><Icon name="trash" size={14} /></button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {edit && <EditReceipt r={edit} toast={toast} close={() => setEdit(null)} />}
    </div>
  )
}

// Блок «Новый чек»: клиент из воронки, сумма, за что, дата, статус.
function NewReceipt({ chats, cur, toast }) {
  const [chatId, setChatId] = useState(0)
  const [amount, setAmount] = useState('')
  const [what, setWhat] = useState('')
  const [date, setDate] = useState(isoDay())
  const [status, setStatus] = useState('paid')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState('')

  const ok = !!chatId && (Number(digits(amount)) > 0 || status === 'wait')

  async function create(e) {
    e.preventDefault()
    if (!ok) return
    setBusy(true)
    try {
      const r = await api.createReceipt({ chatId, amount: Number(digits(amount) || 0), what, date, status })
      setDone(`Чек создан: ${r.chatTitle} · ${money(r.amount, r.currency)}`)
      setChatId(0); setAmount(''); setWhat(''); setStatus('paid')
    } catch (err) { toast(errText(err), 'err') } finally { setBusy(false) }
  }

  return (
    <form className="panel rc-new" onSubmit={create} aria-label="Новый чек">
      <div className="row" style={{ gap: 10 }}>
        <span className="rc-new-ico"><Icon name="plus" size={17} /></span>
        <h2>Новый чек</h2>
        {done && <span className="rc-done" role="status">{done}</span>}
      </div>
      <div className="rc-form">
        <div className="field rc-f-client">
          <span>Клиент из воронки</span>
          <ClientPicker chats={chats} value={chatId} onChange={(id) => { setChatId(id); setDone('') }} />
        </div>
        <label className="field rc-f-amount">
          <span>Сумма, {cur}</span>
          <input className="input mono" inputMode="numeric" placeholder="25 000" value={spaced(amount)} onChange={(e) => setAmount(digits(e.target.value))} />
        </label>
        <label className="field rc-f-what">
          <span>За что</span>
          <input className="input" placeholder="Бот записи · этап 2" value={what} onChange={(e) => setWhat(e.target.value)} />
        </label>
        <label className="field rc-f-date">
          <span>Дата оплаты</span>
          <input className="input" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
        </label>
        <label className="field rc-f-status">
          <span>Статус</span>
          <select className="input" value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="paid">Оплачен</option>
            <option value="wait">Ожидает</option>
            <option value="refund">Возврат</option>
          </select>
        </label>
        <button type="submit" className="btn btn-primary rc-create" disabled={!ok || busy}>{busy ? 'Создаём…' : 'Создать чек'}</button>
      </div>
      <p className="hint" style={{ fontSize: 12 }}>Если включено в настройках плагина: сумма чеков станет «Суммой сделки» клиента, первый оплаченный чек переведёт его в «Сделку».</p>
    </form>
  )
}

// Выбор клиента: кнопка + выпадающий список с поиском.
function ClientPicker({ chats, value, onChange }) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const box = useRef(null)
  const sel = chats.find((c) => c.id === value)
  const list = useMemo(() => {
    const s = q.trim().toLowerCase()
    return chats.filter((c) => !s || (c.title + ' ' + (c.username || '')).toLowerCase().includes(s)).slice(0, 200)
  }, [chats, q])

  useEffect(() => {
    if (!open) return
    const off = (e) => { if (box.current && !box.current.contains(e.target)) setOpen(false) }
    const esc = (e) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', off)
    document.addEventListener('keydown', esc)
    return () => { document.removeEventListener('mousedown', off); document.removeEventListener('keydown', esc) }
  }, [open])

  return (
    <div className="picker" ref={box}>
      <button type="button" className={'input picker-btn' + (open ? ' open' : '')} aria-haspopup="listbox" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {sel ? <><Avatar chat={sel} size={26} /><span className="picker-val">{sel.title}</span></> : <span className="picker-val muted">Выберите чат</span>}
        <Icon name="chevron" size={16} />
      </button>
      {open && (
        <div className="picker-pop">
          <label className="search" style={{ width: '100%' }}>
            <Icon name="search" size={15} />
            <span className="sr">Поиск клиента</span>
            <input autoFocus type="search" placeholder="Поиск по имени" value={q} onChange={(e) => setQ(e.target.value)} />
          </label>
          <div className="picker-list" role="listbox" aria-label="Клиенты">
            {list.map((c) => (
              <button key={c.id} type="button" role="option" aria-selected={c.id === value} className="picker-opt"
                onClick={() => { onChange(c.id); setOpen(false); setQ('') }}>
                <Avatar chat={c} size={30} />
                <span className="picker-name">{c.title} <span className={'ch-mini ' + c.channel}>{c.channel === 'wa' ? 'WA' : c.channel === 'tg' ? 'TG' : ''}</span></span>
                <span className="picker-stage">{stageTitle(c.stage)}</span>
              </button>
            ))}
            {list.length === 0 && <div className="hint" style={{ padding: 12 }}>Никого не нашли</div>}
          </div>
        </div>
      )}
    </div>
  )
}

function EditReceipt({ r, toast, close }) {
  const [amount, setAmount] = useState(String(r.amount || ''))
  const [what, setWhat] = useState(r.what || '')
  const [date, setDate] = useState(isoDay(r.paidAt))
  const [status, setStatus] = useState(r.status)
  const [busy, setBusy] = useState(false)

  async function save(e) {
    e.preventDefault()
    setBusy(true)
    try {
      await api.updateReceipt(r.id, { chatId: r.chatId, amount: Number(digits(amount) || 0), what, date, status })
      toast('Чек изменён'); close()
    } catch (err) { toast(errText(err), 'err') } finally { setBusy(false) }
  }

  return (
    <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <form className="modal modal-sm" role="dialog" aria-modal="true" aria-labelledby="er-title" onSubmit={save}>
        <div className="modal-head">
          <div>
            <h2 id="er-title">Изменить чек</h2>
            <p className="hint" style={{ marginTop: 4 }}>{r.chatTitle}</p>
          </div>
          <button type="button" className="icon-btn" aria-label="Закрыть" onClick={close}><Icon name="close" /></button>
        </div>
        <label className="field">
          <span>Сумма, {r.currency}</span>
          <input className="input mono" style={{ fontSize: 16, fontWeight: 600 }} inputMode="numeric" autoFocus value={spaced(amount)} onChange={(e) => setAmount(digits(e.target.value))} />
        </label>
        <label className="field">
          <span>За что</span>
          <input className="input" value={what} onChange={(e) => setWhat(e.target.value)} />
        </label>
        <div className="row" style={{ flexWrap: 'nowrap' }}>
          <label className="field" style={{ flex: 1 }}>
            <span>Дата оплаты</span>
            <input className="input" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
          </label>
          <label className="field" style={{ flex: 1 }}>
            <span>Статус</span>
            <select className="input" value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="paid">Оплачен</option>
              <option value="wait">Ожидает</option>
              <option value="refund">Возврат</option>
            </select>
          </label>
        </div>
        <div className="row" style={{ justifyContent: 'flex-end' }}>
          <button type="button" className="btn" onClick={close}>Отмена</button>
          <button type="submit" className="btn btn-primary" disabled={busy}>{busy ? 'Сохраняем…' : 'Сохранить'}</button>
        </div>
      </form>
    </div>
  )
}
