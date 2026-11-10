import { useCallback, useEffect, useState } from 'react'
import QRCode from 'qrcode'
import { api, on, errText, pluginOn } from '../api.js'
import { Icon } from '../components/ui.jsx'

// Страница плагина «Уведомления на телефон»: привязка телефона, о чём сообщать, как присылать.
const EVENTS = [
  ['evMsg', 'Клиент написал', 'Новое сообщение от клиента из воронки — Telegram и WhatsApp'],
  ['evFresh', 'Новый клиент', 'В воронке появился новый чат и клиент написал первым'],
  ['evReply', 'Ответил на рассылку', 'Клиент перешёл из «Написал» в «Ответил»'],
  ['evSilent', 'Клиент молчит 3 дня', 'Напоминание написать ещё раз'],
  ['evWait', 'Чек ждёт оплаты больше 5 дней', 'Нужен плагин «Чеки»'],
  ['evAlarm', 'CRM потеряла связь', 'Telegram или WhatsApp отключился — сообщения не приходят'],
]
const OPTIONS = [
  ['skipActive', 'Не присылать, пока я сам в CRM', 'Окно CRM открыто и активно — событие попадёт только в ленту приложения, без звука'],
  ['quiet', 'Тихие часы 23:00–08:00', 'Ночью без звука, утром одно уведомление со сводкой'],
  ['group', 'Несколько сообщений подряд — одним уведомлением', 'Клиент пишет по одной фразе — придёт одно, а не пять'],
]
const SHOW = ['Имя и текст сообщения', 'Только имя клиента', 'Только «новое сообщение»']
const DEFAULTS = { evMsg: true, evFresh: true, evReply: true, evSilent: false, evWait: false, evAlarm: true, show: SHOW[0], skipActive: true, quiet: true, group: true }

