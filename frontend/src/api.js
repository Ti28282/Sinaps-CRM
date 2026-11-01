// Мост к Go: в Wails методы лежат в window.go.main.App, события — в window.runtime.
// Без Wails (npm run dev в браузере) работает демо-режим из mock.js.
import { mock, mockOn } from './mock.js'

const inWails = () => typeof window !== 'undefined' && window.go?.main?.App

function call(name, ...args) {
  if (inWails()) return window.go.main.App[name](...args)
  return mock[name](...args)
}

export const isDemo = !inWails()

export function on(event, cb) {
  if (inWails() && window.runtime?.EventsOn) return window.runtime.EventsOn(event, cb)
  return mockOn(event, cb)
}

export const api = {
  info: () => call('GetInfo'),
  openDataDir: () => call('OpenDataDir'),
  settings: () => call('GetSettings'),
  saveSettings: (p) => call('SaveSettings', p),
  config: () => call('GetConfig'),
  setTheme: (t) => call('SetTheme', t),
  plugins: () => call('ListPlugins'),
  setPluginEnabled: (id, on) => call('SetPluginEnabled', id, on),
  savePluginSettings: (id, settings) => call('SavePluginSettings', id, settings),
  exportData: (format) => call('ExportData', format),
  pickImportFile: () => call('PickImportFile'),
  previewImport: (path) => call('PreviewImport', path),
  importTable: (path) => call('ImportTable', path),
  restoreDatabase: (path) => call('RestoreDatabase', path),
  showInFolder: (path) => call('ShowInFolder', path),

  tgStatus: () => call('TgStatus'),
  tgSaveKeys: (id, hash) => call('TgSaveKeys', Number(id) || 0, hash),
  tgSendCode: (phone) => call('TgSendCode', phone),
  tgSignIn: (code) => call('TgSignIn', code),
  tgResend: () => call('TgResendCode'),
  tgStartQR: () => call('TgStartQR'),
  tgCancelQR: () => call('TgCancelQR'),
  tgPassword: (pwd) => call('TgPassword', pwd),
  tgLogout: () => call('TgLogout'),
  tgFolders: () => call('TgFolders'),
  syncFolder: (id, name) => call('SyncFolder', id, name),
  resync: () => call('Resync'),
  waStatus: () => call('WaStatus'),
  waConnect: () => call('WaConnect'),
  waLogout: () => call('WaLogout'),
  waKnownChats: () => call('WaKnownChats'),
  waAddChats: (jids) => call('WaAddChats', jids),
  waHideChats: (chats) => call('WaHideChats', chats),
  folderChats: (folderId) => call('FolderChats', folderId),
  hideChats: (chats) => call('HideChats', chats),
  unhideChats: (extIds) => call('UnhideChats', extIds),

  chats: (channel = 'all', search = '') => call('ListChats', channel, search),
  chat: (id) => call('GetChat', id),
  updateChat: (id, patch) => call('UpdateChat', id, patch),
  markRead: (id) => call('MarkRead', id),
  deleteChat: (id) => call('DeleteChat', id),
  send: (id, text) => call('SendMessage', id, text),
  deleteMessage: (chatId, msgId) => call('DeleteMessage', chatId, msgId),
  pickFile: () => call('PickFile'),
  fileInfo: (path) => call('FileInfo', path),
  sendFile: (chatId, path, caption, asPhoto) => call('SendFile', chatId, path, caption, asPhoto),
  openAttachment: (chatId, msgId) => call('OpenAttachment', chatId, msgId),
  stats: () => call('Stats'),

  broadcastStatus: () => call('BroadcastStatus'),
  previewTemplate: (tpl, id) => call('PreviewTemplate', tpl, id),
  startBroadcast: (template, chatIds) => call('StartBroadcast', { template, chatIds }),
  stopBroadcast: () => call('StopBroadcast'),
}

export const STAGES = [
  { id: 'new', title: 'Новые', color: '#7CC6F2' },
  { id: 'sent', title: 'Написал', color: '#B39DFF' },
  { id: 'replied', title: 'Ответил', color: '#F5B65C' },
  { id: 'deal', title: 'Сделка', color: '#C8F25A' },
  { id: 'lost', title: 'Потерян', color: '#6B7079' },
]

export const errText = (e) => (typeof e === 'string' ? e : e?.message || String(e))

export const money = (n) => (n || 0).toLocaleString('ru-RU') + ' ₽'

export function when(unix) {
  if (!unix) return ''
  const d = new Date(unix * 1000)
  const now = new Date()
  const days = Math.floor((new Date(now.toDateString()) - new Date(d.toDateString())) / 864e5)
  if (days === 0) return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
  if (days === 1) return 'вчера'
  if (days < 7) return d.toLocaleDateString('ru-RU', { weekday: 'short' })
  return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' })
}

export function clock(unix) {
  return new Date(unix * 1000).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

export function dayLabel(unix) {
  const d = new Date(unix * 1000)
  const days = Math.floor((new Date(new Date().toDateString()) - new Date(d.toDateString())) / 864e5)
  if (days === 0) return 'Сегодня'
  if (days === 1) return 'Вчера'
  return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })
}

const PALETTE = ['#F5B65C', '#B39DFF', '#7CC6F2', '#C8F25A', '#F59BB0', '#7FE0AE', '#8CCDF5']
export const avatarColor = (id) => PALETTE[Math.abs(Number(id) || 0) % PALETTE.length]
export function initials(title = '') {
  const w = title.replace(/[«»"']/g, '').split(/\s+/).filter(Boolean)
  return ((w[0]?.[0] || '?') + (w[1]?.[0] || '')).toUpperCase()
}

export function applyTheme(theme) {
  document.documentElement.dataset.theme = theme === 'light' ? 'light' : 'dark'
}
