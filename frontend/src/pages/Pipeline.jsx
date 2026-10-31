import { useEffect, useMemo, useRef, useState } from 'react'
import { api, STAGES, errText, money, when } from '../api.js'
import { Avatar, ChannelChip, Icon } from '../components/ui.jsx'

export default function Pipeline({ chats, tg, toast, openChat }) {
  const [channel, setChannel] = useState('all')
  const [q, setQ] = useState('')
  const [dragId, setDragId] = useState(null)
  const [over, setOver] = useState(null)
  const [ghost, setGhost] = useState(null) // { title, x, y }
  const drag = useRef(null) // { id, title, x0, y0, active }
  const justDragged = useRef(false)
  const boardRef = useRef(null)

  // Перетаскивание на указателе (мышь/тачпад), а не на HTML5 drag-and-drop:
  // так оно работает в любом webview и не конфликтует с перетаскиванием файлов.
  useEffect(() => {
    function onMove(e) {
      const d = drag.current
      if (!d) return
      if (!d.active) {
        if (Math.hypot(e.clientX - d.x0, e.clientY - d.y0) < 6) return
        d.active = true
        setDragId(d.id)
        document.body.classList.add('dragging-card')
      }
      setGhost({ title: d.title, x: e.clientX, y: e.clientY })
      const col = document.elementFromPoint(e.clientX, e.clientY)?.closest('[data-stage]')
      setOver(col ? col.dataset.stage : null)
      // автопрокрутка доски у краёв
      const b = boardRef.current
      if (b) {
        const r = b.getBoundingClientRect()
        if (e.clientX > r.right - 60) b.scrollLeft += 18
        else if (e.clientX < r.left + 60) b.scrollLeft -= 18
      }
    }
    function onUp(e) {
      const d = drag.current
      drag.current = null
      if (!d || !d.active) return
      justDragged.current = true
      setTimeout(() => { justDragged.current = false }, 0)
      const col = document.elementFromPoint(e.clientX, e.clientY)?.closest('[data-stage]')
      setDragId(null); setOver(null); setGhost(null)
      document.body.classList.remove('dragging-card')
      if (col) move(d.id, col.dataset.stage)
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onUp)
    return () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onUp)
    }
  }) // eslint-disable-line react-hooks/exhaustive-deps
  const [colQ, setColQ] = useState({}) // поиск по названию внутри колонки
  const [syncing, setSyncing] = useState(false)

  const visible = useMemo(() => {
    const s = q.trim().toLowerCase()
    return chats
      .filter((c) => channel === 'all' || c.channel === channel)
      .filter((c) => !s || (c.title + ' ' + c.lastMsg + ' ' + c.note + ' ' + c.tags).toLowerCase().includes(s))
  }, [chats, channel, q])

  const stats = useMemo(() => ({
    total: visible.length,
    unread: visible.filter((c) => c.unread).length,
    pipeline: visible.filter((c) => c.stage !== 'deal' && c.stage !== 'lost').reduce((s, c) => s + (c.amount || 0), 0),
    won: visible.filter((c) => c.stage === 'deal').reduce((s, c) => s + (c.amount || 0), 0),
  }), [visible])

  async function move(id, stage) {
    const c = chats.find((x) => x.id === id)
    if (!c || c.stage === stage) return
    try { await api.updateChat(id, { stage }) } catch (e) { toast(errText(e), 'err') }
  }

  async function sync() {
    setSyncing(true)
    try {
      const r = await api.resync()
      toast(`Синхронизировано: ${r.chats} чатов, новых сообщений ${r.newMessages}`)
    } catch (e) { toast(errText(e), 'err') } finally { setSyncing(false) }
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Воронка</h1>
          <p>Все чаты из папки клиентов в одном месте</p>
        </div>
        <div className="row">
          <label className="search">
            <Icon name="search" size={16} />
            <span className="sr">Поиск клиентов</span>
            <input type="search" placeholder="Поиск по имени, сообщению, заметке" value={q} onChange={(e) => setQ(e.target.value)} />
          </label>
          <button type="button" className="btn" onClick={sync} disabled={syncing || tg.state !== 'ready'}>
            <Icon name="sync" size={16} /> {syncing ? 'Синхронизация…' : 'Синхронизировать'}
          </button>
        </div>
      </header>

      <div className="stats">
        <div className="stat"><div className="stat-label">Клиентов в воронке</div><div className="stat-value">{stats.total}</div></div>
        <div className="stat"><div className="stat-label">Ждут вашего ответа</div><div className="stat-value" style={{ color: 'var(--accent-text)' }}>{stats.unread}</div></div>
        <div className="stat"><div className="stat-label">Сумма в работе</div><div className="stat-value">{money(stats.pipeline)}</div></div>
        <div className="stat"><div className="stat-label">Закрыто сделок на</div><div className="stat-value">{money(stats.won)}</div></div>
      </div>

      <div className="row" style={{ justifyContent: 'space-between' }}>
        <div className="seg" role="group" aria-label="Канал">
          {[['all', 'Все каналы'], ['tg', 'Telegram'], ['wa', 'WhatsApp'], ['mail', 'Gmail']].map(([id, label]) => (
            <button key={id} type="button" aria-pressed={channel === id} onClick={() => setChannel(id)}>{label}</button>
          ))}
        </div>
        <div style={{ fontSize: 13, color: 'var(--faint)' }}>Перетащите карточку, чтобы сменить этап</div>
      </div>

      {ghost && <div className="drag-ghost" style={{ left: ghost.x + 12, top: ghost.y + 8 }}>{ghost.title}</div>}
      <div className="board" ref={boardRef}>
        {STAGES.map((st) => {
          const all = visible.filter((c) => c.stage === st.id)
          const cq = (colQ[st.id] || '').trim().toLowerCase()
          const cards = cq ? all.filter((c) => c.title.toLowerCase().includes(cq)) : all
          const sum = all.reduce((s, c) => s + (c.amount || 0), 0)
          return (
            <section
              key={st.id}
              className={'col' + (over === st.id ? ' drop' : '')}
              data-stage={st.id}
            >
              <div className="col-head">
                <span className="dot" style={{ background: st.color, marginLeft: 0 }} aria-hidden="true" />
                <h2>{st.title}</h2>
                <span className="n">{cq ? `${cards.length}/${all.length}` : all.length}</span>
              </div>
              {sum > 0 && <div className="col-sum">{money(sum)}</div>}
              {all.length > 0 && (
                <label className="col-search">
                  <Icon name="search" size={14} />
                  <span className="sr">Поиск в колонке {st.title}</span>
                  <input type="search" placeholder="Найти по названию" value={colQ[st.id] || ''}
                    onChange={(e) => setColQ((m) => ({ ...m, [st.id]: e.target.value }))} />
                </label>
              )}
              {cards.length === 0 && <div className="col-empty">{cq ? 'Не найдено' : 'Пусто'}</div>}
              <div className="col-cards">
              {cards.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  className={'card' + (dragId === c.id ? ' dragging' : '')}
                  onPointerDown={(e) => { if (e.button === 0) drag.current = { id: c.id, title: c.title, x0: e.clientX, y0: e.clientY, active: false } }}
                  onClick={() => { if (!justDragged.current) openChat(c.id) }}
                >
                  <div className="card-top">
                    <Avatar chat={c} />
                    <div className="card-name">{c.title}</div>
                    <div className="card-time">{when(c.lastAt)}</div>
                  </div>
                  {c.lastMsg && <div className="card-msg">{c.lastOut ? 'Вы: ' : ''}{c.lastMsg}</div>}
                  <div className="card-foot">
                    <ChannelChip channel={c.channel} />
                    {c.unread && <span className="new-badge">новое</span>}
                    {c.amount > 0 && <span className="card-amount">{money(c.amount)}</span>}
                  </div>
                </button>
              ))}
              </div>
            </section>
          )
        })}
      </div>
    </div>
  )
}
