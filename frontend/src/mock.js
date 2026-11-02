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
const ml = chats.find((c) => c.title.startsWith('Клиника'))
messages[ml.id].push({ id: 600, chatId: ml.id, msgId: 600, out: false, date: ml.lastAt + 120, text: '', mediaKind: 'file', fileName: 'Платёжное поручение №418.pdf', fileSize: 186_000 })
const events = {}
let savedTheme = 'dark'
try { savedTheme = localStorage.getItem('sinaps-theme') || 'dark' } catch {}
const hiddenExt = new Set()
let waStatus = { state: 'off' }
const setWa = (s) => { waStatus = s; emit('wa:status', s) }
const waKnown = [
  { jid: '79161234567@s.whatsapp.net', name: 'Салон Lumière', lastAt: now - 2 * h, lastMsg: 'Здравствуйте! Сколько стоит бот для записи?' },
  { jid: '79035550011@s.whatsapp.net', name: 'Мама', lastAt: now - 5 * h, lastMsg: 'Позвони вечером' },
  { jid: '77017778899@s.whatsapp.net', name: 'Автосервис «Мотор»', lastAt: now - 30 * h, lastMsg: 'Пришлите, пожалуйста, КП' },
  { jid: '996700123456@s.whatsapp.net', name: 'Азамат', lastAt: now - 60 * h, lastMsg: 'Ок, спасибо' },
]
const appConfig = { theme: savedTheme, plugins: {} }

const find = (id) => chats.find((c) => c.id === id)

// ---- плагины (демо) ----
const chekiFields = [
  { key: 'kinds', label: 'Что можно отметить как чек', type: 'select', options: ['PDF и фото', 'Только PDF', 'Любой файл'], default: 'PDF и фото' },
  { key: 'currency', label: 'Валюта по умолчанию', type: 'select', options: ['₽ рубль', 'сом', '₸ тенге', '$ доллар'], default: '₽ рубль' },
  { key: 'sumToDeal', label: 'Сумма чеков → «Сумма сделки» клиента', type: 'bool', default: true },
  { key: 'firstToDeal', label: 'Первый чек переводит клиента в «Сделка»', type: 'bool', default: true },
]
const chekiDefaults = Object.fromEntries(chekiFields.map((f) => [f.key, f.default]))
const pluginSettings = {}
const demoPlugins = [
  { file: 'cheki.sinaps', id: 'cheki', name: 'Чеки', version: '0.1', description: 'Отмечайте файлы клиентов как чеки и смотрите статистику оплат', icon: 'receipt', requires: [], fields: chekiFields, enabled: true, base: 'ok' },
  { file: 'moy-nalog.sinaps', id: 'moy-nalog', name: '«Мой налог»', version: '0.1', description: 'Чек самозанятого по отмеченному чеку', icon: 'puzzle', requires: ['cheki'], fields: [], enabled: false, base: 'error', error: 'Эта версия CRM не умеет запускать плагин «moy-nalog»' },
  { file: 'old-reports.sinaps', id: 'reports', name: 'Отчёты', version: '0.3', description: '', icon: 'puzzle', requires: [], fields: [], enabled: false, base: 'error', error: 'Нужна CRM v1.4 или новее' },
  { file: 'photos.sinaps', id: '', name: '', version: '', description: '', icon: '', requires: [], fields: [], enabled: false, base: 'invalid', error: 'В архиве нет manifest.json' },
]
function pluginsView() {
  const on = new Set(demoPlugins.filter((p) => p.base === 'ok' && p.enabled).map((p) => p.id))
  const items = demoPlugins.map((p) => {
    const names = p.requires.map((r) => demoPlugins.find((x) => x.id === r)?.name || r)
    let status = p.base === 'ok' ? (p.enabled ? 'on' : 'off') : p.base
    let error = p.error || ''
    if (status === 'on' && p.requires.some((r) => !on.has(r))) { status = 'wait'; error = 'Включите «' + names.join('», «') + '», чтобы плагин заработал' }
    return { ...p, status, error, requiresNames: names }
  })
  return { dir: '~/.config/SINAPS-CRM/plugins', found: items.length, items }
}

