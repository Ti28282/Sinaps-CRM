import { useEffect, useState } from 'react'
import QRCode from 'qrcode'
import { api, on, errText } from '../api.js'
import { Icon, MailIcon, TgIcon, WaIcon } from '../components/ui.jsx'

const STATE_LABEL = {
  no_keys: ['Нужны ключи', 'chip-muted'],
  connecting: ['Подключение…', 'chip-muted'],
  need_phone: ['Ожидает входа', 'chip-muted'],
  need_code: ['Введите код', 'chip-muted'],
  need_qr: ['Сканируйте QR', 'chip-muted'],
  need_password: ['Нужен пароль 2FA', 'chip-muted'],
  ready: ['Подключён', 'chip-ok'],
  error: ['Ошибка', 'chip-muted'],
}

export default function Connect({ tg, wa, settings, reloadSettings, toast, done }) {
  const [label, cls] = STATE_LABEL[tg.state] || STATE_LABEL.no_keys
  return (
    <div className="page">
      <div className="narrow" style={{ display: 'flex', flexDirection: 'column', gap: 28 }}>
        <div>
          <div className="eyebrow" style={{ color: 'var(--accent-text)' }}>Подключения</div>
          <h1 style={{ margin: '10px 0 0', fontSize: 34, fontWeight: 600, letterSpacing: '-.02em' }}>Подключите каналы связи</h1>
          <p className="hint" style={{ marginTop: 10, maxWidth: 620, fontSize: 15 }}>
            CRM берёт только выбранное: папку Telegram и отмеченные вручную чаты WhatsApp. Личные переписки не попадают, база хранится у вас на компьютере.
          </p>
        </div>

        <div className="grid-2">
          <section className="panel">
            <div className="panel-head">
              <div className="panel-ico" style={{ background: 'var(--tg)' }}><TgIcon size={22} /></div>
              <div>
                <h2>Telegram</h2>
                <div className="hint">Личный аккаунт через MTProto</div>
              </div>
              <span className={'chip ' + cls} style={{ marginLeft: 'auto' }}>{label}</span>
            </div>
            {tg.state === 'error' && <p className="hint" style={{ color: 'var(--danger)' }}>{tg.error}</p>}
            <TgFlow tg={tg} settings={settings} reloadSettings={reloadSettings} toast={toast} done={done} />
          </section>

          <WaPanel wa={wa} toast={toast} />

          <section className="panel soon">
            <div className="panel-head">
              <div className="panel-ico" style={{ background: 'var(--mail)' }}><MailIcon size={22} /></div>
              <div>
                <h2>Gmail</h2>
                <div className="hint">Вход через Google · скоро</div>
              </div>
              <span className="chip chip-muted" style={{ marginLeft: 'auto' }}>Скоро</span>
            </div>
            <p className="hint">
              Письма от клиентов (по ярлыку, например «clients») появятся в той же воронке рядом с чатами, ответы — прямо из CRM.
            </p>
          </section>
        </div>

      </div>
    </div>
  )
}

