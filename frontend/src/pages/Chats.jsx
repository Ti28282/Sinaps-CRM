import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { api, on, STAGES, errText, when, clock, dayLabel } from '../api.js'
import { Avatar, ChannelChip, Icon } from '../components/ui.jsx'

export default function Chats({ chats, openId, setOpenId, toast, tg }) {
  const [q, setQ] = useState('')
  const list = useMemo(() => {
    const s = q.trim().toLowerCase()
    return chats.filter((c) => !s || (c.title + ' ' + c.lastMsg).toLowerCase().includes(s))
  }, [chats, q])

  useEffect(() => {
    if (!openId && list.length) setOpenId(list[0].id)
  }, [openId, list, setOpenId])

  return (
    <div className="chats">
      <aside className="chat-list">
        <div className="chat-list-head">
          <h1>Диалоги</h1>
          <label className="search">
            <Icon name="search" size={16} />
            <span className="sr">Поиск по диалогам</span>
            <input type="search" placeholder="Поиск" value={q} onChange={(e) => setQ(e.target.value)} />
          </label>
        </div>
        <div className="chat-items">
          {list.map((c) => (
            <button key={c.id} type="button" className="chat-item" aria-current={c.id === openId} onClick={() => setOpenId(c.id)}>
              <div className="ava-wrap">
                <Avatar chat={c} size={40} />
                <span className="ava-ch" style={{ background: c.channel === 'wa' ? 'var(--wa)' : 'var(--tg)' }} aria-hidden="true" />
              </div>
              <div className="meta">
                <div className="line1"><span className="name">{c.title}</span><span className="t">{when(c.lastAt)}</span></div>
                <div className="line2"><span className="prev">{c.lastOut ? 'Вы: ' : ''}{c.lastMsg}</span>{c.unread && <span className="unread-dot" aria-label="Непрочитано" />}</div>
              </div>
            </button>
          ))}
          {list.length === 0 && <div style={{ padding: 20, color: 'var(--faint)', fontSize: 13 }}>Ничего не найдено</div>}
        </div>
      </aside>
      {openId ? <Thread key={openId} id={openId} toast={toast} tg={tg} onDeleted={() => setOpenId(null)} /> : (
        <div className="empty"><div><Icon name="chat" size={36} /><h3>Нет диалогов</h3><p>Подключите Telegram и выберите папку с клиентами.</p></div></div>
      )}
    </div>
  )
}

function Thread({ id, toast, tg, onDeleted }) {
  const [view, setView] = useState(null)
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const body = useRef(null)

  const load = () => api.chat(id).then(setView).catch((e) => toast(errText(e), 'err'))

  useEffect(() => {
    load()
    api.markRead(id).catch(() => {})
    const off1 = on('message:new', (m) => {
      if (m.chatId !== id) return
      setView((v) => (v && !v.messages.some((x) => x.msgId === m.msgId) ? { ...v, messages: [...v.messages, m] } : v))
      api.markRead(id).catch(() => {})
    })
    const off2 = on('chats:changed', () => api.chat(id).then((v) => setView((cur) => (cur ? { ...cur, chat: v.chat, events: v.events } : v))).catch(() => {}))
    return () => { off1 && off1(); off2 && off2() }
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (body.current) body.current.scrollTop = body.current.scrollHeight
  }, [view?.messages.length])

  if (!view) return <div className="empty">Загрузка…</div>
  const c = view.chat

  async function send() {
    const t = text.trim()
    if (!t) return
    setSending(true)
    try { await api.send(id, t); setText(''); load() } catch (e) { toast(errText(e), 'err') } finally { setSending(false) }
  }

  async function patch(p) {
    try { await api.updateChat(id, p) } catch (e) { toast(errText(e), 'err') }
  }

  async function remove() {
    if (!window.confirm(`Удалить «${c.title}» из CRM? Переписка в Telegram останется.`)) return
    try { await api.deleteChat(id); onDeleted() } catch (e) { toast(errText(e), 'err') }
  }

  let lastDay = ''
  return (
    <>
      <section className="thread">
        <header className="thread-head">
          <Avatar chat={c} size={44} />
          <div style={{ minWidth: 0 }}>
            <h2>{c.title}</h2>
            <div className="sub">{c.channel === 'wa' ? 'WhatsApp' : 'Telegram'}{c.username ? ` · @${c.username}` : ''}{c.phone ? ` · +${c.phone}` : ''}</div>
          </div>
          <div className="row" style={{ marginLeft: 'auto' }}>
            <button type="button" className="icon-btn" aria-label="Удалить из CRM" onClick={remove}><Icon name="trash" /></button>
          </div>
        </header>

        <div className="thread-body" ref={body}>
          {view.messages.length === 0 && <div className="day">Сообщений пока нет</div>}
          {view.messages.map((m) => {
            const d = dayLabel(m.date)
            const showDay = d !== lastDay
            lastDay = d
            return (
              <Fragment key={m.msgId}>
                {showDay && <div className="day">{d}</div>}
                <div className={'bubble' + (m.out ? ' out' : '')}>
                  {m.text}
                  <div className="time">{clock(m.date)}</div>
                </div>
              </Fragment>
            )
          })}
        </div>

        <div className="composer">
          <label style={{ flex: 1 }} className="field">
            <span>{tg.state === 'ready' ? 'Ответ уйдёт в Telegram · Ctrl+Enter — отправить' : 'Telegram не подключён'}</span>
            <textarea className="textarea" rows={2} placeholder="Написать сообщение…" value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); send() } }} />
          </label>
          <button type="button" className="send-btn" aria-label="Отправить" onClick={send} disabled={sending || !text.trim() || tg.state !== 'ready'}>
            <Icon name="arrow" size={20} />
          </button>
        </div>
      </section>

      <ClientCard key={c.id + ':' + c.stage} chat={c} events={view.events} patch={patch} />
    </>
  )
}

