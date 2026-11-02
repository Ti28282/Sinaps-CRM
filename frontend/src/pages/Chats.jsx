import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { api, on, STAGES, errText, when, clock, dayLabel, pluginOn, canBeReceipt, money } from '../api.js'
import { Avatar, ChannelChip, Icon } from '../components/ui.jsx'

export default function Chats({ chats, openId, setOpenId, toast, tg, wa, plugins }) {
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
      {openId ? <Thread key={openId} id={openId} toast={toast} tg={tg} wa={wa} cheki={pluginOn(plugins, 'cheki')} onDeleted={() => setOpenId(null)} /> : (
        <div className="empty"><div><Icon name="chat" size={36} /><h3>Нет диалогов</h3><p>Подключите Telegram и выберите папку с клиентами.</p></div></div>
      )}
    </div>
  )
}

function Thread({ id, toast, tg, wa, cheki, onDeleted }) {
  const [view, setView] = useState(null)
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [photo, setPhoto] = useState(null)
  const [attach, setAttach] = useState(null) // { path, name, size, isImage, tooBig }
  const [noCompress, setNoCompress] = useState(false)
  const [upload, setUpload] = useState(null) // { done, total }
  const [dragOver, setDragOver] = useState(false)
  const [kinds, setKinds] = useState('PDF и фото') // настройка плагина «Чеки»
  const [receiptFor, setReceiptFor] = useState(null) // сообщение, которое отмечаем как чек
  const [deleteFor, setDeleteFor] = useState(null) // сообщение, которое удаляем
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
    const off6 = on('receipts:changed', (chatId) => chatId === id && api.chat(id).then((v) => setView((cur) => (cur ? { ...cur, chat: v.chat, events: v.events, receipts: v.receipts } : v))).catch(() => {}))
    const off7 = on('plugin:settings', (p) => p?.id === 'cheki' && p.settings?.kinds && setKinds(p.settings.kinds))
    return () => { [off1, off2, off3, off4, off5, off6, off7].forEach((f) => f && f()) }
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps

  // плагин «Чеки» включили/выключили — подтягиваем отметки и настройку
  useEffect(() => {
    api.chat(id).then((v) => setView((cur) => (cur ? { ...cur, receipts: v.receipts } : v))).catch(() => {})
    if (cheki) api.pluginSettings('cheki').then((st) => st?.kinds && setKinds(st.kinds)).catch(() => {})
  }, [cheki, id])

  useEffect(() => {
    if (body.current) body.current.scrollTop = body.current.scrollHeight
  }, [view?.messages.length])

  if (!view) return <div className="empty">Загрузка…</div>
  const c = view.chat
  const net = c.channel === 'wa' ? { name: 'WhatsApp', ready: wa?.state === 'ready' } : { name: 'Telegram', ready: tg.state === 'ready' }
  const canSend = c.channel !== 'manual' && net.ready // лиды из файла — без мессенджера
  const receipts = cheki && view.receipts ? view.receipts : null
  const receiptOf = new Map((receipts || []).map((r) => [r.msgId, r]))

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
                <div className={'bubble' + (m.out ? ' out' : '') + (m.deleted ? ' deleted' : '') + (m.mediaKind === 'photo' ? ' has-photo' : '') + (receiptOf.has(m.msgId) ? ' is-receipt' : '')}>
                  {m.mediaKind === 'photo' && <Photo m={m} open={setPhoto} />}
                  {m.mediaKind === 'file' && <FileCard m={m} toast={toast} />}
                  {m.text && <div className="bubble-text">{m.text}</div>}
                  {receiptOf.has(m.msgId) && <ReceiptTag r={receiptOf.get(m.msgId)} onClick={() => setReceiptFor(m)} />}
                  <div className="time">
                    {m.deleted && <span className="deleted-tag">удалено клиентом · </span>}
                    {clock(m.date)}
                  </div>
                  <div className="msg-actions">
                    {receipts && !m.out && !receiptOf.has(m.msgId) && canBeReceipt(kinds, m) && (
                      <button type="button" className="msg-act receipt" aria-label="Отметить как чек" title="Отметить как чек" onClick={() => setReceiptFor(m)}>
                        <Icon name="receipt" size={15} />
                      </button>
                    )}
                    <button type="button" className="msg-act" aria-label="Удалить сообщение" title="Удалить сообщение" onClick={() => setDeleteFor(m)}>
                      <Icon name="trash" size={15} />
                    </button>
                  </div>
                </div>
              </Fragment>
            )
          })}
        </div>

        {receiptFor && (
          <ReceiptDialog m={receiptFor} chatId={id} r={receiptOf.get(receiptFor.msgId)} toast={toast} close={() => setReceiptFor(null)} />
        )}
        {deleteFor && (
          <DeleteDialog m={deleteFor} chat={c} net={net} canSend={canSend} toast={toast} close={() => setDeleteFor(null)} />
        )}

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
            <span>{canSend ? (attach ? 'Подпись к файлу · Ctrl+Enter — отправить' : `Ответ уйдёт в ${net.name} · Ctrl+Enter — отправить · файл можно перетащить`) : c.channel === 'manual' ? 'Лид из файла — у него нет чата в мессенджере' : `${net.name} не подключён`}</span>
            <textarea className="textarea" rows={2} placeholder={attach ? 'Подпись (необязательно)…' : 'Написать сообщение…'} value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); send() } }} />
          </label>
          <button type="button" className="send-btn" aria-label="Отправить" onClick={send} disabled={sending || (!text.trim() && !attach) || !canSend}>
            <Icon name="arrow" size={20} />
          </button>
        </div>
      </section>

      <ClientCard key={c.id + ':' + c.stage + ':' + c.amount} chat={c} events={view.events} patch={patch} receipts={receipts} />
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

