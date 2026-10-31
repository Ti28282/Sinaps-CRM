// Демо-данные для запуска фронта в браузере без Go (npm run dev).
const listeners = {}
const emit = (ev, data) => (listeners[ev] || []).forEach((cb) => cb(data))
export function mockOn(ev, cb) {
  ;(listeners[ev] ||= []).push(cb)
  return () => (listeners[ev] = listeners[ev].filter((x) => x !== cb))
}

const now = Math.floor(Date.now() / 1000)
const h = 3600
let status = { state: 'no_keys' }
let nextMsg = 1000

const chats = [
  ['Барбершоп «Борода»', 'new', 0, 1, 'Добрый день! А бот для записи сколько стоит и как быстро сделаете?', 0.5],
  ['Стоматология «Улыбка+»', 'new', 0, 1, 'Интересно. Пришлите примеры работ, пожалуйста', 2],
  ['Автосервис «Гараж 24»', 'new', 0, 0, 'Можно созвониться завтра после обеда?', 26],
  ['Школа английского Lingvo', 'sent', 0, 0, 'Здравствуйте! Сделал демо бота записи на пробный урок, показать?', 28, true],
  ['Фитнес-клуб Pulse', 'replied', 60000, 1, 'А интеграция с 1С возможна? Нам важно, чтобы абонементы подтягивались', 4],
  ['Кофейня «Зерно»', 'replied', 35000, 0, 'Давайте обсудим детали в понедельник', 70],
  ['Клиника «Мед-Лайн»', 'deal', 80000, 0, 'Счёт получили, оплатим сегодня до вечера', 6],
  ['Агентство Rent', 'lost', 0, 0, 'Спасибо, пока не актуально', 300],
].map(([title, stage, amount, unread, last, ago, out], i) => ({
  id: i + 1, channel: 'tg', extId: 'u' + i, kind: 'user', title, username: '', phone: '',
  stage, amount, nextStep: '', note: '', tags: '', unread: !!unread, lastMsg: last,
  lastAt: now - Math.round(ago * h), lastOut: !!out, createdAt: now - 10 * 24 * h,
}))

const messages = {}
chats.forEach((c) => {
  messages[c.id] = [
    { id: 1, chatId: c.id, msgId: 1, out: true, date: c.lastAt - 3 * h, text: 'Здравствуйте! Меня зовут Тимур, делаю ботов для записи клиентов. Могу показать демо за 2 минуты.' },
    { id: 2, chatId: c.id, msgId: 2, out: !!c.lastOut, date: c.lastAt, text: c.lastMsg },
  ]
})
const demoPhoto = 'data:image/svg+xml;utf8,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="640" height="420"><rect width="640" height="420" fill="#2B2F38"/><circle cx="470" cy="120" r="54" fill="#F5B65C"/><path d="M0 420 L210 190 L360 330 L450 250 L640 420Z" fill="#4A5160"/></svg>')
const fp = chats.find((c) => c.title.startsWith('Фитнес'))
messages[fp.id].splice(1, 0, { id: 500, chatId: fp.id, msgId: 500, out: false, date: fp.lastAt - 600, text: 'Вот так сейчас выглядит наше расписание', mediaKind: 'photo', mediaUrl: demoPhoto })
messages[fp.id][0].deleted = false
messages[fp.id].push({ id: 502, chatId: fp.id, msgId: 502, out: false, date: fp.lastAt + 60, text: '', mediaKind: 'file', fileName: 'Прайс_абонементы_2026.xlsx', fileSize: 48_300 })
messages[fp.id].splice(1, 0, { id: 501, chatId: fp.id, msgId: 501, out: false, date: fp.lastAt - 1200, text: 'Ой, не туда отправил', deleted: true })
const events = {}
let savedTheme = 'dark'
try { savedTheme = localStorage.getItem('sinaps-theme') || 'dark' } catch {}
const hiddenExt = new Set()
const appConfig = { theme: savedTheme, plugins: {} }

const find = (id) => chats.find((c) => c.id === id)
const wait = (ms = 250) => new Promise((r) => setTimeout(r, ms))
let bc = { running: false, total: 0, done: 0, failed: 0, current: '', nextAt: 0, lastError: '' }
let sentToday = 3
let settings = { tgApiId: 0, hasApiHash: false, tgFolderId: 0, tgFolderName: '', historyLimit: 200, autoSync: true, dailyLimit: 20, template: '{name}, здравствуйте! Это Тимур, SINAPS. Делаю ботов и ИИ-автоматизацию для бизнеса: запись клиентов, ответы 24/7, заявки сразу в таблицу. Если есть что автоматизировать — подскажу бесплатно. Актуально?' }
const setStatus = (s) => { status = s; emit('tg:status', s) }

