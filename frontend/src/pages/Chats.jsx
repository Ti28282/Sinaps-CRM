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
  const [photo, setPhoto] = useState(null)
  const [attach, setAttach] = useState(null) // { path, name, size, isImage, tooBig }
  const [noCompress, setNoCompress] = useState(false)
  const [upload, setUpload] = useState(null) // { done, total }
  const [dragOver, setDragOver] = useState(false)
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
    const off3 = on('message:deleted', (r) => {
      if (r.chatId !== id) return
      const removed = new Set(r.removed || []), marked = new Set(r.marked || [])
      setView((v) => v && {
        ...v,
        messages: v.messages
          .filter((x) => !removed.has(x.msgId))
          .map((x) => (marked.has(x.msgId) ? { ...x, deleted: true } : x)),
      })
    })
    const off2 = on('chats:changed', () => api.chat(id).then((v) => setView((cur) => (cur ? { ...cur, chat: v.chat, events: v.events } : v))).catch(() => {}))
    const off4 = on('files:dropped', (paths) => { setDragOver(false); pickPath(paths[0], paths.length) })
    const off5 = on('upload:progress', (p) => p.chatId === id && setUpload({ done: p.done, total: p.total }))
    return () => { off1 && off1(); off2 && off2(); off3 && off3(); off4 && off4(); off5 && off5() }
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (body.current) body.current.scrollTop = body.current.scrollHeight
  }, [view?.messages.length])

  if (!view) return <div className="empty">Загрузка…</div>
  const c = view.chat
  const canSend = tg.state === 'ready' && c.channel === 'tg' // лиды из файла пока без мессенджера

  async function pickPath(path, count = 1) {
    if (!path) return
    if (count > 1) toast('Пока можно отправить один файл за раз — взят первый')
    try {
      const info = await api.fileInfo(path)
      if (info.tooBig) return toast(`«${info.name}» больше 1,5 ГБ (${fmtSize(info.size)})`, 'err')
      setAttach(info); setNoCompress(false)
    } catch (e) { toast(errText(e), 'err') }
  }

  async function chooseFile() {
    try { pickPath(await api.pickFile()) } catch (e) { toast(errText(e), 'err') }
  }

  async function send() {
    const t = text.trim()
    if (attach) {
      setSending(true); setUpload({ done: 0, total: attach.size })
      try {
        await api.sendFile(id, attach.path, t, attach.isImage && !noCompress)
        setAttach(null); setText(''); load()
      } catch (e) { toast(errText(e), 'err') } finally { setSending(false); setUpload(null) }
      return
    }
    if (!t) return
    setSending(true)
    try { await api.send(id, t); setText(''); load() } catch (e) { toast(errText(e), 'err') } finally { setSending(false) }
  }

  async function patch(p) {
    try { await api.updateChat(id, p) } catch (e) { toast(errText(e), 'err') }
  }

  async function removeMsg(m) {
    if (!window.confirm('Удалить сообщение у вас и у клиента в Telegram?')) return
    try { await api.deleteMessage(id, m.msgId) } catch (e) { toast(errText(e), 'err') }
  }

  async function remove() {
    if (!window.confirm(`Убрать «${c.title}» из CRM? В Telegram ничего не изменится, при синхронизации чат не вернётся (вернуть можно в «Подключениях»).`)) return
    try { await api.deleteChat(id); onDeleted() } catch (e) { toast(errText(e), 'err') }
  }

  let lastDay = ''
  return (
    <>
      <section className={'thread' + (dragOver ? ' drag-over' : '')} style={{ '--wails-drop-target': 'drop' }}
        onDragEnter={(e) => { e.preventDefault(); if (canSend) setDragOver(true) }}
        onDragOver={(e) => e.preventDefault()}
        onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setDragOver(false) }}
        onDrop={(e) => { e.preventDefault(); setDragOver(false) }}>
        {dragOver && <div className="drop-hint"><Icon name="clip" size={28} /> Отпустите, чтобы отправить файл</div>}
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
                <div className={'bubble' + (m.out ? ' out' : '') + (m.deleted ? ' deleted' : '') + (m.mediaKind === 'photo' ? ' has-photo' : '')}>
                  {m.mediaKind === 'photo' && <Photo m={m} open={setPhoto} />}
                  {m.mediaKind === 'file' && <FileCard m={m} toast={toast} />}
                  {m.text && <div className="bubble-text">{m.text}</div>}
                  <div className="time">
                    {m.deleted && <span className="deleted-tag">удалено клиентом · </span>}
                    {clock(m.date)}
                  </div>
                  {m.out && canSend && (
                    <button type="button" className="msg-del" aria-label="Удалить сообщение у всех" onClick={() => removeMsg(m)}>
                      <Icon name="trash" size={14} />
                    </button>
                  )}
                </div>
              </Fragment>
            )
          })}
        </div>

        {photo && (
          <button type="button" className="lightbox" aria-label="Закрыть фото" onClick={() => setPhoto(null)}>
            <img src={photo} alt="Фото из переписки" />
          </button>
        )}

        {attach && (
          <div className="attach">
            <div className="attach-ico"><Icon name={attach.isImage ? 'image' : 'file'} size={22} /></div>
            <div style={{ minWidth: 0, flex: 1 }}>
              <div className="attach-name">{attach.name}</div>
              <div className="hint" style={{ fontSize: 12 }}>
                {fmtSize(attach.size)}{attach.isImage ? (noCompress ? ' · уйдёт файлом без сжатия' : ' · уйдёт как фото (со сжатием)') : ' · уйдёт файлом'}
              </div>
              {upload && (
                <div className="progress" style={{ marginTop: 8 }}>
                  <div style={{ width: `${upload.total ? (upload.done / upload.total) * 100 : 0}%` }} />
                </div>
              )}
            </div>
            {attach.isImage && !sending && (
              <label className="check" style={{ fontSize: 13 }}>
                <input type="checkbox" checked={noCompress} onChange={(e) => setNoCompress(e.target.checked)} />
                Без сжатия
              </label>
            )}
            {!sending && <button type="button" className="icon-btn" aria-label="Убрать файл" onClick={() => setAttach(null)}><Icon name="close" /></button>}
            {upload && <span className="mono hint" style={{ fontSize: 12 }}>{upload.total ? Math.round((upload.done / upload.total) * 100) : 0}%</span>}
          </div>
        )}

        <div className="composer">
          <button type="button" className="icon-btn" style={{ height: 52, width: 52, borderRadius: 14, flexShrink: 0 }}
            aria-label="Прикрепить файл" title="Прикрепить файл (до 1,5 ГБ)" onClick={chooseFile} disabled={sending || !canSend}>
            <Icon name="clip" size={20} />
          </button>
          <label style={{ flex: 1 }} className="field">
            <span>{canSend ? (attach ? 'Подпись к файлу · Ctrl+Enter — отправить' : 'Ответ уйдёт в Telegram · Ctrl+Enter — отправить · файл можно перетащить') : c.channel === 'manual' ? 'Лид из файла — у него нет чата в мессенджере' : 'Telegram не подключён'}</span>
            <textarea className="textarea" rows={2} placeholder={attach ? 'Подпись (необязательно)…' : 'Написать сообщение…'} value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); send() } }} />
          </label>
          <button type="button" className="send-btn" aria-label="Отправить" onClick={send} disabled={sending || (!text.trim() && !attach) || !canSend}>
            <Icon name="arrow" size={20} />
          </button>
        </div>
      </section>

      <ClientCard key={c.id + ':' + c.stage} chat={c} events={view.events} patch={patch} />
    </>
  )
}