function TgFlow({ tg, settings, reloadSettings, toast, done }) {
  const [apiId, setApiId] = useState('')
  const [apiHash, setApiHash] = useState('')
  const [phone, setPhone] = useState('')
  const [code, setCode] = useState('')
  const [pwd, setPwd] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => { if (settings?.tgApiId) setApiId(String(settings.tgApiId)) }, [settings?.tgApiId])

  const run = async (fn) => {
    setBusy(true)
    try { await fn() } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }

  if (tg.state === 'no_keys' || tg.state === 'error') {
    return (
      <form className="field" style={{ gap: 14 }} onSubmit={(e) => { e.preventDefault(); run(async () => { await api.tgSaveKeys(apiId, apiHash); reloadSettings() }) }}>
        <p className="hint">
          Получите ключи на <b>my.telegram.org</b> → API development tools → создайте приложение. Это ваши личные ключи, они остаются на компьютере.
        </p>
        <label className="field"><span>API ID</span><input className="input mono" inputMode="numeric" value={apiId} onChange={(e) => setApiId(e.target.value.replace(/\D/g, ''))} /></label>
        <label className="field"><span>API Hash{settings?.hasApiHash ? ' (сохранён — можно не вводить)' : ''}</span><input className="input mono" value={apiHash} onChange={(e) => setApiHash(e.target.value)} /></label>
        <button className="btn btn-primary" disabled={busy || !apiId} style={{ alignSelf: 'flex-start' }}>Подключиться <Icon name="arrow" size={16} /></button>
      </form>
    )
  }

  if (tg.state === 'connecting') return <p className="hint">Соединяемся с Telegram…</p>

  if (tg.state === 'need_qr') return <QRLogin tg={tg} />

  if (tg.state === 'need_phone') {
    return (
      <div className="field" style={{ gap: 18 }}>
        {tg.error && <p className="hint" style={{ color: 'var(--danger)' }}>{tg.error}</p>}
        <div className="field" style={{ gap: 10 }}>
          <p className="hint">Быстрее всего — по QR: код не нужен, подтверждаете на телефоне.</p>
          <button type="button" className="btn btn-primary" style={{ alignSelf: 'flex-start' }} disabled={busy} onClick={() => run(api.tgStartQR)}>Войти по QR-коду</button>
        </div>
        <form className="field" style={{ gap: 14, paddingTop: 18, borderTop: '1px solid var(--line)' }} onSubmit={(e) => { e.preventDefault(); run(() => api.tgSendCode(phone)) }}>
          <label className="field"><span>Или по номеру телефона</span><input className="input mono" placeholder="+996 555 123 456" value={phone} onChange={(e) => setPhone(e.target.value)} /></label>
          <div className="row">
            <button className="btn" disabled={busy || phone.length < 6}>Получить код</button>
            <button type="button" className="btn" onClick={() => run(api.tgLogout)}>Сменить ключи</button>
          </div>
        </form>
      </div>
    )
  }

  if (tg.state === 'need_code') {
    return <CodeForm tg={tg} code={code} setCode={setCode} busy={busy} run={run} />
  }

  if (tg.state === 'need_password') {
    return (
      <form className="field" style={{ gap: 14 }} onSubmit={(e) => { e.preventDefault(); run(() => api.tgPassword(pwd)) }}>
        <label className="field"><span>Облачный пароль (2FA)</span><input className="input" type="password" value={pwd} onChange={(e) => setPwd(e.target.value)} autoFocus /></label>
        <button className="btn btn-primary" disabled={busy || !pwd} style={{ alignSelf: 'flex-start' }}>Подтвердить</button>
      </form>
    )
  }

  return <Folders tg={tg} settings={settings} reloadSettings={reloadSettings} toast={toast} done={done} />
}

const VIA = {
  app: 'в приложение Telegram — чат «Telegram» на другом вашем устройстве (не SMS)',
  sms: 'по SMS',
  call: 'звонком — код будет в последних цифрах номера',
  email: 'на почту, привязанную к аккаунту',
  fragment: 'через Fragment',
  firebase: 'по SMS',
}
const NEXT = { sms: 'по SMS', call: 'звонком', fragment: 'через Fragment' }

