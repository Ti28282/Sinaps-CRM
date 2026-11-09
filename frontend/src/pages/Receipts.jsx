import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, on, errText, money, STAGES } from '../api.js'
import { Avatar, Icon } from '../components/ui.jsx'

// Страница плагина «Чеки»: новый чек из частей → статистика → история → по типам.
const PERIODS = [
  { id: 'quarter', label: 'Квартал' },
  { id: 'year', label: 'Год' },
  { id: 'all', label: 'Всё время' },
]
const STATUS = { paid: 'оплачен', wait: 'ожидает', refund: 'возврат' }
const PROJECT = 'project'
const stageTitle = (id) => STAGES.find((s) => s.id === id)?.title || ''
const short = (n) => (n >= 1_000_000 ? (n / 1_000_000).toLocaleString('ru-RU', { maximumFractionDigits: 1 }) + 'м' : n >= 1000 ? Math.round(n / 1000) + 'к' : String(n))
const isoDay = (unix) => {
  const d = unix ? new Date(unix * 1000) : new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
const digits = (v) => String(v ?? '').replace(/\D/g, '')
const spaced = (v) => (digits(v) ? Number(digits(v)).toLocaleString('ru-RU') : '')
const num = (v) => Number(digits(v) || 0)
const plural = (n, a, b, c) => (n % 10 === 1 && n % 100 !== 11 ? a : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? b : c)
const typeOf = (types, id) => types.find((t) => t.id === id) || { id, label: id.replace(/^c:/, ''), short: id.replace(/^c:/, ''), color: '#8A8F98', inProject: false }
const emptyLine = (kind = PROJECT) => ({ kind, what: '', amount: '' })
const toParts = (lines) => lines.map((l) => ({ kind: l.kind, what: l.what, amount: num(l.amount) }))

export default function Receipts({ toast, chats, openChat }) {
  const [chatId, setChatId] = useState(0)
  const [period, setPeriod] = useState('year')
  const [kind, setKind] = useState('')
  const [rep, setRep] = useState(null)
  const [busy, setBusy] = useState(false)
  const [edit, setEdit] = useState(null) // чек, который правим

  const load = useCallback(() => api.receipts(chatId, period, kind).then(setRep).catch((e) => toast(errText(e), 'err')), [chatId, period, kind, toast])
  useEffect(() => { load() }, [load])
  useEffect(() => on('receipts:changed', () => load()), [load])

  async function exportXlsx() {
    setBusy(true)
    try {
      const r = await api.exportReceipts(chatId, period, kind)
      if (r.path) toast(`Сохранено чеков: ${r.count} → ${r.path.split(/[\\/]/).pop()}`)
    } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }

  async function remove(r) {
    if (!window.confirm(`Удалить чек «${r.chatTitle}» на ${money(r.amount, r.currency)}?`)) return
    try { await api.deleteReceipt(r.id); toast('Чек удалён') } catch (e) { toast(errText(e), 'err') }
  }

  async function addType(label) {
    try { await api.addReceiptType(label); await load(); return true } catch (e) { toast(errText(e), 'err'); return false }
  }

  if (!rep) return <div className="page"><p className="hint">Загрузка…</p></div>
  const cur = rep.currency
  const types = rep.allTypes
  const max = Math.max(1, ...rep.months.map((m) => m.sum))
  const topMax = Math.max(1, ...rep.top.map((t) => t.sum))
  const client = rep.clients.find((c) => c.chatId === chatId)
  const kindT = kind ? typeOf(types, kind) : null
  const typesPaid = rep.types.reduce((a, t) => a + t.sum, 0)
  const filterNote = [client?.title, kindT?.label].filter(Boolean).join(' · ')

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <div className="row" style={{ gap: 10 }}>
            <h1>Чеки</h1>
            <span className="plugin-badge">плагин</span>
          </div>
          <p>Один чек от клиента можно разбить на части: что идёт в счёт проекта, а что — сверху</p>
        </div>
        <button type="button" className="btn" onClick={exportXlsx} disabled={busy}>{busy ? 'Сохраняем…' : 'Экспорт в Excel'}</button>
      </header>

      <NewReceipt chats={chats} types={types} cur={cur} toast={toast} addType={addType} />

      <div className="row rc-stats-head">
        <h2>Статистика</h2>
        {kindT && (
          <button type="button" className="rc-kind-chip" onClick={() => setKind('')} title="Снять фильтр по типу">
            <i style={{ background: kindT.color }} aria-hidden="true" />{kindT.label}<Icon name="close" size={14} />
          </button>
        )}
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
            <span className="hint">{filterNote || 'все клиенты'}, только оплаченные · 12 мес.</span>
          </div>
          <div className="bars" role="img" aria-label="Оплаты по месяцам">
            {rep.months.map((m, i) => (
              <div key={i} className="bar-col" title={`${m.label}: ${money(m.sum, cur)}`}>
                <span className="bar-val">{m.sum ? short(m.sum) : ''}</span>
                <div className={'bar' + (i === rep.months.length - 1 ? ' now' : '')} style={{ height: `${Math.max(m.sum ? 4 : 2, (m.sum / max) * 100)}%`, ...(kindT && m.sum ? { background: kindT.color } : {}) }} />
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
          <span className="hint">{filterNote ? 'фильтр: ' + filterNote : 'новые сверху; один чек — несколько частей'}</span>
          <span className="hint" style={{ marginLeft: 'auto', fontSize: 12 }}>
            {rep.rows.length} {plural(rep.rows.length, 'чек', 'чека', 'чеков')}{rep.rows.length > 7 ? ' · дальше прокрутка' : ''}
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
                <tr><th>Дата</th><th>Клиент</th><th>Из чего состоит</th><th>Статус</th><th className="num">Итого</th><th className="act"><span className="sr">Действия</span></th></tr>
              </thead>
              <tbody>
                {rep.rows.map((r) => (
                  <tr key={r.id}>
                    <td className="muted nowrap">{new Date(r.paidAt * 1000).toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit', year: '2-digit' })}</td>
                    <td className="nowrap">
                      <button type="button" className="link-btn" onClick={() => openChat(r.chatId)}>{r.chatTitle}</button>
                      <span className={'ch-mini ' + r.channel}>{r.channel === 'wa' ? 'WA' : 'TG'}</span>
                    </td>
                    <td>
                      <div className="rc-parts">
                        {r.parts.map((p, i) => {
                          const t = typeOf(types, p.kind)
                          return (
                            <span key={i} className={'rc-part' + (kind && p.kind !== kind ? ' dim' : '')} title={p.what || t.label}>
                              <i style={{ background: t.color }} aria-hidden="true" />{t.short} <b className="mono">{money(p.amount, '').trim()}</b>
                            </span>
                          )
                        })}
                      </div>
                    </td>
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

      <section className="panel" aria-label="Суммы по типам">
        <div className="row" style={{ gap: 12 }}>
          <h2>По типам</h2>
          <span className="hint">{client ? client.title : 'все клиенты'}, оплаченные за период · нажмите на тип, чтобы отфильтровать страницу</span>
        </div>
        <div className="type-bar" role="img" aria-label="Доли типов в оплатах">
          {rep.types.filter((t) => t.sum > 0).map((t) => (
            <span key={t.id} style={{ width: `${(t.sum / (typesPaid || 1)) * 100}%`, background: t.color, opacity: !kind || kind === t.id ? 1 : 0.25 }} />
          ))}
        </div>
        <div className="type-cards">
          {rep.types.map((t) => (
            <button key={t.id} type="button" className="type-card" aria-pressed={kind === t.id} onClick={() => setKind(kind === t.id ? '' : t.id)}>
              <span className="type-card-head"><i style={{ background: t.color }} aria-hidden="true" />{t.label}<span className="mono">{typesPaid ? Math.round((t.sum / typesPaid) * 100) : 0}%</span></span>
              <span className="type-card-sum mono">{money(t.sum, cur)}</span>
              <span className="hint" style={{ fontSize: 12 }}>{t.count} {plural(t.count, 'часть', 'части', 'частей')}{t.inProject ? ' · в счёт проекта' : ' · сверх проекта'}</span>
            </button>
          ))}
        </div>
      </section>

      {edit && <EditReceipt r={edit} types={types} toast={toast} addType={addType} close={() => setEdit(null)} />}
    </div>
  )
}

// Блок «Новый чек»: клиент и его проект слева, части чека справа.
function NewReceipt({ chats, types, cur, toast, addType }) {
  const [chatId, setChatId] = useState(0)
  const [lines, setLines] = useState([emptyLine()])
  const [date, setDate] = useState(isoDay())
  const [status, setStatus] = useState('paid')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState('')

  const inProj = (l) => typeOf(types, l.kind).inProject
  const total = lines.reduce((a, l) => a + num(l.amount), 0)
  const projNow = lines.filter(inProj).reduce((a, l) => a + num(l.amount), 0)
  const ok = !!chatId && (total > 0 || status === 'wait')

  async function create(e) {
    e.preventDefault()
    if (!ok) return
    setBusy(true)
    try {
      const r = await api.createReceipt({ chatId, date, status, parts: toParts(lines) })
      setDone(`Чек создан: ${r.chatTitle} · ${money(r.amount, r.currency)}`)
      setLines([emptyLine()]); setStatus('paid')
    } catch (err) { toast(errText(err), 'err') } finally { setBusy(false) }
  }

  return (
    <form className="panel rc-new" onSubmit={create} aria-label="Новый чек">
      <div className="row" style={{ gap: 10 }}>
        <span className="rc-new-ico"><Icon name="plus" size={17} /></span>
        <h2>Новый чек</h2>
        {done && <span className="rc-done" role="status">{done}</span>}
      </div>
      <div className="rc-new-body">
        <div className="rc-new-left">
          <div className="field">
            <span>Клиент из воронки</span>
            <ClientPicker chats={chats} value={chatId} onChange={(id) => { setChatId(id); setDone('') }} />
          </div>
          {chatId ? <ProjectCard chatId={chatId} cur={cur} projNow={status === 'paid' ? projNow : 0} extraNow={status === 'paid' ? total - projNow : 0} toast={toast} /> : (
            <p className="hint" style={{ fontSize: 13 }}>Выберите клиента — здесь появится его проект: стоимость, сколько оплачено и сколько осталось.</p>
          )}
        </div>
        <div className="rc-new-right">
          <div className="row" style={{ alignItems: 'baseline' }}>
            <span className="hint" style={{ fontSize: 12 }}>Из чего состоит чек</span>
            <span className="hint" style={{ fontSize: 12, marginLeft: 'auto' }}>часть «В счёт проекта» уменьшает остаток по проекту</span>
          </div>
          <PartsEditor lines={lines} setLines={(l) => { setLines(l); setDone('') }} types={types} cur={cur} addType={addType} />
          <div className="row" style={{ gap: 12 }}>
            <label className="row hint" style={{ gap: 8, fontSize: 13 }}>Дата
              <input className="input input-sm" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
            </label>
            <label className="row hint" style={{ gap: 8, fontSize: 13 }}>Статус
              <select className="input input-sm" value={status} onChange={(e) => setStatus(e.target.value)}>
                <option value="paid">Оплачен</option>
                <option value="wait">Ожидает</option>
                <option value="refund">Возврат</option>
              </select>
            </label>
          </div>
          <div className="rc-total">
            <span className="hint" style={{ fontSize: 13 }}>в счёт проекта {money(projNow, cur)} · сверх проекта {money(total - projNow, cur)}</span>
            <span className="hint" style={{ marginLeft: 'auto', fontSize: 13 }}>Итого чек</span>
            <span className="mono rc-total-sum">{money(total, cur)}</span>
            <button type="submit" className="btn btn-primary" disabled={!ok || busy}>{busy ? 'Создаём…' : 'Создать чек'}</button>
          </div>
        </div>
      </div>
    </form>
  )
}

// Строки чека: тип, комментарий, сумма. В выпадающем списке типов — «+ Свой тип…».
function PartsEditor({ lines, setLines, types, cur, addType }) {
  const [newFor, setNewFor] = useState(-1) // строка, где вводят свой тип
  const [newLabel, setNewLabel] = useState('')
  const set = (i, patch) => setLines(lines.map((l, j) => (j === i ? { ...l, ...patch } : l)))

  async function saveType(i) {
    const label = newLabel.trim()
    if (!label) return setNewFor(-1)
    if (await addType(label)) set(i, { kind: 'c:' + label })
    setNewFor(-1); setNewLabel('')
  }

  return (
    <div className="parts">
      {lines.map((l, i) => {
        const t = typeOf(types, l.kind)
        return (
          <div key={i} className="part-row">
            {newFor === i ? (
              <span className="part-type part-new">
                <input className="input" autoFocus placeholder="Новый тип, например «Консультация»" value={newLabel}
                  onChange={(e) => setNewLabel(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); saveType(i) } if (e.key === 'Escape') setNewFor(-1) }} />
                <button type="button" className="btn btn-sm" onClick={() => saveType(i)}>Добавить</button>
              </span>
            ) : (
              <label className="part-type">
                <span className="sr">Тип части</span>
                <select className="input" style={{ borderColor: t.color }} value={l.kind}
                  onChange={(e) => (e.target.value === '__new' ? (setNewFor(i), setNewLabel('')) : set(i, { kind: e.target.value }))}>
                  {types.map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}
                  <option value="__new">+ Свой тип…</option>
                </select>
              </label>
            )}
            <label className="part-what">
              <span className="sr">Комментарий</span>
              <input className="input" placeholder="Комментарий" value={l.what} onChange={(e) => set(i, { what: e.target.value })} />
            </label>
            <label className="part-amount">
              <span className="sr">Сумма части, {cur}</span>
              <input className="input mono" inputMode="numeric" placeholder="0" value={spaced(l.amount)} onChange={(e) => set(i, { amount: digits(e.target.value) })} />
            </label>
            <span className={'part-tag' + (t.inProject ? ' in' : '')}>{t.inProject ? 'входит в проект' : 'сверх проекта'}</span>
            <button type="button" className="msg-act" aria-label="Убрать часть" title="Убрать часть" disabled={lines.length === 1}
              onClick={() => setLines(lines.filter((_, j) => j !== i))}><Icon name="close" size={14} /></button>
          </div>
        )
      })}
      <button type="button" className="btn btn-sm btn-dashed" onClick={() => setLines([...lines, emptyLine(lines.some((l) => l.kind === PROJECT) ? 'edits' : PROJECT)])}>
        <Icon name="plus" size={14} /> Добавить часть
      </button>
    </div>
  )
}

// Проект клиента: стоимость, оплачено, этим чеком, осталось, сверх проекта.
function ProjectCard({ chatId, cur, projNow, extraNow, toast }) {
  const [p, setP] = useState(null)
  const [edit, setEdit] = useState(false)

  const load = useCallback(() => api.chekiProject(chatId).then(setP).catch((e) => toast(errText(e), 'err')), [chatId, toast])
  useEffect(() => { setP(null); load() }, [load])
  useEffect(() => on('receipts:changed', (id) => id === chatId && load()), [chatId, load])

  if (!p) return <div className="proj-card"><span className="hint">Загрузка…</span></div>
  const price = p.price || 0
  const paidW = price ? Math.min(100, (p.paid / price) * 100) : 0
  const nowW = price ? Math.min(100 - paidW, (projNow / price) * 100) : 0
  const left = Math.max(0, price - p.paid - projNow)

  return (
    <div className="proj-card">
      <div className="row" style={{ gap: 8 }}>
        <span className="eyebrow">Проект клиента</span>
        <button type="button" className="btn btn-sm btn-quiet" style={{ marginLeft: 'auto' }} onClick={() => setEdit(true)}>{price ? 'Изменить' : 'Задать проект'}</button>
      </div>
      {price ? (
        <>
          <div className="row" style={{ alignItems: 'baseline', flexWrap: 'nowrap' }}>
            <span className="proj-name">{p.name || 'Проект'}</span>
            <span className="mono proj-price">{money(price, cur)}</span>
          </div>
          <div className="proj-bar" role="img" aria-label={`Проект оплачен на ${Math.round(((p.paid + projNow) / price) * 100)}% с учётом этого чека`}>
            <span className="paid" style={{ width: paidW + '%' }} />
            <span className="now" style={{ left: paidW + '%', width: nowW + '%' }} />
          </div>
          <div className="proj-nums">
            <span>Оплачено <b className="mono">{money(p.paid, cur)}</b></span>
            {projNow > 0 && <span>этим чеком <b className="mono accent">+{money(projNow, cur)}</b></span>}
            <span style={{ marginLeft: 'auto' }}>осталось <b className="mono">{money(left, cur)}</b></span>
          </div>
        </>
      ) : (
        <p className="hint" style={{ fontSize: 13 }}>Стоимость проекта не задана. Задайте её — и будет видно, сколько осталось оплатить.</p>
      )}
      <div className="hint" style={{ fontSize: 12 }}>
        Сверх проекта у клиента: <span className="mono warn-text">{money(p.extra + extraNow, cur)}</span> — правки, поддержка и т.п. в стоимость проекта не входят
      </div>
      {edit && <ProjectDialog p={p} cur={cur} toast={toast} close={() => setEdit(false)} onSaved={setP} />}
    </div>
  )
}

function ProjectDialog({ p, cur, toast, close, onSaved }) {
  const [name, setName] = useState(p.name || '')
  const [price, setPrice] = useState(p.price ? String(p.price) : '')
  const [busy, setBusy] = useState(false)
  async function save(e) {
    e.preventDefault()
    setBusy(true)
    try { onSaved(await api.setChekiProject(p.chatId, name, num(price))); toast('Проект сохранён'); close() } catch (err) { toast(errText(err), 'err') } finally { setBusy(false) }
  }
  return (
    <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <form className="modal modal-sm" role="dialog" aria-modal="true" aria-labelledby="pj-title" onSubmit={save}>
        <div className="modal-head">
          <h2 id="pj-title">Проект клиента</h2>
          <button type="button" className="icon-btn" aria-label="Закрыть" onClick={close}><Icon name="close" /></button>
        </div>
        <label className="field">
          <span>Название</span>
          <input className="input" autoFocus placeholder="Бот записи + 1С" value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="field">
          <span>Полная стоимость, {cur}</span>
          <input className="input mono" style={{ fontSize: 16, fontWeight: 600 }} inputMode="numeric" placeholder="67 000" value={spaced(price)} onChange={(e) => setPrice(digits(e.target.value))} />
        </label>
        <p className="hint" style={{ fontSize: 13 }}>В оплату проекта идут только части чеков с типом «В счёт проекта». Правки и другое — сверх проекта.</p>
        <div className="row" style={{ justifyContent: 'flex-end' }}>
          <button type="button" className="btn" onClick={close}>Отмена</button>
          <button type="submit" className="btn btn-primary" disabled={busy}>{busy ? 'Сохраняем…' : 'Сохранить'}</button>
        </div>
      </form>
    </div>
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

function EditReceipt({ r, types, toast, addType, close }) {
  const [lines, setLines] = useState(r.parts.length ? r.parts.map((p) => ({ kind: p.kind, what: p.what, amount: String(p.amount) })) : [emptyLine()])
  const [date, setDate] = useState(isoDay(r.paidAt))
  const [status, setStatus] = useState(r.status)
  const [busy, setBusy] = useState(false)
  const total = lines.reduce((a, l) => a + num(l.amount), 0)

  async function save(e) {
    e.preventDefault()
    setBusy(true)
    try {
      await api.updateReceipt(r.id, { chatId: r.chatId, date, status, parts: toParts(lines) })
      toast('Чек изменён'); close()
    } catch (err) { toast(errText(err), 'err') } finally { setBusy(false) }
  }

  return (
    <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <form className="modal" style={{ width: 'min(720px, 100%)' }} role="dialog" aria-modal="true" aria-labelledby="er-title" onSubmit={save}>
        <div className="modal-head">
          <div>
            <h2 id="er-title">Изменить чек</h2>
            <p className="hint" style={{ marginTop: 4 }}>{r.chatTitle}</p>
          </div>
          <button type="button" className="icon-btn" aria-label="Закрыть" onClick={close}><Icon name="close" /></button>
        </div>
        <PartsEditor lines={lines} setLines={setLines} types={types} cur={r.currency} addType={addType} />
        <div className="row" style={{ gap: 12 }}>
          <label className="row hint" style={{ gap: 8, fontSize: 13 }}>Дата
            <input className="input input-sm" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
          </label>
          <label className="row hint" style={{ gap: 8, fontSize: 13 }}>Статус
            <select className="input input-sm" value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="paid">Оплачен</option>
              <option value="wait">Ожидает</option>
              <option value="refund">Возврат</option>
            </select>
          </label>
          <span className="mono rc-total-sum" style={{ marginLeft: 'auto' }}>{money(total, r.currency)}</span>
        </div>
        <div className="row" style={{ justifyContent: 'flex-end' }}>
          <button type="button" className="btn" onClick={close}>Отмена</button>
          <button type="submit" className="btn btn-primary" disabled={busy}>{busy ? 'Сохраняем…' : 'Сохранить'}</button>
        </div>
      </form>
    </div>
  )
}