// ---- чеки (демо) ----
const day = 86400
const ago = (d) => now - d * day
let receipts = [
  [ml.id, 'Клиника «Мед-Лайн»', 'Бот записи · этап 2', 'Платёжное поручение №418.pdf', 'paid', 25000, 3],
  [fp.id, 'Фитнес-клуб Pulse', 'Бот записи + 1С', 'чек_sber.pdf', 'paid', 60000, 11],
  [3, 'Автосервис «Гараж 24»', 'Предоплата', 'счёт-оплата.pdf', 'wait', 17500, 30],
  [4, 'Школа английского Lingvo', 'Бот пробного урока', 'payment.pdf', 'paid', 45000, 44],
  [ml.id, 'Клиника «Мед-Лайн»', 'Предоплата 50%', 'pp_312.pdf', 'paid', 40000, 50],
  [fp.id, 'Фитнес-клуб Pulse', 'Аудит и ТЗ', 'чек_0822.pdf', 'paid', 15000, 72],
  [4, 'Школа английского Lingvo', 'Доработки', 'payment_07.pdf', 'refund', 8000, 95],
  [6, 'Кофейня «Зерно»', 'Лендинг', 'pp_0611.pdf', 'paid', 32000, 140],
  [ml.id, 'Клиника «Мед-Лайн»', 'Консультация', 'check.pdf', 'paid', 12000, 200],
].map(([chatId, chatTitle, what, fileName, status, amount, d], i) => ({ id: i + 1, chatId, msgId: i === 0 ? 600 : 9000 + i, chatTitle, channel: 'tg', what, fileName, status, amount, currency: '₽', paidAt: ago(d), createdAt: ago(d) }))
const MONTHS = ['янв', 'фев', 'мар', 'апр', 'май', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']
function demoReport(chatId, period) {
  const d = new Date()
  const from = period === 'all' ? 0 : period === 'quarter' ? new Date(d.getFullYear(), d.getMonth() - 2, 1).getTime() / 1000 : new Date(d.getFullYear(), 0, 1).getTime() / 1000
  const months = Array.from({ length: 12 }, (_, i) => { const m = new Date(d.getFullYear(), d.getMonth() - 11 + i, 1); return { label: MONTHS[m.getMonth()], sum: 0, key: m.getFullYear() * 12 + m.getMonth() } })
  const sorted = [...receipts].sort((a, b) => b.paidAt - a.paidAt)
  const rows = sorted.filter((r) => r.paidAt >= from && (!chatId || r.chatId === chatId))
  sorted.filter((r) => r.status === 'paid' && (!chatId || r.chatId === chatId)).forEach((r) => { const t = new Date(r.paidAt * 1000); const m = months.find((x) => x.key === t.getFullYear() * 12 + t.getMonth()); if (m) m.sum += r.amount })
  const paid = rows.filter((r) => r.status === 'paid')
  const total = paid.reduce((a, r) => a + r.amount, 0)
  const by = {}; sorted.filter((r) => r.paidAt >= from && r.status === 'paid').forEach((r) => { (by[r.chatId] ||= { chatId: r.chatId, title: r.chatTitle, channel: r.channel, sum: 0 }).sum += r.amount })
  const cl = {}; sorted.forEach((r) => { cl[r.chatId] ||= { chatId: r.chatId, title: r.chatTitle, channel: r.channel, sum: 0 } })
  return { rows, total, wait: rows.filter((r) => r.status === 'wait').reduce((a, r) => a + r.amount, 0), avg: paid.length ? Math.round(total / paid.length) : 0, count: rows.length, months, top: Object.values(by).sort((a, b) => b.sum - a.sum).slice(0, 5), clients: Object.values(cl).sort((a, b) => a.title.localeCompare(b.title)), currency: '₽', year: d.getFullYear() }
}
const wait = (ms = 250) => new Promise((r) => setTimeout(r, ms))
let bc = { running: false, total: 0, done: 0, failed: 0, current: '', nextAt: 0, lastError: '' }
let sentToday = 3
let settings = { tgApiId: 0, hasApiHash: false, tgFolderId: 0, tgFolderName: '', historyLimit: 200, autoSync: true, dailyLimit: 20, template: '{name}, здравствуйте! Это Тимур, SINAPS. Делаю ботов и ИИ-автоматизацию для бизнеса: запись клиентов, ответы 24/7, заявки сразу в таблицу. Если есть что автоматизировать — подскажу бесплатно. Актуально?' }
const setStatus = (s) => { status = s; emit('tg:status', s) }

export const mock = {
  GetInfo: async () => ({ version: '0.2.0-demo', dataDir: '~/.config/SINAPS-CRM' }),
  OpenDataDir: async () => {},
  GetSettings: async () => ({ ...settings }),
  SaveSettings: async (p) => { Object.assign(settings, Object.fromEntries(Object.entries(p).filter(([, v]) => v != null))) },

  GetConfig: async () => ({ ...appConfig }),
  SetTheme: async (theme) => { appConfig.theme = theme === 'light' ? 'light' : 'dark'; try { localStorage.setItem('sinaps-theme', appConfig.theme) } catch {} ; return { ...appConfig } },
  Plugins: async () => pluginsView(),
  SetPluginEnabled: async (id, on) => { const p = demoPlugins.find((x) => x.id === id); if (p) p.enabled = on; const v = pluginsView(); emit('plugins:changed', v); emit('chats:changed'); return v },
  PluginSettings: async (id) => ({ ...chekiDefaults, ...(pluginSettings[id] || {}) }),
  SavePluginSettings: async (id, st) => { pluginSettings[id] = st },
  OpenPluginsDir: async () => {},
  ChoosePluginsDir: async () => pluginsView(),
  InstallPlugin: async (path) => { const file = path.split('/').pop(); if (!demoPlugins.some((p) => p.file === file)) demoPlugins.push({ file, id: file.replace(/\.sinaps$/, ''), name: file.replace(/\.sinaps$/, ''), version: '0.1', description: '', icon: 'puzzle', requires: [], fields: [], enabled: false, base: 'error', error: 'Эта версия CRM не умеет запускать этот плагин' }); const v = pluginsView(); emit('plugins:changed', v); return v },
  RevealPlugin: async () => {},
  MarkReceipt: async (r) => {
    await wait(200)
    const m = (messages[r.chatId] || []).find((x) => x.msgId === r.msgId); const c = find(r.chatId)
    const paidAt = r.date ? Math.floor(new Date(r.date + 'T12:00:00').getTime() / 1000) : m.date
    const rec = { id: r.chatId * 1000 + r.msgId, chatId: r.chatId, msgId: r.msgId, chatTitle: c.title, channel: c.channel, fileName: m.fileName || 'Фото', amount: r.amount, currency: '₽', status: r.status || 'paid', what: r.what || '', paidAt, createdAt: now }
    receipts = receipts.filter((x) => !(x.chatId === r.chatId && x.msgId === r.msgId)); receipts.push(rec)
    c.amount = receipts.filter((x) => x.chatId === r.chatId && x.status === 'paid').reduce((a, x) => a + x.amount, 0)
    if (rec.status === 'paid') c.stage = 'deal'
    emit('receipts:changed', r.chatId); emit('chats:changed'); return rec
  },
  UnmarkReceipt: async (chatId, msgId) => { receipts = receipts.filter((x) => !(x.chatId === chatId && x.msgId === msgId)); emit('receipts:changed', chatId); emit('chats:changed') },
  Receipts: async ({ chatId, period }) => demoReport(chatId, period),
  ChekiSummary: async () => ({ count: receipts.length, sumYear: receipts.filter((r) => r.status === 'paid').reduce((a, r) => a + r.amount, 0), clients: new Set(receipts.map((r) => r.chatId)).size, year: new Date().getFullYear(), currency: '₽' }),
  ExportReceipts: async () => { await wait(400); return { path: '/home/timur/sinaps-cheki.xlsx', count: receipts.length } },
  RemoveMessage: async (chatId, msgId) => {
    messages[chatId] = messages[chatId].filter((m) => m.msgId !== msgId)
    emit('message:deleted', { chatId, removed: [msgId], marked: [] }); emit('chats:changed')
  },
  WaStatus: async () => waStatus,
  WaConnect: async () => {
    setWa({ state: 'connecting' }); await wait(600)
    setWa({ state: 'need_qr', qr: '2@demo,' + Date.now() })
    setTimeout(() => { if (waStatus.state === 'need_qr') { setWa({ state: 'ready', name: 'Тимур', phone: '+996555000111', known: 0, syncing: true }); setTimeout(() => { setWa({ ...waStatus, syncing: false, known: waKnown.length }); emit('wa:known') }, 1500) } }, 6000)
  },
  WaLogout: async () => setWa({ state: 'off' }),
  WaKnownChats: async () => waKnown.map((k) => ({ ...k, inCrm: chats.some((c) => c.channel === 'wa' && c.extId === k.jid), hidden: false })),
  WaAddChats: async (jids) => {
    let n = 0
    jids.forEach((jid) => {
      const k = waKnown.find((x) => x.jid === jid); if (!k || chats.some((c) => c.extId === jid)) return
      const id = Math.max(...chats.map((c) => c.id)) + 1
      chats.push({ id, channel: 'wa', extId: jid, kind: 'user', title: k.name, username: '', phone: jid.split('@')[0], stage: 'new', amount: 0, nextStep: '', note: '', tags: '', unread: false, lastMsg: k.lastMsg, lastAt: k.lastAt, lastOut: false, createdAt: k.lastAt })
      messages[id] = [{ id: ++nextMsg, chatId: id, msgId: nextMsg, out: false, text: k.lastMsg, date: k.lastAt }]
      n++
    })
    emit('chats:changed'); return { added: n, messages: n }
  },
  WaHideChats: async (m) => { Object.keys(m).forEach((e) => { const i = chats.findIndex((c) => c.extId === e); if (i >= 0) chats.splice(i, 1) }); emit('chats:changed'); return Object.keys(m).length },
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
  GetChat: async (id) => ({ chat: { ...find(id) }, messages: [...(messages[id] || [])], events: [...(events[id] || []), { kind: 'import', text: 'Импорт из Telegram', at: find(id).createdAt }], receipts: pluginsView().items.some((p) => p.id === 'cheki' && p.status === 'on') ? receipts.filter((r) => r.chatId === id).sort((a, b) => b.paidAt - a.paidAt) : null }),
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
  ExportData: async (fmt) => { await wait(500); return { path: '/home/timur/sinaps-crm-2026-11-01.' + (fmt === 'sqlite' ? 'db' : fmt), count: chats.length } },
  PickImportFile: async () => '/home/timur/Загрузки/лиды-2гис.xlsx',
  PreviewImport: async (path) => { await wait(400); return { kind: 'table', file: path.split('/').pop(), rows: 42, create: 39, update: 3, skip: 0, problems: ['строка 17: неизвестный этап «думает» — этап не изменится'], sample: [['Стоматология «Дента»', 'Новые', '', 'новый'], ['Фитнес-клуб Pulse', 'Сделка', '60000', 'обновить'], ['Барбершоп Top Gun', 'Новые', '', 'новый']] } },
  ImportTable: async () => { await wait(700); return { created: 39, updated: 3, skipped: 0, problems: [] } },
  RestoreDatabase: async () => {},
  ShowInFolder: async () => {},
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
