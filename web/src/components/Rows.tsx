// The rows every list shares: an asset or debt with its value, a holding, an activity.
import { Link } from 'react-router-dom'
import { ACTIVITY, MODE_NAME, SOURCE_ICON, SOURCE_NAME, kindOf, linkedApp, money, pct, signed, units } from '../lib/wealth'
import { fmtDate } from '../lib/format'
import type { Activity, Asset, Position } from '../lib/types'
import { Icon } from './icons'

export function AssetIcon({ a, size = 16 }: { a: Pick<Asset, 'kind' | 'group'>; size?: number }) {
  return <span className={`list-ico gr-${a.group}`}><Icon name={kindOf(a.kind).icon} size={size} /></span>
}

export function AssetRow({ a, base, showChange = true }: { a: Asset; base: string; showChange?: boolean }) {
  const app = linkedApp(a.source_ref)
  const closed = !!a.closed_on && a.closed_on <= new Date().toISOString().slice(0, 10)
  const sub = [a.institution, closed ? `${a.debt ? 'paid off' : 'closed'} ${fmtDate(a.closed_on)}` : MODE_NAME[a.value_mode], a.share < 1 && `your ${Math.round(a.share * 100)}%`].filter(Boolean).join(' · ')
  return (
    <Link to={`/a/${a.id}`} className={`list-row ${closed || !a.include ? 'is-archived' : ''}`}>
      <AssetIcon a={a} />
      <span className="grow row-main">
        <span className="row-title"><span className="ellipsis">{a.name}</span>{a.stale && <span className="pill st-soon" title="Typed in a while ago"><Icon name="clock" size={11} />Update</span>}{a.linked_error && <span className="pill st-expired" title={a.linked_error}><Icon name="alert" size={11} />Link</span>}</span>
        <span className="row-sub muted small">
          {app && <span className="src-chip" title={`From ${SOURCE_NAME[app]}`}><Icon name={SOURCE_ICON[app] || 'layers'} size={11} /></span>}
          <span className="ellipsis">{sub || ' '}</span>
        </span>
      </span>
      <span className="row-num">
        <span className="tnum row-amount">{a.known ? `${a.debt ? '−' : ''}${money(a.mine, a.currency, true)}` : '—'}</span>
        {a.currency !== base && a.known && <span className="muted small tnum">{money(a.value_base, base, true)}</span>}
        {showChange && a.currency === base && a.month_change !== 0 && a.known && !closed && <span className={`small tnum ${(a.debt ? -a.month_change : a.month_change) >= 0 ? 'ok' : 'bad'}`}>{signed(a.debt ? -a.month_change : a.month_change, base)}</span>}
      </span>
    </Link>
  )
}

export function PositionRow({ p, onClick, showAsset }: { p: Position; onClick?: () => void; showAsset?: boolean }) {
  const sub = [p.symbol, `${units(p.units)} × ${money(Math.round(p.price * 100), p.currency)}`, showAsset && p.asset_name].filter(Boolean).join(' · ')
  return (
    <button type="button" className="list-row as-btn" onClick={onClick}>
      <span className="list-ico gr-investments"><Icon name={p.kind === 'crypto' ? 'bitcoin' : p.kind === 'bond' ? 'landmark' : p.kind === 'cash' ? 'wallet' : 'trending-up'} size={16} /></span>
      <span className="grow row-main">
        <span className="row-title"><span className="ellipsis">{p.name}</span>{p.price_error && <span className="pill st-expired" title={p.price_error}><Icon name="alert" size={11} />Price</span>}</span>
        <span className="row-sub muted small"><span className="ellipsis">{sub}</span></span>
      </span>
      <span className="row-num">
        <span className="tnum row-amount">{money(p.value, p.currency, true)}</span>
        {p.cost > 0 && <span className={`small tnum ${p.gain >= 0 ? 'ok' : 'bad'}`}>{signed(p.gain, p.currency)} · {pct(p.gain_pct)}</span>}
      </span>
    </button>
  )
}

export function ActivityRow({ x, showAsset, onDelete }: { x: Activity; showAsset?: boolean; onDelete?: () => void }) {
  const k = ACTIVITY[x.kind] || { label: x.kind, icon: 'dot', past: x.kind }
  const what = x.position_name || (showAsset ? x.asset_name : '')
  const title = x.kind === 'buy' || x.kind === 'sell' ? `${k.past} ${units(x.units)} ${what}` : `${k.past}${what ? ` · ${what}` : ''}`
  const sub = [fmtDate(x.date), showAsset && x.position_name && x.asset_name, x.note].filter(Boolean).join(' · ')
  const neg = x.kind === 'buy' || x.kind === 'withdrawal' || x.kind === 'fee'
  return (
    <div className="list-row">
      <span className={`list-ico ${x.kind === 'dividend' || x.kind === 'interest' ? 'st-done' : 'st-ok'}`}><Icon name={k.icon} size={15} /></span>
      <span className="grow row-main"><span className="row-title"><span className="ellipsis">{title}</span></span><span className="muted small ellipsis">{sub}</span></span>
      <span className={`tnum row-amount ${x.kind === 'dividend' || x.kind === 'interest' ? 'ok' : ''}`}>{neg ? '−' : '+'}{money(x.amount, x.currency)}</span>
      {onDelete && <button type="button" className="btn ghost icon-only sm" aria-label="Delete" title="Delete (does not change the holding back)" onClick={onDelete}><Icon name="trash" size={14} /></button>}
    </div>
  )
}
