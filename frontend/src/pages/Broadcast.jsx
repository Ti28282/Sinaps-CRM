import { useEffect, useMemo, useState } from 'react'
import { api, on, STAGES, errText, when } from '../api.js'
import { Avatar, Icon } from '../components/ui.jsx'

export default function Broadcast({ chats, settings, reloadSettings, tg, toast }) {
  const [tpl, setTpl] = useState('')
  const [stage, setStage] = useState('new')
  const [picked, setPicked] = useState(new Set())
  const [preview, setPreview] = useState('')
  const [st, setSt] = useState(null)
  const [now, setNow] = useState(Date.now())

  useEffect(() => { if (settings && !tpl) setTpl(settings.template) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    api.broadcastStatus().then(setSt)
    const off = on('broadcast:status', setSt)
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => { off && off(); clearInterval(t) }
  }, [])

  const list = useMemo(() => chats.filter((c) => c.stage === stage && c.channel === 'tg'), [chats, stage])
  const left = st ? Math.max(0, st.dailyLimit - st.sentToday) : 0

  // по умолчанию выбираем всех, кому ещё не писали, в пределах лимита
  useEffect(() => { setPicked(new Set(list.slice(0, left || list.length).map((c) => c.id))) }, [stage, list.length, left])

  const firstId = list.find((c) => picked.has(c.id))?.id
  useEffect(() => {
    if (!tpl) return setPreview('')
    if (!firstId) return setPreview(tpl.replaceAll('{name}', 'Иван').replaceAll('{title}', 'Иван Петров'))
    api.previewTemplate(tpl, firstId).then(setPreview)
  }, [tpl, firstId])

  const toggle = (id) => setPicked((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n })

  async function start() {
    const ids = list.filter((c) => picked.has(c.id)).map((c) => c.id)
    if (!window.confirm(`Отправить сообщение ${Math.min(ids.length, left)} клиентам? Между сообщениями пауза 40–90 секунд.`)) return
    try { await api.startBroadcast(tpl, ids); reloadSettings() } catch (e) { toast(errText(e), 'err') }
  }

  const running = st?.running
  const wait = st?.nextAt ? Math.max(0, Math.round(st.nextAt - now / 1000)) : 0
  const count = list.filter((c) => picked.has(c.id)).length

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Рассылки</h1>
          <p>Одно сообщение выбранным клиентам: паузы и дневной лимит защищают аккаунт от блокировки</p>
        </div>
        {st && (
          <div className="stat" style={{ flex: '0 0 auto', padding: '12px 18px' }}>
            <div className="stat-label">Отправлено сегодня</div>
            <div className="stat-value" style={{ fontSize: 22, marginTop: 4 }}>{st.sentToday} <span style={{ color: 'var(--faint)' }}>/ {st.dailyLimit}</span></div>
          </div>
        )}
      </header>

      {running && (
        <section className="panel">
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <h2>Идёт рассылка: {st.done} из {st.total}{st.failed ? ` · ошибок ${st.failed}` : ''}</h2>
            <button type="button" className="btn btn-danger" onClick={() => api.stopBroadcast()}><Icon name="stop" size={16} /> Остановить</button>
          </div>
          <div className="progress"><div style={{ width: `${st.total ? ((st.done + st.failed) / st.total) * 100 : 0}%` }} /></div>
          <p className="hint">{st.current ? `Отправляем: ${st.current}` : wait ? `Следующее через ${wait} с` : 'Готовим…'}</p>
          {st.lastError && <p className="hint" style={{ color: 'var(--danger)' }}>{st.lastError}</p>}
        </section>
      )}

      <div className="grid-2">
        <section className="panel">
          <h2>Сообщение</h2>
          <label className="field">
            <span>Шаблон · {'{name}'} — имя, {'{title}'} — полное название чата</span>
            <textarea className="textarea" rows={7} value={tpl} onChange={(e) => setTpl(e.target.value)} disabled={running} />
          </label>
          <div className="field">
            <span className="eyebrow">Так увидит клиент</span>
            <div className="preview">{preview || '—'}</div>
          </div>
        </section>

        <section className="panel">
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <h2>Получатели</h2>
            <div className="row" role="group" aria-label="Этап" style={{ gap: 6 }}>
              {STAGES.filter((s) => s.id !== 'deal').map((s) => (
                <button key={s.id} type="button" className="pill" aria-pressed={stage === s.id} onClick={() => setStage(s.id)}>{s.title}</button>
              ))}
            </div>
          </div>
          {list.length === 0 ? <p className="hint">На этапе «{STAGES.find((s) => s.id === stage).title}» никого нет.</p> : (
            <div className="recipients">
              {list.map((c) => (
                <label key={c.id} className="recipient">
                  <input type="checkbox" checked={picked.has(c.id)} onChange={() => toggle(c.id)} disabled={running} />
                  <Avatar chat={c} size={30} />
                  <span style={{ fontWeight: 500, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{c.title}</span>
                  <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--faint)' }}>{when(c.lastAt)}</span>
                </label>
              ))}
            </div>
          )}
          {count > left && st && <p className="hint" style={{ color: 'var(--warn-text)' }}>Сегодня можно ещё {left}. Остальным отправим завтра.</p>}
          <button type="button" className="btn btn-primary" style={{ alignSelf: 'flex-start' }}
            disabled={running || !count || !left || !tpl.trim() || tg.state !== 'ready'} onClick={start}>
            <Icon name="send" size={16} /> Отправить {Math.min(count, left)}
          </button>
          {tg.state !== 'ready' && <p className="hint">Сначала подключите Telegram.</p>}
        </section>
      </div>
    </div>
  )
}
