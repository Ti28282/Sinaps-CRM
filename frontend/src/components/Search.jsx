import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, errText, money, when, clock, STAGES, pluginOn } from '../api.js'
import { Avatar, Icon } from './ui.jsx'

// Поиск по всему (Ctrl+K): клиенты, переписка, чеки, файлы, история, разделы CRM.
// Ищет только в базе CRM на этом компьютере.

const norm = (s) => String(s || '').toLowerCase().replace(/ё/g, 'е')
const words = (q) => norm(q).split(/[^\p{L}\p{N}]+/u).filter(Boolean)
const stageTitle = (id) => STAGES.find((s) => s.id === id)?.title || ''
const chName = (ch) => (ch === 'wa' ? 'WhatsApp' : ch === 'tg' ? 'Telegram' : ch === 'manual' ? 'Из файла' : '')
const kb = (n) => (!n ? '' : n >= 1 << 20 ? (n / (1 << 20)).toLocaleString('ru-RU', { maximumFractionDigits: 1 }) + ' МБ' : Math.max(1, Math.round(n / 1024)) + ' КБ')
const day = (unix) => new Date(unix * 1000).toLocaleDateString('ru-RU', { day: 'numeric', month: 'short', year: 'numeric' })
const RECEIPT_ST = { paid: 'оплачен', wait: 'ожидает', refund: 'возврат' }

const KINDS = [
  { id: 'all', label: 'Всё' },
  { id: 'client', label: 'Клиенты' },
  { id: 'msg', label: 'Сообщения' },
  { id: 'receipt', label: 'Чеки', plugin: 'cheki' },
  { id: 'file', label: 'Файлы' },
  { id: 'event', label: 'История и заметки' },
  { id: 'nav', label: 'Разделы' },
]

// разделы CRM, в которые можно перейти из поиска
const PAGES = [
  { page: 'pipeline', title: 'Воронка', text: 'Клиенты по этапам' },
  { page: 'chats', title: 'Диалоги', text: 'Переписка с клиентами' },
  { page: 'broadcast', title: 'Рассылки', text: 'Шаблон и отправка по воронке' },
  { page: 'connect', title: 'Подключения', text: 'Telegram, WhatsApp, Gmail' },
  { page: 'settings', tab: 'appearance', title: 'Настройки → Внешний вид', text: 'Тёмная и светлая тема' },
  { page: 'settings', tab: 'sync', title: 'Настройки → Синхронизация', text: 'Папка Telegram, подтягивание сообщений' },
  { page: 'settings', tab: 'data', title: 'Настройки → Данные', text: 'Экспорт, импорт, резервная копия' },
  { page: 'settings', tab: 'plugins', title: 'Настройки → Плагины', text: 'Папка плагинов, включить и выключить' },
  { page: 'cheki', plugin: 'cheki', title: 'Чеки', text: 'Плагин · новый чек, статистика, история' },
  { page: 'notify', plugin: 'notify', title: 'Уведомления на телефон', text: 'Плагин · привязка телефона' },
]

// Подсветка совпадений с началом слов, как ищет CRM.
function Hl({ text, ws }) {
  const s = String(text || '')
  if (!ws.length || !s) return s
  const low = norm(s)
  const marks = new Array(s.length).fill(false)
  for (const w of ws) {
    let i = low.indexOf(w)
    while (i >= 0) {
      const start = i === 0 || !/[\p{L}\p{N}]/u.test(low[i - 1])
      if (start) for (let k = i; k < i + w.length; k++) marks[k] = true
      i = low.indexOf(w, i + 1)
    }
  }
  const out = []
  let k = 0
  while (k < s.length) {
    let j = k
    while (j < s.length && marks[j] === marks[k]) j++
    out.push(marks[k] ? <mark key={k}>{s.slice(k, j)}</mark> : <Fragment key={k}>{s.slice(k, j)}</Fragment>)
    k = j
  }
  return out
}

const recentKey = 'sinaps-search-recent'
function loadRecent() { try { return JSON.parse(localStorage.getItem(recentKey) || '[]').slice(0, 5) } catch { return [] } }
function saveRecent(q) {
  const t = q.trim()
  if (t.length < 2) return
  try { localStorage.setItem(recentKey, JSON.stringify([t, ...loadRecent().filter((x) => x !== t)].slice(0, 5))) } catch { /* без истории */ }
}

