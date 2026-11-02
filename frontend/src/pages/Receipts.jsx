import { useCallback, useEffect, useState } from 'react'
import { api, on, errText, money } from '../api.js'
import { Icon } from '../components/ui.jsx'

// Страница плагина «Чеки»: оплаты клиентов из воронки по отмеченным чекам.
const PERIODS = [
  { id: 'quarter', label: 'Квартал' },
  { id: 'year', label: 'Год' },
  { id: 'all', label: 'Всё время' },
]
const STATUS = { paid: 'оплачен', wait: 'ожидает', refund: 'возврат' }
const short = (n) => (n >= 1_000_000 ? (n / 1_000_000).toLocaleString('ru-RU', { maximumFractionDigits: 1 }) + 'м' : n >= 1000 ? Math.round(n / 1000) + 'к' : String(n))

export default function Receipts({ toast, openChat }) {
  const [chatId, setChatId] = useState(0)
  const [period, setPeriod] = useState('year')
  const [rep, setRep] = useState(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => api.receipts(chatId, period).then(setRep).catch((e) => toast(errText(e), 'err')), [chatId, period, toast])
  useEffect(() => { load() }, [load])
  useEffect(() => on('receipts:changed', () => load()), [load])

  async function exportXlsx() {
    setBusy(true)
    try {
      const r = await api.exportReceipts(chatId, period)
      if (r.path) toast(`Сохранено чеков: ${r.count} → ${r.path.split(/[\\/]/).pop()}`)
    } catch (e) { toast(errText(e), 'err') } finally { setBusy(false) }
  }

  if (!rep) return <div className="page"><p className="hint">Загрузка…</p></div>
  const cur = rep.currency
  const max = Math.max(1, ...rep.months.map((m) => m.sum))
  const topMax = Math.max(1, ...rep.top.map((t) => t.sum))
  const client = rep.clients.find((c) => c.chatId === chatId)

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <div className="row" style={{ gap: 10 }}>
            <h1>Чеки</h1>
            <span className="plugin-badge">плагин</span>
          </div>
          <p>Оплаты клиентов из воронки по отмеченным чекам</p>
        </div>
        <div className="row">
          <label className="select-pill">
            <span>Клиент</span>
            <select value={chatId} onChange={(e) => setChatId(Number(e.target.value))}>
              <option value={0}>Все из воронки</option>
              {rep.clients.map((c) => <option key={c.chatId} value={c.chatId}>{c.title}</option>)}
            </select>
          </label>
          <div className="seg" role="group" aria-label="Период">
            {PERIODS.map((p) => (
              <button key={p.id} type="button" aria-pressed={period === p.id} onClick={() => setPeriod(p.id)}>{p.id === 'year' ? rep.year : p.label}</button>
            ))}
          </div>
          <button type="button" className="btn" onClick={exportXlsx} disabled={busy}>{busy ? 'Сохраняем…' : 'Экспорт в Excel'}</button>
        </div>
      </header>

      <div className="stats">
        <div className="stat"><div className="stat-label">Получено</div><div className="stat-value accent">{money(rep.total, cur)}</div></div>
        <div className="stat"><div className="stat-label">Чеков</div><div className="stat-value">{rep.count}</div></div>
        <div className="stat"><div className="stat-label">Средний чек</div><div className="stat-value">{rep.avg ? money(rep.avg, cur) : '—'}</div></div>
        <div className="stat"><div className="stat-label">Ожидают оплаты</div><div className="stat-value warn">{money(rep.wait, cur)}</div></div>
      </div>

      <div className="rc-grid">
        <section className="panel rc-chart">
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <h2>По месяцам</h2>
            <span className="hint">{client ? client.title : 'все клиенты'}, только оплаченные · 12 мес.</span>
          </div>
          <div className="bars" role="img" aria-label="Оплаты по месяцам">
            {rep.months.map((m, i) => (
              <div key={i} className="bar-col" title={`${m.label}: ${money(m.sum, cur)}`}>
                <span className="bar-val">{m.sum ? short(m.sum) : ''}</span>
                <div className={'bar' + (i === rep.months.length - 1 ? ' now' : '')} style={{ height: `${Math.max(m.sum ? 4 : 2, (m.sum / max) * 100)}%` }} />
                <span className="bar-lbl">{m.label}</span>
              </div>
            ))}
          </div>
        </section>

        <section className="panel rc-top">
          <h2>Топ клиентов</h2>
          {rep.top.length === 0 && <p className="hint">За период оплат нет</p>}
          {rep.top.map((t) => (
            <button key={t.chatId} type="button" className="top-row" aria-pressed={chatId === t.chatId} onClick={() => setChatId(chatId === t.chatId ? 0 : t.chatId)}>
              <span className="row" style={{ justifyContent: 'space-between', flexWrap: 'nowrap' }}>
                <span className="top-name">{t.title} <span className={'ch-mini ' + t.channel}>{t.channel === 'wa' ? 'WA' : 'TG'}</span></span>
                <span className="mono">{money(t.sum, cur)}</span>
              </span>
              <span className="top-bar"><i style={{ width: `${(t.sum / topMax) * 100}%` }} /></span>
            </button>
          ))}
        </section>
      </div>

      <section className="panel" style={{ padding: 0, gap: 0, overflow: 'hidden' }}>
        <div className="row rc-table-head">
          <h2>Все чеки</h2>
          <span className="hint">{chatId ? 'фильтр: ' + (client?.title || '') : 'нажмите на клиента в топе, чтобы отфильтровать'}</span>
          <span className="hint" style={{ marginLeft: 'auto', fontSize: 12 }}>
            {rep.rows.length > 7 ? `${rep.rows.length} записей · видно 7, остальные — прокруткой` : `${rep.rows.length} записей`}
          </span>
        </div>
        {rep.rows.length === 0 ? (
          <div className="empty" style={{ padding: 32 }}>
            <div><Icon name="receipt" size={30} /><h3>Чеков пока нет</h3><p>В «Диалогах» наведите на PDF или фото клиента и нажмите значок чека.</p></div>
          </div>
        ) : (
          <div className="rc-scroll">
            <table className="rc-table">
              <thead>
                <tr><th>Дата</th><th>Клиент</th><th>За что</th><th>Файл</th><th>Статус</th><th className="num">Сумма</th></tr>
              </thead>
              <tbody>
                {rep.rows.map((r) => (
                  <tr key={r.id}>
                    <td className="muted nowrap">{new Date(r.paidAt * 1000).toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit', year: '2-digit' })}</td>
                    <td className="nowrap">
                      <button type="button" className="link-btn" onClick={() => openChat(r.chatId)}>{r.chatTitle}</button>
                      <span className={'ch-mini ' + r.channel}>{r.channel === 'wa' ? 'WA' : 'TG'}</span>
                    </td>
                    <td className="muted">{r.what || '—'}</td>
                    <td className="file nowrap">{r.fileName}</td>
                    <td><span className={'rc-st st-' + r.status}>{STATUS[r.status]}</span></td>
                    <td className="num mono nowrap">{money(r.amount, r.currency)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}