function ClientCard({ chat: c, events, patch, receipts }) {
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

      {receipts && (
        <div className="field">
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <span className="eyebrow">Чеки клиента</span>
            <span className="hint" style={{ fontSize: 12 }}>{receipts.length} шт.</span>
          </div>
          {receipts.length === 0 ? (
            <p className="hint" style={{ fontSize: 13 }}>Наведите на PDF или фото клиента в переписке и нажмите значок чека.</p>
          ) : (
            <div className="client-receipts">
              {receipts.slice(0, 6).map((r) => (
                <div key={r.id} className="client-receipt">
                  <span className="mono">{new Date(r.paidAt * 1000).toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit' })}</span>
                  <span className="cr-what">{r.what || r.fileName}</span>
                  <span className={'mono cr-sum st-' + r.status}>{money(r.amount, r.currency)}</span>
                </div>
              ))}
              <div className="client-receipt total">
                <span>Оплачено</span>
                <span className="mono">{money(receipts.filter((r) => r.status === 'paid').reduce((a, r) => a + r.amount, 0), receipts[0]?.currency)}</span>
              </div>
            </div>
          )}
        </div>
      )}

      <div className="field">
        <div className="eyebrow">История</div>
        <div className="history">
          {events.map((e, i) => <div key={i}><i aria-hidden="true" />{e.text} · {when(e.at)}</div>)}
        </div>
      </div>
    </aside>
  )
}

const RECEIPT_STATUS = { paid: 'оплачен', wait: 'ожидает', refund: 'возврат' }