function ClientCard({ chat: c, events, patch }) {
  const [amount, setAmount] = useState(c.amount ? String(c.amount) : '')
  const [nextStep, setNextStep] = useState(c.nextStep)
  const [tags, setTags] = useState(c.tags)
  const [note, setNote] = useState(c.note)

  return (
    <aside className="client">
      <div className="client-head">
        <Avatar chat={c} size={72} />
        <div className="title">{c.title}</div>
        <ChannelChip channel={c.channel} />
      </div>

      <div className="field">
        <div className="eyebrow">Этап</div>
        <div className="row" role="group" aria-label="Этап сделки" style={{ gap: 6 }}>
          {STAGES.map((s) => (
            <button key={s.id} type="button" className="pill" aria-pressed={c.stage === s.id} onClick={() => patch({ stage: s.id })}>{s.title}</button>
          ))}
        </div>
      </div>

      <div className="kv">
        <label>
          <span className="k">Сумма, ₽</span>
          <input className="mono" inputMode="numeric" placeholder="0" value={amount}
            onChange={(e) => setAmount(e.target.value.replace(/\D/g, ''))}
            onBlur={() => Number(amount || 0) !== c.amount && patch({ amount: Number(amount || 0) })} />
        </label>
        <label>
          <span className="k">Следующий шаг</span>
          <input placeholder="Созвон завтра в 15:00" value={nextStep}
            onChange={(e) => setNextStep(e.target.value)} onBlur={() => nextStep !== c.nextStep && patch({ nextStep })} />
        </label>
        <label>
          <span className="k">Теги</span>
          <input placeholder="фитнес, бот записи" value={tags}
            onChange={(e) => setTags(e.target.value)} onBlur={() => tags !== c.tags && patch({ tags })} />
        </label>
        <div className="kv-row">
          <span className="k">В CRM с</span>
          <span>{new Date(c.createdAt * 1000).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })}</span>
        </div>
      </div>

      <label className="field">
        <span className="eyebrow">Заметка</span>
        <textarea className="textarea" rows={4} placeholder="Что важно помнить о клиенте" value={note}
          onChange={(e) => setNote(e.target.value)} onBlur={() => note !== c.note && patch({ note })} />
      </label>

      <div className="field">
        <div className="eyebrow">История</div>
        <div className="history">
          {events.map((e, i) => <div key={i}><i aria-hidden="true" />{e.text} · {when(e.at)}</div>)}
        </div>
      </div>
    </aside>
  )
}