function CodeForm({ tg, code, setCode, busy, run }) {
  const [left, setLeft] = useState(tg.timeout || 0)
  useEffect(() => {
    setLeft(tg.timeout || 0)
    const t = setInterval(() => setLeft((s) => Math.max(0, s - 1)), 1000)
    return () => clearInterval(t)
  }, [tg.timeout, tg.codeVia])

  return (
    <form className="field" style={{ gap: 14 }} onSubmit={(e) => { e.preventDefault(); run(() => api.tgSignIn(code)) }}>
      <p className="hint">Код отправлен на {tg.phone} {VIA[tg.codeVia] || ''}.</p>
      {tg.codeVia === 'app' && (
        <p className="hint">Если Telegram открыт только на этом номере и нигде больше — код не придёт по SMS сам. Войдите по QR или запросите повторно.</p>
      )}
      <label className="field"><span>Код</span><input className="input mono" inputMode="numeric" value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} autoFocus /></label>
      <div className="row">
        <button className="btn btn-primary" disabled={busy || code.length < 4}>Войти</button>
        {tg.nextVia && (
          <button type="button" className="btn" disabled={busy || left > 0} onClick={() => run(api.tgResend)}>
            {left > 0 ? `Отправить ${NEXT[tg.nextVia]} через ${left} с` : `Отправить ${NEXT[tg.nextVia]}`}
          </button>
        )}
        <button type="button" className="btn" disabled={busy} onClick={() => run(api.tgStartQR)}>Войти по QR</button>
      </div>
    </form>
  )
}

function QRLogin({ tg }) {
  const [img, setImg] = useState('')
  useEffect(() => {
    if (!tg.qrUrl) return
    QRCode.toDataURL(tg.qrUrl, { width: 440, margin: 1, color: { dark: '#0D0E11', light: '#ECEDEF' } }).then(setImg)
  }, [tg.qrUrl])

  return (
    <div className="row" style={{ gap: 24, alignItems: 'center' }}>
      <div style={{ width: 220, height: 220, borderRadius: 18, background: '#ECEDEF', padding: 10, flexShrink: 0 }}>
        {img && <img src={img} alt="QR-код для входа в Telegram" style={{ width: '100%', height: '100%', display: 'block' }} />}
      </div>
      <div className="field" style={{ flex: '1 1 200px', gap: 14 }}>
        <ol style={{ margin: 0, paddingLeft: 18, display: 'flex', flexDirection: 'column', gap: 8, lineHeight: 1.5, color: 'var(--text-2)' }}>
          <li>Откройте Telegram на телефоне</li>
          <li>Настройки → Устройства → Подключить устройство</li>
          <li>Наведите камеру на код</li>
        </ol>
        <p className="hint">Код обновляется сам каждые 30 секунд.</p>
        <button type="button" className="btn" style={{ alignSelf: 'flex-start' }} onClick={() => api.tgCancelQR()}>Отмена</button>
      </div>
    </div>
  )
}