export const mock = {
  GetInfo: async () => ({ version: '0.1.0-demo', dataDir: '~/.config/SINAPS-CRM' }),
  OpenDataDir: async () => {},
  GetSettings: async () => ({ ...settings }),
  SaveSettings: async (p) => { Object.assign(settings, Object.fromEntries(Object.entries(p).filter(([, v]) => v != null))) },

  GetConfig: async () => ({ ...appConfig }),
  SetTheme: async (theme) => { appConfig.theme = theme === 'light' ? 'light' : 'dark'; try { localStorage.setItem('sinaps-theme', appConfig.theme) } catch {} ; return { ...appConfig } },
  ListPlugins: async () => [],
  SetPluginEnabled: async () => { throw new Error('плагин не найден') },
  SavePluginSettings: async () => {},
  TgStatus: async () => status,
  TgSaveKeys: async (id) => { settings.tgApiId = id; settings.hasApiHash = true; setStatus({ state: 'connecting' }); await wait(600); setStatus({ state: 'need_phone' }) },
  TgSendCode: async (phone) => { await wait(); setStatus({ state: 'need_code', phone, codeVia: 'app', nextVia: 'sms', timeout: 30 }) },
  TgResendCode: async () => { await wait(); setStatus({ ...status, codeVia: 'sms', nextVia: 'call', timeout: 60 }) },
  TgStartQR: async () => {
    setStatus({ state: 'need_qr', qrUrl: 'tg://login?token=demo' + Date.now(), qrExpire: Math.floor(Date.now() / 1000) + 30 })
    setTimeout(() => status.state === 'need_qr' && setStatus({ state: 'ready', name: 'Тимур', username: 'TimurDevel' }), 8000)
  },
  TgCancelQR: async () => setStatus({ state: 'need_phone' }),
  TgSignIn: async () => { await wait(); setStatus({ state: 'ready', name: 'Тимур', username: 'TimurDevel', phone: '+996…' }) },
  TgPassword: async () => { await wait(); setStatus({ state: 'ready', name: 'Тимур', username: 'TimurDevel' }) },
  TgLogout: async () => setStatus({ state: 'no_keys' }),
  TgFolders: async () => [
    { id: 2, title: 'clients', count: chats.length, hasRules: false },
    { id: 3, title: 'Работа', count: 23, hasRules: false },
    { id: 4, title: 'Личное', count: 17, hasRules: true },
  ],
  SyncFolder: async (id, name) => {
    settings.tgFolderId = id; settings.tgFolderName = name
    for (let i = 0; i <= chats.length; i++) { emit('sync:progress', { done: i, total: chats.length }); await wait(120) }
    emit('chats:changed')
    return { chats: chats.length, newChats: 0, newMessages: 0, skipped: 0 }
  },
  FolderChats: async (id) => {
    await wait(300)
    if (id === 2) return [...chats.map((c) => ({ extId: c.extId, title: c.title, byRule: false, inCrm: true, hidden: false })),
      ...[...hiddenExt].map((e) => ({ extId: e, title: 'Скрытый чат ' + e, byRule: false, inCrm: false, hidden: true }))]
    return Array.from({ length: id === 4 ? 17 : 23 }, (_, i) => ({ extId: 'x' + id + '_' + i, title: (id === 4 ? 'Контакт ' : 'Чат ') + (i + 1), byRule: id === 4 }))
  },
  HideChats: async (m) => {
    await wait(300); let n = 0
    Object.keys(m).forEach((e) => { hiddenExt.add(e); const i = chats.findIndex((c) => c.extId === e); if (i >= 0) { chats.splice(i, 1); n++ } })
    emit('chats:changed'); return n
  },
  UnhideChats: async (ids) => { ids.forEach((e) => hiddenExt.delete(e)) },
  Resync: async () => mock.SyncFolder(settings.tgFolderId || 2, settings.tgFolderName || 'clients'),

  ListChats: async (channel, search) => {
    const q = (search || '').toLowerCase()
    return chats
      .filter((c) => channel === 'all' || !channel || c.channel === channel)
      .filter((c) => !q || (c.title + c.lastMsg + c.note).toLowerCase().includes(q))
      .sort((a, b) => b.lastAt - a.lastAt)
      .map((c) => ({ ...c }))
  },
  GetChat: async (id) => ({ chat: { ...find(id) }, messages: [...(messages[id] || [])], events: [...(events[id] || []), { kind: 'import', text: 'Импорт из Telegram', at: find(id).createdAt }] }),
  UpdateChat: async (id, p) => {
    const c = find(id)
    if (p.stage && p.stage !== c.stage) (events[id] ||= []).unshift({ kind: 'stage', text: 'Этап изменён', at: Math.floor(Date.now() / 1000) })
    Object.entries(p).forEach(([k, v]) => v != null && (c[k] = v))
    emit('chats:changed')
  },
  MarkRead: async (id) => { find(id).unread = false; emit('chats:changed') },
  DeleteChat: async (id) => { chats.splice(chats.indexOf(find(id)), 1); emit('chats:changed') },
  SendMessage: async (id, text) => {
    await wait(200)
    const c = find(id)
    const m = { id: ++nextMsg, chatId: id, msgId: nextMsg, out: true, text, date: Math.floor(Date.now() / 1000) }
    messages[id].push(m)
    Object.assign(c, { lastMsg: text, lastAt: m.date, lastOut: true, unread: false })
    if (c.stage === 'new') c.stage = 'sent'
    emit('message:new', m); emit('chats:changed')
  },
  DeleteMessage: async (chatId, msgId) => {
    messages[chatId] = messages[chatId].filter((m) => m.msgId !== msgId)
    emit('message:deleted', { chatId, removed: [msgId], marked: [] }); emit('chats:changed')
  },
  PickFile: async () => (Math.random() > 0.5 ? '/home/timur/Документы/КП SINAPS.pdf' : '/home/timur/Изображения/скрин.png'),
  FileInfo: async (path) => { const name = path.split('/').pop(); return { path, name, size: name.endsWith('.pdf') ? 2_450_000 : 830_000, isImage: /\.(png|jpe?g|webp)$/i.test(name), tooBig: false } },
  SendFile: async (chatId, path, caption, asPhoto) => {
    const total = 2_000_000
    for (let d = 0; d <= total; d += 400_000) { emit('upload:progress', { chatId, done: d, total }); await wait(200) }
    const name = path.split('/').pop()
    const m = asPhoto
      ? { id: ++nextMsg, chatId, msgId: nextMsg, out: true, text: caption, date: Math.floor(Date.now() / 1000), mediaKind: 'photo', mediaUrl: demoPhoto }
      : { id: ++nextMsg, chatId, msgId: nextMsg, out: true, text: caption, date: Math.floor(Date.now() / 1000), mediaKind: 'file', fileName: name, fileSize: 2_450_000 }
    messages[chatId].push(m); emit('message:new', m); emit('chats:changed')
  },
  OpenAttachment: async () => { await wait(600) },
  Stats: async () => {
    const s = { total: chats.length, unread: 0, pipeline: 0, won: 0, byStage: {}, byChannel: { tg: chats.length } }
    chats.forEach((c) => { s.byStage[c.stage] = (s.byStage[c.stage] || 0) + 1; if (c.unread) s.unread++; if (c.stage === 'deal') s.won++; if (!['deal', 'lost'].includes(c.stage)) s.pipeline += c.amount })
    return s
  },

  BroadcastStatus: async () => ({ ...bc, sentToday, dailyLimit: settings.dailyLimit }),
  PreviewTemplate: async (tpl, id) => {
    const c = find(id) || { title: '' }
    return tpl.replaceAll('{name}', c.title.split(' ')[0] || 'Здравствуйте').replaceAll('{title}', c.title)
  },
  StartBroadcast: async (req) => {
    const { chatIds } = req
    settings.template = req.template || settings.template
    bc = { running: true, total: chatIds.length, done: 0, failed: 0, current: '', nextAt: 0, lastError: '' }
    const push = () => emit('broadcast:status', { ...bc, sentToday, dailyLimit: settings.dailyLimit })
    push()
    ;(async () => {
      for (const id of chatIds) {
        if (!bc.running) break
        bc.current = find(id)?.title || ''; push(); await wait(900)
        bc.done++; sentToday++; const c = find(id); if (c && c.stage === 'new') c.stage = 'sent'
        emit('chats:changed')
        bc.current = ''; bc.nextAt = Math.floor(Date.now() / 1000) + 3; push(); await wait(1500)
      }
      bc.running = false; bc.nextAt = 0; push()
    })()
  },
  StopBroadcast: async () => { bc.running = false },
}

// в демо иногда «пишет» клиент — чтобы было видно живые обновления
setInterval(() => {
  if (status.state !== 'ready') return
  const c = chats[Math.floor(Math.random() * chats.length)]
  const m = { id: ++nextMsg, chatId: c.id, msgId: nextMsg, out: false, text: 'Добрый день! Есть минутка обсудить?', date: Math.floor(Date.now() / 1000) }
  messages[c.id].push(m)
  Object.assign(c, { lastMsg: m.text, lastAt: m.date, lastOut: false, unread: true })
  if (c.stage === 'sent') c.stage = 'replied'
  emit('message:new', m); emit('chats:changed')
}, 45000)