const hhmm = (unix) => new Date(unix * 1000).toLocaleString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
const mmss = (s) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`

export default function Notify({ toast, plugins }) {
  const [st, setSt] = useState(null)
  const [vals, setVals] = useState(null)
  const [qr, setQr] = useState('')
  const [left, setLeft] = useState(0)
  const [busy, setBusy] = useState('')

  const load = useCallback(() => api.notifyState().then(setSt).catch((e) => toast(errText(e), 'err')), [toast])
  useEffect(() => {
    load()
    api.pluginSettings('notify').then((v) => setVals({ ...DEFAULTS, ...v })).catch((e) => toast(errText(e), 'err'))
    return on('notify:changed', () => load())
  }, [load, toast])

  // QR-код и обратный отсчёт: код привязки живёт 5 минут
  useEffect(() => {
    if (!st?.pairing) { setQr(''); setLeft(0); return }
    QRCode.toDataURL(st.pairUrl, { width: 440, margin: 1, color: { dark: '#0D0E11', light: '#ECEDEF' } }).then(setQr)
    const tick = () => {
      const s = st.pairing.expiresAt - Math.floor(Date.now() / 1000)
      setLeft(Math.max(0, s))
      if (s <= 0) load()
    }
    tick()
    const t = setInterval(tick, 1000)
    return () => clearInterval(t)
  }, [st?.pairing?.code]) // eslint-disable-line react-hooks/exhaustive-deps

  async function run(kind, fn, ok) {
    setBusy(kind)
    try { const v = await fn(); if (v && v.devices) setSt(v); if (ok) toast(ok) } catch (e) { toast(errText(e), 'err') } finally { setBusy('') }
  }
  async function set(k, v) {
    const next = { ...vals, [k]: v }
    setVals(next)
    try { await api.savePluginSettings('notify', next) } catch (e) { toast(errText(e), 'err') }
  }
  function unpair(d) {
    if (!window.confirm(`Отвязать «${d.name}»? Уведомления на этот телефон перестанут приходить.`)) return
    run('unpair', () => api.notifyUnpair(d.id), 'Телефон отвязан')
  }
  function quit() {
    if (!window.confirm('Выйти из CRM? Пока она выключена, сообщения не подтягиваются и уведомления на телефон не приходят.')) return
    api.quitApp()
  }

  if (!st || !vals) return <div className="page"><p className="hint">Загрузка…</p></div>
  const linked = st.devices.length > 0
  const cheki = pluginOn(plugins, 'cheki')
  const addr = st.addrs.length ? st.addrs.map((a) => `${a}:${st.port}`).join('  ·  ') : ''

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>Уведомления на телефон</h1>
          <p>Своё приложение на Android: принимает сообщения клиентов в фоне и показывает их</p>
        </div>
      </div>

      <div className="ntf-grid">
        <section className={'panel ntf-phone' + (linked ? ' linked' : '')} aria-label="Телефон">
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <h2>Телефон</h2>
            <button type="button" className="btn btn-sm" onClick={() => api.openApkPage()}><Icon name="down" size={15} /> Скачать приложение (.apk)</button>
          </div>

          {st.devices.map((d) => (
            <div key={d.id} className="ntf-dev">
              <div className={'plug-ico' + (d.online ? ' on' : '')}><Icon name="phone" size={22} /></div>
              <div style={{ minWidth: 0, flex: '1 1 200px' }}>
                <div className="plug-name">{d.name}</div>
                <div className="plug-detail">
                  Привязан · {d.online ? 'на связи' : 'не на связи'}{!d.online && d.lastSeen ? ` · был ${hhmm(d.lastSeen)}` : ''}
                </div>
              </div>
              <div className="row" style={{ gap: 8 }}>
                <button type="button" className="btn btn-sm btn-inv" disabled={!!busy} onClick={() => run('test', () => api.notifyTest(), 'Тестовое уведомление отправлено — проверь телефон')}>Отправить тест</button>
                <button type="button" className="btn btn-sm btn-quiet" disabled={!!busy} onClick={() => unpair(d)}>Отвязать</button>
              </div>
            </div>
          ))}

          {st.pairing ? (
            <div className="ntf-pair">
              <div className="ntf-qr">{qr ? <img src={qr} alt="QR-код для привязки телефона" /> : null}</div>
              <div style={{ minWidth: 0, flex: '1 1 260px' }}>
                <ol className="ntf-steps">
                  <li>Установи приложение на телефон (кнопка «Скачать приложение»).</li>
                  <li>Открой его и наведи камеру на этот код.</li>
                  <li>Разреши уведомления — и телефон привязан.</li>
                </ol>
                <div className="kv" style={{ marginTop: 12 }}>
                  <div className="kv-row"><span className="k">Код вручную</span><span className="mono" style={{ fontWeight: 600, letterSpacing: '.08em' }}>{st.pairing.code.slice(0, 4)} {st.pairing.code.slice(4)}</span></div>
                  <div className="kv-row"><span className="k">Адрес</span><span className="mono" style={{ fontSize: 12, overflowWrap: 'anywhere', textAlign: 'right' }}>{addr || 'сеть не найдена'}</span></div>
                </div>
                <div className="row" style={{ marginTop: 12, gap: 10 }}>
                  <span className="hint" style={{ fontSize: 12 }}>Код действует ещё {mmss(left)}</span>
                  <button type="button" className="btn btn-sm btn-quiet" onClick={() => run('pair', () => api.notifyCancelPairing())}>Скрыть код</button>
                </div>
              </div>
            </div>
          ) : (
            <div className="row" style={{ gap: 12 }}>
              <button type="button" className={'btn ' + (linked ? '' : 'btn-primary')} disabled={!!busy || !st.running} onClick={() => run('pair', () => api.notifyStartPairing())}>
                {linked ? 'Привязать ещё телефон' : 'Привязать телефон'}
              </button>
              {!linked && <span className="hint" style={{ fontSize: 13 }}>Покажем QR-код — его сканирует приложение</span>}
            </div>
          )}

          <p className="hint" style={{ fontSize: 12, margin: 0 }}>
            Телефон подключается к этому компьютеру напрямую, без серверов посередине: компьютер должен быть включён, а телефон — в той же сети Wi-Fi
            (или видеть компьютер через VPN). Сообщения шифруются ключом, который есть только у этой CRM и у телефона.
          </p>
        </section>

        <section className="panel ntf-list" aria-label="О чём сообщать">
          <h2>О чём сообщать</h2>
          <div>
            {EVENTS.map(([k, label, hint]) => {
              const off = k === 'evWait' && !cheki
              return (
                <div key={k} className={'ntf-row' + (off ? ' off' : '')}>
                  <span><b>{label}</b><small>{off ? 'Включите плагин «Чеки»' : hint}</small></span>
                  <button type="button" role="switch" className="switch" aria-checked={!!vals[k] && !off} aria-label={label} disabled={off} onClick={() => set(k, !vals[k])}><span /></button>
                </div>
              )
            })}
          </div>
        </section>

        <section className="panel ntf-list" aria-label="Как присылать">
          <h2>Как присылать</h2>
          <div>
            <div className="ntf-row">
              <span><b>Что видно в уведомлении</b><small>На заблокированном экране можно скрыть текст и оставить только имя</small></span>
              <select className="input input-sm" style={{ width: 'auto', maxWidth: '100%' }} value={vals.show} aria-label="Что видно в уведомлении" onChange={(e) => set('show', e.target.value)}>
                {SHOW.map((o) => <option key={o} value={o}>{o}</option>)}
              </select>
            </div>
            {OPTIONS.map(([k, label, hint]) => (
              <div key={k} className="ntf-row">
                <span><b>{label}</b><small>{hint}</small></span>
                <button type="button" role="switch" className="switch" aria-checked={!!vals[k]} aria-label={label} onClick={() => set(k, !vals[k])}><span /></button>
              </div>
            ))}
          </div>
        </section>
      </div>

      <div className="ntf-bg">
        <span className="hint" style={{ fontSize: 13, flex: '1 1 320px' }}>
          Пока привязан телефон, крестик не выключает CRM, а прячет окно: она продолжает получать сообщения и слать уведомления.
          Открыть окно снова — запусти CRM ещё раз.
        </span>
        <button type="button" className="btn btn-sm" onClick={quit}>Выйти из CRM</button>
      </div>
    </div>
  )
}