function Folders({ tg, settings, reloadSettings, toast, done }) {
  const [folders, setFolders] = useState(null)
  const [sel, setSel] = useState(settings?.tgFolderId || 0)
  const [progress, setProgress] = useState(null)
  const [cleaning, setCleaning] = useState(null)

  const loadFolders = () => api.tgFolders().then(setFolders).catch((e) => toast(errText(e), 'err'))

  useEffect(() => {
    api.tgFolders().then((f) => {
      setFolders(f)
      if (!sel) {
        const guess = f.find((x) => /client|клиент/i.test(x.title))
        if (guess) setSel(guess.id)
      }
    }).catch((e) => toast(errText(e), 'err'))
    return on('sync:progress', setProgress)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  async function sync() {
    const f = folders.find((x) => x.id === sel)
    setProgress({ done: 0, total: f?.count || 0 })
    try {
      const r = await api.syncFolder(sel, f?.title || '')
      toast(`Готово: ${r.chats} чатов (новых ${r.newChats}), сообщений ${r.newMessages}${r.skipped ? `, пропущено ${r.skipped}` : ''}`)
      reloadSettings()
      done()
    } catch (e) { toast(errText(e), 'err') } finally { setProgress(null) }
  }

  const picked = folders?.find((x) => x.id === sel)

  return (
    <>
      <div className="account">
        <div className="avatar" style={{ width: 36, height: 36, background: 'var(--accent)', fontSize: 13 }}>{(tg.name || '?')[0]}</div>
        <div>
          <div style={{ fontWeight: 600 }}>{tg.username ? '@' + tg.username : tg.name}</div>
          <div className="hint" style={{ fontSize: 12 }}>Сессия активна на этом компьютере</div>
        </div>
        <button type="button" className="btn btn-sm" style={{ marginLeft: 'auto' }} onClick={() => api.tgLogout().catch((e) => toast(errText(e), 'err'))}>Выйти</button>
      </div>

      <fieldset style={{ margin: 0, padding: 0, border: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
        <legend className="eyebrow" style={{ marginBottom: 10 }}>Папка с клиентами</legend>
        {!folders && <p className="hint">Загружаем папки…</p>}
        {folders?.length === 0 && <p className="hint">Папок нет. Создайте в Telegram папку «clients» и добавьте туда чаты клиентов.</p>}
        <div className="folder-list">
        {folders?.map((f) => (
          <div key={f.id} className="folder-row">
            <button type="button" className="option" aria-pressed={f.id === sel} onClick={() => setSel(f.id)}>
              <span className="radio" aria-hidden="true" />
              <Icon name="folder" size={16} />
              <span style={{ fontWeight: 500 }}>{f.title}</span>
              <span className="mono" style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--muted-2)' }}>{f.hasRules ? 'по правилам' : `${f.count} чатов`}</span>
            </button>
            <button type="button" className="icon-btn folder-clean" title="Убрать чаты из CRM (Telegram не меняется)" aria-label={`Чаты папки ${f.title} в CRM`} onClick={() => setCleaning(f)}>
              <Icon name="hide" size={18} />
            </button>
          </div>
        ))}
        </div>
      </fieldset>

      {progress && (
        <div className="field">
          <span>Импорт: {progress.done} из {progress.total}</span>
          <div className="progress"><div style={{ width: `${progress.total ? (progress.done / progress.total) * 100 : 0}%` }} /></div>
        </div>
      )}

      <button type="button" className="btn btn-primary" disabled={!sel || !!progress} onClick={sync} style={{ alignSelf: 'flex-start' }}>
        {settings?.tgFolderId === sel ? 'Синхронизировать' : `Импортировать${picked && !picked.hasRules ? ` ${picked.count} чатов` : ''}`}
        <Icon name="arrow" size={16} />
      </button>

      {cleaning && (
        <FolderClean folder={cleaning} toast={toast} close={() => setCleaning(null)}
          onDone={() => loadFolders()} />
      )}
    </>
  )
}


// Окно «Убрать из CRM»: только локально. Папка в Telegram НЕ меняется,
// скрытые чаты просто не импортируются при синхронизации.
function FolderClean({ folder, toast, close, onDone }) {
  const [chats, setChats] = useState(null)
  const [picked, setPicked] = useState(new Set()) // по умолчанию ничего не выбрано
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)

  const load = () => api.folderChats(folder.id).then(setChats).catch((e) => { toast(errText(e), 'err'); close() })

  useEffect(() => {
    load()
    const onKey = (e) => e.key === 'Escape' && close()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const s = q.trim().toLowerCase()
  const visible = (chats || []).filter((c) => !s || c.title.toLowerCase().includes(s))
  const allVisible = visible.length > 0 && visible.every((c) => picked.has(c.extId))
  const toggle = (id) => setPicked((p) => { const n = new Set(p); n.has(id) ? n.delete(id) : n.add(id); return n })
  const toggleAll = () => setPicked((p) => {
    const n = new Set(p)
    visible.forEach((c) => (allVisible ? n.delete(c.extId) : n.add(c.extId)))
    return n
  })
  const sel = (chats || []).filter((c) => picked.has(c.extId))
  const toHide = sel.filter((c) => !c.hidden)
  const toShow = sel.filter((c) => c.hidden)

  async function hide() {
    if (!window.confirm(`Убрать ${toHide.length} чатов из CRM? Переписка в CRM удалится, при синхронизации они больше не подтянутся. В Telegram и в папке «${folder.title}» ничего не изменится.`)) return
    setBusy(true)
    try {
      const n = await api.hideChats(Object.fromEntries(toHide.map((c) => [c.extId, c.title])))
      toast(`Убрано из CRM: ${toHide.length}${n !== toHide.length ? ` (было в CRM: ${n})` : ''}. Папка в Telegram не изменена`)
      setPicked(new Set()); await load(); onDone()
    } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }

  async function unhide() {
    setBusy(true)
    try {
      await api.unhideChats(toShow.map((c) => c.extId))
      toast(`Вернули ${toShow.length}. Подтянутся при следующей синхронизации`)
      setPicked(new Set()); await load(); onDone()
    } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }

  return (
    <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="fc-title">
        <div className="modal-head">
          <div>
            <h2 id="fc-title">Чаты папки «{folder.title}» в CRM</h2>
            <p className="hint" style={{ marginTop: 4 }}>Меняется только CRM. Папка и чаты в Telegram остаются как есть.</p>
          </div>
          <button type="button" className="icon-btn" aria-label="Закрыть" onClick={close}><Icon name="close" /></button>
        </div>

        {!chats ? <p className="hint">Загружаем чаты папки…</p> : (
          <>
            <div className="row" style={{ justifyContent: 'space-between' }}>
              <label className="search" style={{ flex: '1 1 240px' }}>
                <Icon name="search" size={16} />
                <span className="sr">Поиск по чатам папки</span>
                <input type="search" placeholder="Поиск" value={q} onChange={(e) => setQ(e.target.value)} />
              </label>
              <label className="check">
                <input type="checkbox" checked={allVisible} onChange={toggleAll} />
                Выбрать все{s ? ' найденные' : ''}
              </label>
            </div>

            <div className="recipients" style={{ maxHeight: 360 }}>
              {visible.map((c) => (
                <label key={c.extId} className="recipient" style={c.hidden ? { opacity: 0.6 } : undefined}>
                  <input type="checkbox" checked={picked.has(c.extId)} onChange={() => toggle(c.extId)} />
                  <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontWeight: 500 }}>{c.title}</span>
                  <span style={{ marginLeft: 'auto' }}>
                    {c.hidden ? <span className="chip chip-muted">скрыт</span> : c.inCrm ? <span className="chip chip-ok">в CRM</span> : <span className="chip chip-muted">не импортирован</span>}
                  </span>
                </label>
              ))}
              {visible.length === 0 && <p className="hint" style={{ padding: 10 }}>{chats.length ? 'Ничего не найдено' : 'В папке нет чатов'}</p>}
            </div>

            <div className="row" style={{ justifyContent: 'flex-end' }}>
              <button type="button" className="btn" onClick={close}>Закрыть</button>
              {toShow.length > 0 && (
                <button type="button" className="btn" disabled={busy} onClick={unhide}>Вернуть {toShow.length}</button>
              )}
              <button type="button" className="btn btn-primary" disabled={busy || toHide.length === 0} onClick={hide}>
                {busy ? 'Сохраняем…' : `Убрать из CRM ${toHide.length || ''}`}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}


const WA_LABEL = {
  off: ['Не подключён', 'chip-muted'], connecting: ['Подключение…', 'chip-muted'],
  need_qr: ['Сканируйте QR', 'chip-muted'], ready: ['Подключён', 'chip-ok'], error: ['Ошибка', 'chip-muted'],
}

function WaPanel({ wa, toast }) {
  const [img, setImg] = useState('')
  const [busy, setBusy] = useState(false)
  const [picker, setPicker] = useState(false)
  const [label, cls] = WA_LABEL[wa.state] || WA_LABEL.off

  useEffect(() => {
    if (!wa.qr) return setImg('')
    QRCode.toDataURL(wa.qr, { width: 440, margin: 1, color: { dark: '#0D0E11', light: '#ECEDEF' } }).then(setImg)
  }, [wa.qr])

  const run = async (fn) => { setBusy(true); try { await fn() } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) } }

  return (
    <section className="panel">
      <div className="panel-head">
        <div className="panel-ico" style={{ background: 'var(--wa)' }}><WaIcon size={22} /></div>
        <div>
          <h2>WhatsApp</h2>
          <div className="hint">Связанное устройство, вход по QR</div>
        </div>
        <span className={'chip ' + cls} style={{ marginLeft: 'auto' }}>{label}</span>
      </div>
      {wa.error && <p className="hint" style={{ color: 'var(--danger)' }}>{wa.error}</p>}

      {(wa.state === 'off' || wa.state === 'error') && (
        <>
          <p className="hint">
            CRM станет «связанным устройством», как WhatsApp Web. Сообщения уходят только по кнопке «Отправить»,
            «прочитано» не ставится, рассылок через WhatsApp нет. Подключение неофициальное — риск блокировки номера ваш.
          </p>
          <button type="button" className="btn btn-primary" style={{ alignSelf: 'flex-start' }} disabled={busy} onClick={() => run(api.waConnect)}>Подключить по QR</button>
        </>
      )}

      {wa.state === 'connecting' && <p className="hint">Соединяемся с WhatsApp…</p>}

      {wa.state === 'need_qr' && (
        <div className="row" style={{ gap: 24, alignItems: 'center' }}>
          <div style={{ width: 220, height: 220, borderRadius: 18, background: '#ECEDEF', padding: 10, flexShrink: 0 }}>
            {img && <img src={img} alt="QR-код для входа в WhatsApp" style={{ width: '100%', height: '100%', display: 'block' }} />}
          </div>
          <div className="field" style={{ flex: '1 1 200px', gap: 14 }}>
            <ol style={{ margin: 0, paddingLeft: 18, display: 'flex', flexDirection: 'column', gap: 8, lineHeight: 1.5, color: 'var(--text-2)' }}>
              <li>Откройте WhatsApp на телефоне с нужным номером</li>
              <li>Настройки → Связанные устройства → Привязка устройства</li>
              <li>Наведите камеру на код</li>
            </ol>
            <p className="hint">Код обновляется сам.</p>
            <button type="button" className="btn" style={{ alignSelf: 'flex-start' }} onClick={() => run(api.waLogout)}>Отмена</button>
          </div>
        </div>
      )}

      {wa.state === 'ready' && (
        <>
          <div className="account">
            <div className="avatar" style={{ width: 36, height: 36, background: '#7FE0AE', fontSize: 13 }}>{(wa.name || '?')[0]}</div>
            <div>
              <div style={{ fontWeight: 600 }}>{wa.name || 'WhatsApp'} <span className="hint mono" style={{ fontSize: 12 }}>{wa.phone}</span></div>
              <div className="hint" style={{ fontSize: 12 }}>{wa.syncing ? 'Телефон присылает историю чатов…' : `Известно личных чатов: ${wa.known}`}</div>
            </div>
            <button type="button" className="btn btn-sm" style={{ marginLeft: 'auto' }} disabled={busy}
              onClick={() => window.confirm('Отвязать CRM от WhatsApp? Переписка в CRM останется.') && run(api.waLogout)}>Отвязать</button>
          </div>
          <button type="button" className="btn btn-primary" style={{ alignSelf: 'flex-start' }} onClick={() => setPicker(true)}>
            Выбрать чаты клиентов <Icon name="arrow" size={16} />
          </button>
          {wa.known === 0 && !wa.syncing && <p className="hint">Чаты появятся, когда телефон пришлёт историю (обычно 1–2 минуты после первого входа) или когда кто-то напишет.</p>}
        </>
      )}

      {picker && <WaPicker toast={toast} close={() => setPicker(false)} />}
    </section>
  )
}

// Выбор чатов WhatsApp для CRM. Меняется только CRM, в WhatsApp ничего не происходит.
function WaPicker({ toast, close }) {
  const [list, setList] = useState(null)
  const [picked, setPicked] = useState(new Set())
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)

  const load = () => api.waKnownChats().then(setList).catch((e) => toast(errText(e), 'err'))
  useEffect(() => {
    load()
    const off = on('wa:known', load)
    const onKey = (e) => e.key === 'Escape' && close()
    window.addEventListener('keydown', onKey)
    return () => { off && off(); window.removeEventListener('keydown', onKey) }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const s = q.trim().toLowerCase()
  const visible = (list || []).filter((c) => !s || (c.name + ' ' + c.jid).toLowerCase().includes(s))
  const toggle = (id) => setPicked((p) => { const n = new Set(p); n.has(id) ? n.delete(id) : n.add(id); return n })
  const sel = (list || []).filter((c) => picked.has(c.jid))
  const toAdd = sel.filter((c) => !c.inCrm)
  const toRemove = sel.filter((c) => c.inCrm)

  async function add() {
    setBusy(true)
    try {
      const r = await api.waAddChats(toAdd.map((c) => c.jid))
      toast(`Добавлено в CRM: ${toAdd.length}, сообщений из истории: ${r.messages}`)
      setPicked(new Set()); load()
    } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }
  async function remove() {
    if (!window.confirm(`Убрать ${toRemove.length} чатов из CRM? В WhatsApp ничего не изменится.`)) return
    setBusy(true)
    try {
      await api.waHideChats(Object.fromEntries(toRemove.map((c) => [c.jid, c.name])))
      toast(`Убрано из CRM: ${toRemove.length}`)
      setPicked(new Set()); load()
    } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }

  return (
    <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="wa-title">
        <div className="modal-head">
          <div>
            <h2 id="wa-title">Чаты WhatsApp в CRM</h2>
            <p className="hint" style={{ marginTop: 4 }}>Отметьте клиентов. Меняется только CRM — в WhatsApp ничего не происходит.</p>
          </div>
          <button type="button" className="icon-btn" aria-label="Закрыть" onClick={close}><Icon name="close" /></button>
        </div>
        {!list ? <p className="hint">Загружаем…</p> : (
          <>
            <label className="search" style={{ width: '100%' }}>
              <Icon name="search" size={16} />
              <span className="sr">Поиск по чатам WhatsApp</span>
              <input type="search" placeholder="Имя или номер" value={q} onChange={(e) => setQ(e.target.value)} />
            </label>
            <div className="recipients" style={{ maxHeight: 380 }}>
              {visible.map((c) => (
                <label key={c.jid} className="recipient">
                  <input type="checkbox" checked={picked.has(c.jid)} onChange={() => toggle(c.jid)} />
                  <span style={{ minWidth: 0, flex: 1 }}>
                    <span style={{ display: 'block', fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{c.name}</span>
                    <span className="hint" style={{ display: 'block', fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      +{c.jid.split('@')[0]}{c.lastMsg ? ` · ${c.lastMsg}` : ''}
                    </span>
                  </span>
                  {c.inCrm ? <span className="chip chip-ok">в CRM</span> : c.hidden ? <span className="chip chip-muted">скрыт</span> : null}
                </label>
              ))}
              {visible.length === 0 && <p className="hint" style={{ padding: 10 }}>{list.length ? 'Ничего не найдено' : 'Пока нет чатов — подождите, пока телефон пришлёт историю'}</p>}
            </div>
            <div className="row" style={{ justifyContent: 'flex-end' }}>
              <button type="button" className="btn" onClick={close}>Закрыть</button>
              {toRemove.length > 0 && <button type="button" className="btn" disabled={busy} onClick={remove}>Убрать из CRM {toRemove.length}</button>}
              <button type="button" className="btn btn-primary" disabled={busy || toAdd.length === 0} onClick={add}>Добавить в CRM {toAdd.length || ''}</button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
