import { useCallback, useEffect, useRef, useState } from 'react'
import { api, on, isDemo, errText } from './api.js'
import { Icon, TgIcon, WaIcon } from './components/ui.jsx'
import Pipeline from './pages/Pipeline.jsx'
import Chats from './pages/Chats.jsx'
import Broadcast from './pages/Broadcast.jsx'
import Connect from './pages/Connect.jsx'
import Settings from './pages/Settings.jsx'

const NAV = [
  { id: 'pipeline', label: 'Воронка', icon: 'board' },
  { id: 'chats', label: 'Диалоги', icon: 'chat' },
  { id: 'broadcast', label: 'Рассылки', icon: 'send' },
  { id: 'connect', label: 'Подключения', icon: 'link' },
  { id: 'settings', label: 'Настройки', icon: 'settings' },
]

export default function App() {
  const [page, setPage] = useState('pipeline')
  const [openChat, setOpenChat] = useState(null)
  const [chats, setChats] = useState([])
  const [tg, setTg] = useState({ state: 'no_keys' })
  const [settings, setSettings] = useState(null)
  const [toasts, setToasts] = useState([])
  const tid = useRef(0)

  const toast = useCallback((text, kind = 'ok') => {
    const id = ++tid.current
    setToasts((t) => [...t, { id, text, kind }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), kind === 'err' ? 6000 : 3500)
  }, [])

  const reload = useCallback(() => api.chats().then(setChats).catch((e) => toast(errText(e), 'err')), [toast])
  const reloadSettings = useCallback(() => api.settings().then(setSettings), [])

  useEffect(() => {
    reload()
    reloadSettings()
    api.tgStatus().then(setTg)
    const offs = [
      on('tg:status', setTg),
      on('chats:changed', reload),
      on('sync:warn', (m) => toast(m, 'err')),
      on('sync:done', (r) => r?.newMessages && toast(`Подтянуто новых сообщений: ${r.newMessages}`)),
      on('sync:suggest', () => toast('Было много обновлений — нажмите «Синхронизировать»')),
    ]
    return () => offs.forEach((off) => off && off())
  }, [reload, reloadSettings, toast])

  // когда ещё ничего не подключено — сразу на экран подключения
  useEffect(() => {
    if (settings && !settings.tgFolderId && chats.length === 0) setPage('connect')
  }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps

  const unread = chats.filter((c) => c.unread).length
  const tgCount = chats.filter((c) => c.channel === 'tg').length
  const go = (p, chatId) => { setPage(p); if (chatId !== undefined) setOpenChat(chatId) }

  return (
    <div className="shell">
      {isDemo && <div className="demo-badge">Демо-режим: запущено без Go-бэкенда</div>}
      <aside className="side">
        <div className="brand">
          <div className="brand-mark">S</div>
          <div className="brand-name">SINAPS <span>CRM</span></div>
        </div>
        <nav className="nav" aria-label="Основное">
          {NAV.map((n) => (
            <button key={n.id} type="button" aria-current={page === n.id ? 'page' : undefined} onClick={() => go(n.id)}>
              <Icon name={n.icon} />
              {n.label}
              {n.id === 'chats' && unread > 0 && <span className="count">{unread}</span>}
            </button>
          ))}
        </nav>
        <div className="sources">
          <div className="eyebrow">Источники</div>
          <div className="source">
            <div className="source-ico" style={{ background: 'var(--tg)' }}><TgIcon /></div>
            <div style={{ minWidth: 0 }}>
              <div className="source-name">Telegram</div>
              <div className="source-sub">
                {tg.state === 'ready'
                  ? (settings?.tgFolderName ? `Папка «${settings.tgFolderName}» · ${tgCount}` : 'Папка не выбрана')
                  : 'Не подключён'}
              </div>
            </div>
            <span className="dot" role="img" aria-label={tg.state === 'ready' ? 'Подключён' : 'Не подключён'}
              style={{ background: tg.state === 'ready' ? 'var(--accent)' : 'var(--off)' }} />
          </div>
          <div className="source soon">
            <div className="source-ico" style={{ background: 'var(--wa)' }}><WaIcon /></div>
            <div>
              <div className="source-name">WhatsApp</div>
              <div className="source-sub">Скоро</div>
            </div>
          </div>
        </div>
      </aside>

      <main className="main">
        {page === 'pipeline' && <Pipeline chats={chats} tg={tg} toast={toast} openChat={(id) => go('chats', id)} />}
        {page === 'chats' && <Chats chats={chats} openId={openChat} setOpenId={setOpenChat} toast={toast} tg={tg} />}
        {page === 'broadcast' && <Broadcast chats={chats} settings={settings} reloadSettings={reloadSettings} tg={tg} toast={toast} />}
        {page === 'settings' && <Settings settings={settings} reloadSettings={reloadSettings} toast={toast} />}
        {page === 'connect' && <Connect tg={tg} settings={settings} reloadSettings={reloadSettings} toast={toast} done={() => go('pipeline')} />}
      </main>

      <div className="toasts" aria-live="polite">
        {toasts.map((t) => <div key={t.id} className={'toast ' + t.kind}>{t.text}</div>)}
      </div>
    </div>
  )
}