export default function Search({ close, chats, plugins, toast, openChat, openReceipt, openPage }) {
  const [q, setQ] = useState(() => { try { return sessionStorage.getItem('sinaps-search-q') || '' } catch { return '' } })
  const [res, setRes] = useState(null)
  const [busy, setBusy] = useState(false)
  const [kind, setKind] = useState('all')
  const [selKey, setSelKey] = useState('')
  const [ctx, setCtx] = useState({}) // соседние сообщения для предпросмотра: key → []
  const [types, setTypes] = useState([])
  const input = useRef(null)
  const listRef = useRef(null)
  const seq = useRef(0)
  const cheki = pluginOn(plugins, 'cheki')
  const ws = useMemo(() => words(q), [q])

  useEffect(() => { input.current?.focus(); input.current?.select() }, [])
  useEffect(() => { if (cheki) api.receiptTypes().then(setTypes).catch(() => {}) }, [cheki])
  useEffect(() => { try { sessionStorage.setItem('sinaps-search-q', q) } catch { /* ок */ } }, [q])

  // запрос с задержкой: не дёргаем базу на каждую букву
  useEffect(() => {
    const my = ++seq.current
    if (!ws.length) { setRes(null); setBusy(false); return }
    setBusy(true)
    const t = setTimeout(() => {
      api.search(q).then((r) => { if (my === seq.current) { setRes(r); setBusy(false); setSelKey('') } })
        .catch((e) => { if (my === seq.current) { setBusy(false); toast(errText(e), 'err') } })
    }, 160)
    return () => clearTimeout(t)
  }, [q]) // eslint-disable-line react-hooks/exhaustive-deps

  const chatById = useMemo(() => new Map(chats.map((c) => [c.id, c])), [chats])
  const typeLabel = (id) => types.find((t) => t.id === id)?.label || id.replace(/^c:/, '')

  // всё найденное — в один формат строк
  const groups = useMemo(() => {
    if (!ws.length) return []
    const r = res || { clients: [], messages: [], receipts: [], files: [], events: [], counts: {} }
    const pages = PAGES.filter((p) => (!p.plugin || pluginOn(plugins, p.plugin)) && (norm(p.title).includes(ws.join(' ')) || ws.every((w) => words(p.title + ' ' + p.text).some((t) => t.startsWith(w)))))
    const g = {
      client: r.clients.map((c) => ({ key: 'c' + c.id, kind: 'client', chat: c, title: c.title,
        sub: [chName(c.channel), c.username ? '@' + c.username.replace(/^@/, '') : c.phone, c.amount ? money(c.amount) : ''].filter(Boolean).join(' · '),
        right: when(c.lastAt), tag: stageTitle(c.stage) })),
      msg: r.messages.map((m) => ({ key: `m${m.chatId}:${m.msgId}`, kind: 'msg', m, title: m.chatTitle, sub: (m.out ? 'Вы: ' : '') + (m.snippet || m.text), right: when(m.date), deleted: m.deleted })),
      receipt: r.receipts.map((x) => ({ key: 'r' + x.id, kind: 'receipt', r: x, title: `Чек · ${money(x.amount, x.currency)} · ${x.chatTitle}`,
        sub: [day(x.paidAt), x.parts.map((p) => `${typeLabel(p.kind)} ${money(p.amount, '').trim()}`).join(' + '), x.fileName].filter(Boolean).join(' · '),
        right: new Date(x.paidAt * 1000).toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit' }), tag: RECEIPT_ST[x.status], tagKind: x.status })),
      file: r.files.map((f) => ({ key: `f${f.chatId}:${f.msgId}`, kind: 'file', f, title: f.fileName, sub: `${f.chatTitle} · ${f.out ? 'отправили вы' : 'от клиента'} · ${day(f.date)}`, right: kb(f.fileSize) })),
      event: r.events.map((e, i) => ({ key: `e${e.chatId}:${e.at}:${i}`, kind: 'event', e, title: e.text, sub: e.chatTitle, right: when(e.at) })),
      nav: pages.map((p) => ({ key: 'n' + p.page + (p.tab || ''), kind: 'nav', p, title: p.title, sub: p.text, right: '' })),
    }
    const counts = { ...r.counts, nav: pages.length }
    return KINDS.slice(1)
      .filter((k) => (!k.plugin || pluginOn(plugins, k.plugin)) && (kind === 'all' || kind === k.id) && g[k.id].length)
      .map((k) => ({ id: k.id, title: k.label, total: counts[k.id] || g[k.id].length, items: kind === 'all' ? g[k.id].slice(0, 3) : g[k.id] }))
  }, [res, ws, kind, plugins, types]) // eslint-disable-line react-hooks/exhaustive-deps

  const counts = useMemo(() => {
    const c = { ...(res?.counts || {}) }
    c.nav = ws.length ? PAGES.filter((p) => (!p.plugin || pluginOn(plugins, p.plugin)) && (norm(p.title).includes(ws.join(' ')) || ws.every((w) => words(p.title + ' ' + p.text).some((t) => t.startsWith(w))))).length : 0
    c.all = KINDS.slice(1).reduce((a, k) => a + (!k.plugin || pluginOn(plugins, k.plugin) ? c[k.id] || 0 : 0), 0)
    return c
  }, [res, ws, plugins])

  const flat = useMemo(() => groups.flatMap((g) => g.items), [groups])
  const sel = flat.find((x) => x.key === selKey) || flat[0] || null

  // предпросмотр сообщения: соседние реплики
  useEffect(() => {
    if (sel?.kind !== 'msg' || ctx[sel.key]) return
    api.searchContext(sel.m.chatId, sel.m.msgId).then((l) => setCtx((c) => ({ ...c, [sel.key]: l }))).catch(() => {})
  }, [sel?.key]) // eslint-disable-line react-hooks/exhaustive-deps

  // выбранная строка всегда видна в списке
  useEffect(() => {
    if (!sel) return
    listRef.current?.querySelector(`[data-key="${CSS.escape(sel.key)}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [sel?.key])

  const go = useCallback((x, alt) => {
    if (!x) return
    saveRecent(q)
    close()
    if (x.kind === 'client' || x.kind === 'event') return openChat(x.kind === 'client' ? x.chat.id : x.e.chatId)
    if (x.kind === 'msg') return openChat(x.m.chatId, x.m.msgId)
    if (x.kind === 'receipt') return openReceipt(x.r, alt === 'file')
    if (x.kind === 'file') {
      if (alt === 'chat') return openChat(x.f.chatId, x.f.msgId)
      return api.openAttachment(x.f.chatId, x.f.msgId).catch((e) => toast(errText(e), 'err'))
    }
    if (x.kind === 'nav') return openPage(x.p.page, x.p.tab)
  }, [q, close, openChat, openReceipt, openPage, toast])

  function onKey(e) {
    if (e.key === 'Escape') { e.preventDefault(); close(); return }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (!flat.length) return
      const i = Math.max(0, flat.findIndex((x) => x.key === sel?.key))
      setSelKey(flat[(i + (e.key === 'ArrowDown' ? 1 : -1) + flat.length) % flat.length].key)
      return
    }
    if (e.key === 'Enter') { e.preventDefault(); go(sel); return }
    if (e.key === 'Tab') {
      e.preventDefault()
      const ks = KINDS.filter((k) => !k.plugin || pluginOn(plugins, k.plugin)).map((k) => k.id)
      const i = ks.indexOf(kind)
      setKind(ks[(i + (e.shiftKey ? -1 : 1) + ks.length) % ks.length]); setSelKey('')
    }
  }

  const recent = !ws.length ? loadRecent() : []
  const nothing = ws.length > 0 && res && !busy && flat.length === 0

  return (
    <div className="modal-back search-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className="search-box" role="dialog" aria-modal="true" aria-label="Поиск по всему" onKeyDown={onKey}>
        <div className="search-top">
          <Icon name="search" size={20} className="search-top-ico" />
          <input ref={input} value={q} onChange={(e) => { setQ(e.target.value); setKind('all') }} aria-label="Что ищем"
            placeholder="Клиент, текст сообщения, сумма чека, файл, @username или телефон" />
          {busy && <span className="hint" style={{ fontSize: 12 }}>Ищем…</span>}
          {q && <button type="button" className="btn btn-sm btn-quiet" onClick={() => { setQ(''); input.current?.focus() }}>Очистить</button>}
          <kbd>Esc</kbd>
        </div>

        <div className="search-chips" role="tablist" aria-label="Где искать">
          {KINDS.filter((k) => !k.plugin || pluginOn(plugins, k.plugin)).map((k) => (
            <button key={k.id} type="button" role="tab" aria-selected={kind === k.id} className={'search-chip' + (ws.length && !counts[k.id] ? ' none' : '')}
              onClick={() => { setKind(k.id); setSelKey(''); input.current?.focus() }}>
              {k.label}{ws.length > 0 && res && <span className="mono">{counts[k.id] >= 1000 ? '999+' : counts[k.id] || 0}</span>}
            </button>
          ))}
        </div>

        <div className="search-body">
          <div className="search-list" ref={listRef} role="listbox" aria-label="Результаты">
            {!ws.length && (
              <div className="search-hello">
                {recent.length > 0 && <div className="search-group-head"><span>Недавние запросы</span></div>}
                {recent.map((t) => (
                  <button key={t} type="button" className="search-recent" onClick={() => { setQ(t); input.current?.focus() }}><Icon name="refresh" size={14} /> {t}</button>
                ))}
                <p className="hint" style={{ padding: '14px 12px', fontSize: 13, lineHeight: 1.6 }}>
                  Ищем по клиентам (имя, @username, телефон, заметка), тексту переписки, чекам, именам файлов и истории клиентов.
                  Сумму можно писать как «25 000», «25000» или «25к». Регистр и «ё/е» не важны.
                </p>
              </div>
            )}
            {nothing && (
              <div className="search-empty">Ничего не нашли по «{q}».<br />Ищем только то, что уже есть в CRM: в Telegram и WhatsApp запросов нет.</div>
            )}
            {groups.map((g) => (
              <div key={g.id} className="search-group">
                <div className="search-group-head">
                  <span>{g.title}</span><span className="mono">{g.total >= 1000 ? '999+' : g.total}</span>
                  {kind === 'all' && g.total > g.items.length && (
                    <button type="button" onClick={() => { setKind(g.id); setSelKey(''); input.current?.focus() }}>ещё {g.total - g.items.length} →</button>
                  )}
                </div>
                {g.items.map((x) => (
                  <button key={x.key} data-key={x.key} type="button" role="option" aria-selected={sel?.key === x.key} className="search-row"
                    onMouseMove={() => sel?.key !== x.key && setSelKey(x.key)} onClick={() => go(x)}>
                    <RowIcon x={x} chatById={chatById} />
                    <span className="search-row-main">
                      <span className="search-row-title"><Hl text={x.title} ws={ws} /></span>
                      <span className="search-row-sub">{x.deleted && <i>удалено · </i>}<Hl text={x.sub} ws={ws} /></span>
                    </span>
                    <span className="search-row-right">
                      <span>{x.right}</span>
                      {x.tag && <span className={'search-tag' + (x.tagKind ? ' rc-st st-' + x.tagKind : '')}>{x.tag}</span>}
                    </span>
                  </button>
                ))}
              </div>
            ))}
          </div>

          <aside className="search-preview" aria-label="Предпросмотр">
            {sel ? <Preview x={sel} ws={ws} ctx={ctx[sel.key]} chatById={chatById} typeLabel={typeLabel} go={go} /> : (
              <div className="search-preview-empty">{ws.length ? '' : 'Начните печатать — здесь будет предпросмотр'}</div>
            )}
          </aside>
        </div>

        <div className="search-foot">
          <span><kbd>↑</kbd><kbd>↓</kbd> выбор</span>
          <span><kbd>Enter</kbd> открыть</span>
          <span><kbd>Tab</kbd> раздел</span>
          <span><kbd>Esc</kbd> закрыть</span>
          <span style={{ marginLeft: 'auto' }}>{ws.length && res ? `Найдено: ${counts.all >= 1000 ? '1000+' : counts.all}` : 'Поиск в базе CRM на этом компьютере'}</span>
        </div>
      </div>
    </div>
  )
}

function RowIcon({ x, chatById }) {
  if (x.kind === 'client') return <Avatar chat={x.chat} size={36} />
  const icon = { msg: 'chat', receipt: 'receipt', file: 'file', event: 'card', nav: 'arrow' }[x.kind]
  return <span className={'search-row-ico k-' + x.kind}><Icon name={icon} size={16} /></span>
}

function Preview({ x, ws, ctx, chatById, typeLabel, go }) {
  const chatId = x.chat?.id || x.m?.chatId || x.r?.chatId || x.f?.chatId || x.e?.chatId
  const c = chatId ? chatById.get(chatId) || x.chat : null
  const head = c ? (
    <div className="search-pv-head">
      <Avatar chat={c} size={44} />
      <div style={{ minWidth: 0 }}>
        <div className="search-pv-name">{c.title}</div>
        <div className="hint" style={{ fontSize: 12 }}>{[chName(c.channel), c.stage && `этап «${stageTitle(c.stage)}»`].filter(Boolean).join(' · ')}</div>
      </div>
    </div>
  ) : null
  const fact = (k, v, cls) => v ? <div className="search-fact"><span>{k}</span><span className={cls}>{v}</span></div> : null

  if (x.kind === 'client') {
    return (
      <>
        {head}
        <div className="search-pv-title">Клиент</div>
        {c.lastMsg && <div className={'search-bubble' + (c.lastOut ? ' out' : '')}><span>{c.lastMsg}</span><small>{when(c.lastAt)}</small></div>}
        {fact('Username', c.username && '@' + c.username.replace(/^@/, ''))}
        {fact('Телефон', c.phone)}
        {fact('Сумма сделки', c.amount ? money(c.amount) : '', 'mono')}
        {fact('Следующий шаг', c.nextStep)}
        {fact('Заметка', c.note)}
        <div className="search-pv-acts"><button type="button" className="btn btn-primary btn-sm" onClick={() => go(x)}>Открыть диалог</button></div>
      </>
    )
  }
  if (x.kind === 'msg') {
    const list = ctx || [x.m]
    return (
      <>
        {head}
        <div className="search-pv-title">Сообщение в переписке · {day(x.m.date)}</div>
        <div className="search-ctx">
          {list.map((m) => (
            <div key={m.msgId} className={'search-bubble' + (m.out ? ' out' : '') + (m.msgId === x.m.msgId ? ' hit' : '')}>
              <span>{m.msgId === x.m.msgId ? <Hl text={m.text} ws={ws} /> : (m.text || (m.mediaKind === 'photo' ? 'Фото' : m.mediaKind === 'file' ? 'Файл: ' + m.fileName : ''))}</span>
              <small>{clock(m.date)}{m.deleted ? ' · удалено' : ''}</small>
            </div>
          ))}
        </div>
        <div className="search-pv-acts"><button type="button" className="btn btn-primary btn-sm" onClick={() => go(x)}>Открыть в переписке</button></div>
      </>
    )
  }
  if (x.kind === 'receipt') {
    const r = x.r
    return (
      <>
        {head}
        <div className="search-pv-title">Чек · {day(r.paidAt)}</div>
        <div className="search-pv-box">
          {r.parts.map((p, i) => (
            <div key={i} className="search-fact"><span>{typeLabel(p.kind)}{p.what ? <small> · {p.what}</small> : null}</span><span className="mono">{money(p.amount, r.currency)}</span></div>
          ))}
          <div className="search-fact total"><span>Итого</span><span className="mono">{money(r.amount, r.currency)}</span></div>
        </div>
        {fact('Статус', RECEIPT_ST[r.status], 'rc-st st-' + r.status)}
        {fact('Файл', r.hasFile ? r.fileName : 'нет')}
        <div className="search-pv-acts">
          <button type="button" className="btn btn-primary btn-sm" onClick={() => go(x)}>Открыть в «Чеках»</button>
          {r.hasFile && <button type="button" className="btn btn-sm" onClick={() => go(x, 'file')}>Посмотреть файл</button>}
        </div>
      </>
    )
  }
  if (x.kind === 'file') {
    return (
      <>
        {head}
        <div className="search-pv-title">Файл из переписки</div>
        {fact('Файл', <Hl text={x.f.fileName} ws={ws} />)}
        {fact('Размер', kb(x.f.fileSize))}
        {fact('Кто прислал', x.f.out ? 'вы' : 'клиент')}
        {fact('Когда', day(x.f.date) + ', ' + clock(x.f.date))}
        <div className="search-pv-acts">
          <button type="button" className="btn btn-primary btn-sm" onClick={() => go(x)}>Открыть</button>
          <button type="button" className="btn btn-sm" onClick={() => go(x, 'chat')}>Показать в переписке</button>
        </div>
      </>
    )
  }
  if (x.kind === 'event') {
    return (
      <>
        {head}
        <div className="search-pv-title">История клиента</div>
        <div className="search-pv-box"><Hl text={x.e.text} ws={ws} /></div>
        {fact('Когда', day(x.e.at) + ', ' + clock(x.e.at))}
        <div className="search-pv-acts"><button type="button" className="btn btn-primary btn-sm" onClick={() => go(x)}>Открыть диалог</button></div>
      </>
    )
  }
  return (
    <>
      <div className="search-pv-title">Раздел CRM</div>
      <div className="search-pv-name">{x.p.title}</div>
      <p className="hint" style={{ margin: 0 }}>{x.p.text}</p>
      <div className="search-pv-acts"><button type="button" className="btn btn-primary btn-sm" onClick={() => go(x)}>Перейти</button></div>
    </>
  )
}