export function fmtSize(n) {
  if (!n) return '0 Б'
  const u = ['Б', 'КБ', 'МБ', 'ГБ']
  const i = Math.min(u.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  return (n / 1024 ** i).toLocaleString('ru-RU', { maximumFractionDigits: i ? 1 : 0 }) + ' ' + u[i]
}

function FileCard({ m, toast }) {
  const [busy, setBusy] = useState(false)
  async function open() {
    setBusy(true)
    try { await api.openAttachment(m.chatId, m.msgId) } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }
  return (
    <button type="button" className="file-card" onClick={open} disabled={busy} title="Скачать и открыть">
      <span className="file-ico"><Icon name="file" size={20} /></span>
      <span style={{ minWidth: 0, textAlign: 'left' }}>
        <span className="file-name">{m.fileName || 'Файл'}</span>
        <span className="file-size">{busy ? 'Скачиваем…' : fmtSize(m.fileSize)}</span>
      </span>
    </button>
  )
}

function Photo({ m, open }) {
  const [failed, setFailed] = useState(false)
  const src = m.mediaUrl || `/media/${m.chatId}/${m.msgId}`
  if (failed) return <div className="photo-missing">Фото недоступно</div>
  return (
    <button type="button" className="photo" onClick={() => open(src)} aria-label="Открыть фото">
      <img src={src} alt="Фото" loading="lazy" onError={() => setFailed(true)} />
    </button>
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
