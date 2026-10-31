import { useMemo, useState } from 'react'
import { api, STAGES, errText, money, when } from '../api.js'
import { Avatar, ChannelChip, Icon } from '../components/ui.jsx'

export default function Pipeline({ chats, tg, toast, openChat }) {
  const [channel, setChannel] = useState('all')
  const [q, setQ] = useState('')
  const [dragId, setDragId] = useState(null)
  const [over, setOver] = useState(null)
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
          {[['all', 'Все каналы'], ['tg', 'Telegram'], ['wa', 'WhatsApp']].map(([id, label]) => (
            <button key={id} type="button" aria-pressed={channel === id} onClick={() => setChannel(id)}>{label}</button>
          ))}
        </div>
        <div style={{ fontSize: 13, color: 'var(--faint)' }}>Перетащите карточку, чтобы сменить этап</div>
      </div>

      <div className="board">
        {STAGES.map((st) => {
          const cards = visible.filter((c) => c.stage === st.id)
          const sum = cards.reduce((s, c) => s + (c.amount || 0), 0)
          return (
            <section
              key={st.id}
              className={'col' + (over === st.id ? ' drop' : '')}
              onDragOver={(e) => { e.preventDefault(); setOver(st.id) }}
              onDragLeave={() => setOver((o) => (o === st.id ? null : o))}
              onDrop={(e) => { e.preventDefault(); setOver(null); move(Number(e.dataTransfer.getData('text/plain')), st.id) }}
            >
              <div className="col-head">
                <span className="dot" style={{ background: st.color, marginLeft: 0 }} aria-hidden="true" />
                <h2>{st.title}</h2>
                <span className="n">{cards.length}</span>
              </div>
              {sum > 0 && <div className="col-sum">{money(sum)}</div>}
              {cards.length === 0 && <div className="col-empty">Пусто</div>}
              {cards.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  className={'card' + (dragId === c.id ? ' dragging' : '')}
                  draggable
                  onDragStart={(e) => { setDragId(c.id); e.dataTransfer.setData('text/plain', String(c.id)) }}
                  onDragEnd={() => setDragId(null)}
                  onClick={() => openChat(c.id)}
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
            </section>
          )
        })}
      </div>
    </div>
  )
}