function ReceiptTag({ r, onClick }) {
  return (
    <button type="button" className={'receipt-tag st-' + r.status} onClick={onClick} title="Изменить чек">
      <Icon name="receipt" size={14} />
      Чек · {money(r.amount, r.currency)} · {RECEIPT_STATUS[r.status]}
      <span className="receipt-date">{new Date(r.paidAt * 1000).toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' })}</span>
    </button>
  )
}

const isoDay = (unix) => {
  const d = new Date(unix * 1000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function ReceiptDialog({ m, chatId, r, toast, close }) {
  const [amount, setAmount] = useState(r ? String(r.amount) : '')
  const [date, setDate] = useState(isoDay(r ? r.paidAt : m.date))
  const [status, setStatus] = useState(r?.status || 'paid')
  const [what, setWhat] = useState(r?.what || '')
  const [busy, setBusy] = useState(false)

  async function save() {
    setBusy(true)
    try {
      await api.markReceipt({ chatId, msgId: m.msgId, amount: Number(amount || 0), date, status, what })
      toast(r ? 'Чек изменён' : 'Чек сохранён')
      close()
    } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }
  async function unmark() {
    setBusy(true)
    try { await api.unmarkReceipt(chatId, m.msgId); toast('Отметка «чек» снята'); close() } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }

  return (
    <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <form className="modal modal-sm" role="dialog" aria-modal="true" aria-labelledby="rc-title" onSubmit={(e) => { e.preventDefault(); save() }}>
        <div className="modal-head">
          <div>
            <h2 id="rc-title">{r ? 'Чек от клиента' : 'Отметить как чек'}</h2>
            <p className="hint" style={{ marginTop: 4 }}>{m.fileName || (m.mediaKind === 'photo' ? 'Фото' : 'Файл')}</p>
          </div>
          <button type="button" className="icon-btn" aria-label="Закрыть" onClick={close}><Icon name="close" /></button>
        </div>
        <label className="field">
          <span>Сумма</span>
          <input className="input mono" style={{ fontSize: 16, fontWeight: 600 }} inputMode="numeric" autoFocus placeholder="25000"
            value={amount} onChange={(e) => setAmount(e.target.value.replace(/\D/g, ''))} />
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
        <label className="field">
          <span>За что</span>
          <input className="input" placeholder="Бот записи · этап 2" value={what} onChange={(e) => setWhat(e.target.value)} />
        </label>
        <div className="row" style={{ justifyContent: 'flex-end' }}>
          {r && <button type="button" className="btn btn-danger" style={{ marginRight: 'auto' }} disabled={busy} onClick={unmark}>Снять отметку</button>}
          <button type="button" className="btn" onClick={close}>Отмена</button>
          <button type="submit" className="btn btn-primary" disabled={busy || (!amount && status !== 'wait')}>{busy ? 'Сохраняем…' : 'Сохранить чек'}</button>
        </div>
      </form>
    </div>
  )
}

function DeleteDialog({ m, chat, net, canSend, toast, close }) {
  // у обоих: Telegram — и своё, и клиента; WhatsApp — только своё
  const allOk = canSend && (chat.channel === 'tg' || (chat.channel === 'wa' && m.out))
  const allWhy = !canSend ? `${net.name} не подключён` : chat.channel === 'wa' && !m.out ? 'В WhatsApp у обоих можно удалить только свои сообщения' : ''
  const [mode, setMode] = useState('crm')
  const [busy, setBusy] = useState(false)

  async function run() {
    setBusy(true)
    try {
      await api.removeMessage(chat.id, m.msgId, mode)
      toast(mode === 'all' ? `Сообщение удалено у обоих в ${net.name}` : `Сообщение убрано из CRM · в ${net.name} осталось`)
      close()
    } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }

  return (
    <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className="modal modal-sm" role="dialog" aria-modal="true" aria-labelledby="dm-title">
        <div className="modal-head">
          <h2 id="dm-title">Удалить сообщение?</h2>
          <button type="button" className="icon-btn" aria-label="Закрыть" onClick={close}><Icon name="close" /></button>
        </div>
        <p className="hint del-preview">{m.text || m.fileName || (m.mediaKind === 'photo' ? 'Фото' : 'Сообщение')}</p>
        <label className={'del-opt' + (mode === 'crm' ? ' sel' : '')}>
          <input type="radio" name="dm" checked={mode === 'crm'} onChange={() => setMode('crm')} />
          <span>
            <b>Только в CRM</b>
            <span className="hint">В {net.name} у {m.out ? 'клиента' : 'вас и клиента'} останется. Синхронизация его не вернёт.</span>
          </span>
        </label>
        <label className={'del-opt danger' + (mode === 'all' ? ' sel' : '') + (allOk ? '' : ' off')}>
          <input type="radio" name="dm" checked={mode === 'all'} disabled={!allOk} onChange={() => setMode('all')} />
          <span>
            <b>У обоих в {net.name}</b>
            <span className="hint">{allOk ? 'Исчезнет у вас и у клиента. Отменить нельзя.' : allWhy}</span>
          </span>
        </label>
        <div className="row" style={{ justifyContent: 'flex-end' }}>
          <button type="button" className="btn" onClick={close}>Отмена</button>
          <button type="button" className={'btn ' + (mode === 'all' ? 'btn-danger-solid' : 'btn-inv')} disabled={busy} onClick={run}>
            {busy ? 'Удаляем…' : mode === 'all' ? 'Удалить у обоих' : 'Удалить в CRM'}
          </button>
        </div>
      </div>
    </div>
  )
}
