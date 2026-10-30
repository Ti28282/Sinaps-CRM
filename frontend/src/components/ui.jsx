import { avatarColor, initials } from '../api.js'

const P = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round', strokeLinejoin: 'round' }

const paths = {
  board: <><rect x="3" y="4" width="5" height="16" rx="1.5" /><rect x="10" y="4" width="5" height="11" rx="1.5" /><rect x="17" y="4" width="4" height="7" rx="1.5" /></>,
  chat: <path d="M4 5h16v11H9l-5 4z" />,
  send: <><path d="M21 3 10 14" /><path d="M21 3 14.5 21 10 14 3 9.5z" /></>,
  link: <><path d="M9 15 15 9" /><path d="M11 6.5 13.5 4a4.2 4.2 0 0 1 6 6L17 12.5" /><path d="M13 17.5 10.5 20a4.2 4.2 0 0 1-6-6L7 11.5" /></>,
  search: <><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></>,
  sync: <><path d="M20 11a8 8 0 0 0-14.5-4.5L4 8" /><path d="M4 4v4h4" /><path d="M4 13a8 8 0 0 0 14.5 4.5L20 16" /><path d="M20 20v-4h-4" /></>,
  arrow: <path d="M5 12h14M13 6l6 6-6 6" />,
  back: <path d="M15 18 9 12l6-6" />,
  trash: <><path d="M4 7h16M10 11v6M14 11v6" /><path d="M6 7l1 13h10l1-13M9 7V4h6v3" /></>,
  folder: <path d="M3 6.5A1.5 1.5 0 0 1 4.5 5H9l2 2h8.5A1.5 1.5 0 0 1 21 8.5v9a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 17.5z" />,
  stop: <rect x="6" y="6" width="12" height="12" rx="2" />,
  plus: <path d="M12 5v14M5 12h14" />,
}

export function Icon({ name, size = 18, ...rest }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" {...P} aria-hidden="true" {...rest}>{paths[name]}</svg>
}

export function TgIcon({ size = 15 }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="2" strokeLinejoin="round" aria-hidden="true"><path d="M21 4 3 11l6 2 2 7 3-4 5 4z" /></svg>
}

export function WaIcon({ size = 15 }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M4 20l1.3-4A8 8 0 1 1 8 18.7z" /></svg>
}

export function Avatar({ chat, size = 34 }) {
  return (
    <div className="avatar" style={{ width: size, height: size, background: avatarColor(chat.id), fontSize: Math.round(size * 0.36) }}>
      {initials(chat.title)}
    </div>
  )
}

export function ChannelChip({ channel }) {
  return channel === 'wa'
    ? <span className="chip chip-wa">WhatsApp</span>
    : <span className="chip chip-tg">Telegram</span>
}
