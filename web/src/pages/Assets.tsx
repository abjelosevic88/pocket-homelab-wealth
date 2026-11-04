// Everything you own and owe, by group; and the page about one of them.
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { announceChange, useOnChange } from '../lib/data'
import { fmtDate } from '../lib/format'
import { GROUPS, MODE_NAME, SOURCE_NAME, kindOf, linkedApp, money, moneyShort, months, pct, signed } from '../lib/wealth'
import type { Asset, AssetDetail, Payoff } from '../lib/types'
import { Card, Confirm, Empty, Loading, MenuButton, errMsg, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { LineChart } from '../components/Chart'
import { ActivityRow, AssetRow, PositionRow } from '../components/Rows'
import { ActivityModal, AssetModal, AttachModal, PositionModal, ReadStatementModal, ValueModal } from '../components/Forms'
import { openCreate, useCrumb } from '../components/Layout'

export default function Assets() {
  const [sp, setSp] = useSearchParams()
  const group = sp.get('group') || ''
  const [q, setQ] = useState('')
  const [closed, setClosed] = useState(false)
  const { data, reload, error } = useAsync(() => api.get<{ currency: string; assets: Asset[] }>(`${V1}/assets${closed ? '?closed=1' : ''}`), [closed])
  useOnChange(reload)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error}</div>
  if (!data) return <Loading />
  const cur = data.currency
  const ql = q.trim().toLowerCase()
  const list = data.assets.filter(a => (!group || a.group === group) && (!ql || `${a.name} ${a.institution} ${kindOf(a.kind).label}`.toLowerCase().includes(ql)))
  const groups = GROUPS.map(g => ({ ...g, items: list.filter(a => a.group === g.value) })).filter(g => g.items.length > 0)
  const total = (items: Asset[]) => items.filter(a => a.include).reduce((s, a) => s + a.value_base, 0)
  const counts = Object.fromEntries(GROUPS.map(g => [g.value, data.assets.filter(a => a.group === g.value).length]))
  return (
    <div className="stack loose">
      <div className="page-head">
        <div className="grow"><h1 className="page-title">Assets and debts</h1><p className="muted">{data.assets.length} in all · your part of each, in {cur}</p></div>
        <button type="button" className="btn primary" onClick={() => openCreate('asset')}><Icon name="plus" size={15} />Add</button>
      </div>
      <div className="row wrap filters">
        <input className="search-in" value={q} onChange={e => setQ(e.target.value)} placeholder="Search…" aria-label="Search" />
        <div className="chips">
          <button type="button" className={`chip ${!group ? 'active' : ''}`} onClick={() => setSp({})}>All</button>
          {GROUPS.filter(g => counts[g.value]).map(g => <button key={g.value} type="button" className={`chip ${group === g.value ? 'active' : ''}`} onClick={() => setSp({ group: g.value })}>{g.label}<span className="count">{counts[g.value]}</span></button>)}
        </div>
        <label className="check-row"><input type="checkbox" checked={closed} onChange={e => setClosed(e.target.checked)} />Sold and paid off too</label>
      </div>
      {groups.length === 0 ? <Empty title={data.assets.length ? 'Nothing matches' : 'Nothing here yet'}>{data.assets.length ? 'Try another search or group.' : <>Add what you own and owe, or <Link to="/connect">bring it in from your other apps</Link>.</>}</Empty> :
        groups.map(g => (
          <Card key={g.value} title={<span className="row"><Icon name={g.icon} size={15} />{g.label}</span>} actions={<span className={`tnum strong ${g.value === 'debts' ? '' : ''}`}>{g.value === 'debts' ? '−' : ''}{money(total(g.items), cur, true)}</span>} flush>
            <div className="list">{g.items.map(a => <AssetRow key={a.id} a={a} base={cur} />)}</div>
          </Card>
        ))}
    </div>
  )
}

/** The months and interest left at a monthly payment (the server does the same, plan.go payoffOf). */
function payoff(balance: number, rate: number, payment: number, extra: number): Payoff {
  const r = rate / 100 / 12
  const out: Payoff = { balance, rate, payment, extra, months: 0, paid_off_on: '', interest_left: 0, interest_month: Math.round(balance * r) }
  const pay = payment + extra
  if (balance <= 0) return out
  if (pay <= 0 || pay <= balance * r) return { ...out, months: -1 }
  let bal = balance, interest = 0, n = 0
  while (bal > 0.5 && n < 1200) { const i = bal * r; interest += i; bal = bal + i - pay; n++ }
  const d = new Date(); d.setMonth(d.getMonth() + n)
  return { ...out, months: n, interest_left: Math.round(interest), paid_off_on: d.toISOString().slice(0, 10) }
}

function PayoffCard({ a }: { a: Asset }) {
  const [extra, setExtra] = useState(0)
  const base = payoff(a.mine, a.rate, a.payment, 0)
  const step = Math.max(1000, Math.round(a.payment / 10 / 1000) * 1000 || 1000)
  const more = payoff(a.mine, a.rate, a.payment, extra)
  if (!a.payment) return <Card title="Paying it off"><p className="muted">Add the monthly payment (Edit) to see when it is paid off and what interest is still to come.{a.rate ? ` This month's interest is about ${money(base.interest_month, a.currency, true)}.` : ''}</p></Card>
  return (
    <Card title="Paying it off" sub={`${money(a.payment, a.currency, true)} a month at ${a.rate}%`}>
      <div className="stats-row tight">
        <div className="stat"><span className="stat-l">Paid off</span><span className="stat-v">{base.months < 0 ? 'Never' : fmtDate(base.paid_off_on)}</span><span className="stat-s small">{months(base.months)}</span></div>
        <div className="stat"><span className="stat-l">Interest to come</span><span className="stat-v tnum">{money(base.interest_left, a.currency, true)}</span><span className="stat-s small">{money(base.interest_month, a.currency, true)} this month</span></div>
      </div>
      {base.months > 0 && <div className="whatif">
        <label className="small" htmlFor="extra">If I paid <b className="tnum">{money(extra, a.currency, true)}</b> more each month</label>
        <input id="extra" type="range" min={0} max={step * 20} step={step} value={extra} onChange={e => setExtra(Number(e.target.value))} />
        {extra > 0 && <p className="small">Paid off <b>{fmtDate(more.paid_off_on)}</b>, {months(base.months - more.months)} sooner, saving <b className="ok tnum">{money(base.interest_left - more.interest_left, a.currency, true)}</b> in interest.</p>}
      </div>}
    </Card>
  )
}

export function AssetPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [sp, setSp] = useSearchParams()
  const { toast } = useToast()
  const { data, reload, error } = useAsync(() => api.get<AssetDetail>(`${V1}/assets/${id}`), [id])
  useOnChange(reload)
  const [modal, setModal] = useState<'' | 'edit' | 'value' | 'read' | 'activity' | 'position' | 'attach' | 'delete'>('')
  const [editPos, setEditPos] = useState<number | null>(null)
  const [updating, setUpdating] = useState(false)
  useCrumb(data?.asset.name)
  useEffect(() => { if (sp.get('update') && data) { setModal(data.asset.value_mode === 'positions' ? 'position' : 'value'); setSp({}, { replace: true }) } }, [sp, data, setSp])
  const vals = useMemo(() => (data ? [...data.valuations].reverse() : []), [data])
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error}</div>
  if (!data) return <Loading />
  const a = data.asset
  const cur = data.currency
  const k = kindOf(a.kind)
  const app = linkedApp(a.source_ref)
  const closed = !!a.closed_on && a.closed_on <= new Date().toISOString().slice(0, 10)
  const manual = a.value_mode === 'manual' || a.value_mode === 'estimate'
  const del = async () => {
    try { await api.del(`${V1}/assets/${a.id}`); toast(`${a.name} deleted`); announceChange(); navigate('/assets') } catch (e) { toast(errMsg(e), 'err') }
  }
  const delVal = async (vid: number) => { try { await api.del(`${V1}/valuations/${vid}`); announceChange() } catch (e) { toast(errMsg(e), 'err') } }
  const delAct = async (xid: number) => { if (!window.confirm('Delete this? The holding is not changed back.')) return; try { await api.del(`${V1}/activity/${xid}`); announceChange() } catch (e) { toast(errMsg(e), 'err') } }
  const delAtt = async (aid: number) => { try { await api.del(`${V1}/attachments/${aid}`); announceChange() } catch (e) { toast(errMsg(e), 'err') } }
  const prices = async () => {
    setUpdating(true)
    try { const r = await api.post<{ updated: number; failed: number }>(`${V1}/prices/update`); toast(`${r.updated} prices updated${r.failed ? `, ${r.failed} not found` : ''}`); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setUpdating(false) }
  }
  const refresh = async () => {
    setUpdating(true)
    try { await api.post(`${V1}/linked/refresh`); toast('Read again'); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setUpdating(false) }
  }
  const hist = data.history
  const sub = [k.label, a.institution, a.number].filter(Boolean).join(' · ')
  return (
    <div className="stack loose">
      <div className="detail-head">
        <span className={`detail-ico gr-${a.group}`}><Icon name={k.icon} size={22} /></span>
        <div className="grow" style={{ minWidth: 0 }}>
          <h1 className="page-title ellipsis">{a.name}</h1>
          <p className="muted small">{sub}{closed && <> · <b>{a.debt ? 'paid off' : 'closed'} {fmtDate(a.closed_on)}</b></>}</p>
        </div>
        <div className="row toolbar">
          {!closed && manual && <button type="button" className="btn primary" onClick={() => setModal('value')}><Icon name="edit" size={14} />{a.debt ? 'Update balance' : 'Update value'}</button>}
          {!closed && a.value_mode === 'positions' && <button type="button" className="btn primary" onClick={() => setModal('activity')}><Icon name="plus" size={14} />Record</button>}
          {!closed && a.value_mode === 'linked' && <button type="button" className="btn" disabled={updating} onClick={refresh}><Icon name="refresh" size={14} />Read again</button>}
          <button type="button" className="btn" onClick={() => setModal('edit')}><Icon name="settings" size={14} /><span className="hide-xs">Edit</span></button>
          <MenuButton label="More" align="right" items={[
            ...(manual ? [{ label: 'Read a statement (AI)', icon: 'sparkles', onSelect: () => setModal('read') }] : []),
            ...(a.value_mode !== 'positions' && !a.debt ? [{ label: 'Record money in or out', icon: 'repeat', onSelect: () => setModal('activity') }] : []),
            ...(!a.debt ? [{ label: 'Add a holding', icon: 'trending-up', onSelect: () => setModal('position') }] : []),
            { label: 'Keep a statement or contract', icon: 'file', onSelect: () => setModal('attach') },
            'sep' as const,
            { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setModal('delete') },
          ]}><Icon name="more" size={16} /></MenuButton>
        </div>
      </div>

      {a.linked_error && <div className="callout warn"><Icon name="alert" size={15} /><span>{SOURCE_NAME[app]} did not answer: {a.linked_error}. The last value read is shown.</span></div>}

      <div className="tiles">
        <div className="tile static"><div className="tile-head">{a.debt ? 'Still owed' : 'Worth'}{a.share < 1 && ` (your ${Math.round(a.share * 100)}%)`}</div>
          <div className="tile-val tnum">{a.known ? money(a.mine, a.currency, true) : '—'}</div>
          <div className="muted small">{a.currency !== cur && a.known ? `${money(a.value_base, cur, true)} · ` : ''}{a.share < 1 ? `all of it ${money(a.value, a.currency, true)} · ` : ''}{MODE_NAME[a.value_mode]}{a.updated ? `, ${fmtDate(a.updated)}` : ''}</div>
        </div>
        {a.gain != null && <div className="tile static"><div className="tile-head">{a.kind === 'vehicle' ? 'Since you bought it' : 'Gain'}</div>
          <div className={`tile-val tnum ${a.gain >= 0 ? 'ok' : 'bad'}`}>{signed(a.gain, a.currency)}</div>
          <div className="muted small">{pct(a.gain_pct)} on {money(a.cost!, a.currency, true)} {a.value_mode === 'positions' ? 'paid' : a.invested != null ? 'put in' : 'paid'}</div>
        </div>}
        {!closed && <div className="tile static"><div className="tile-head">This month</div>
          <div className={`tile-val tnum ${(a.debt ? -a.month_change : a.month_change) >= 0 ? 'ok' : 'bad'}`}>{signed(a.debt ? -a.month_change : a.month_change, cur)}</div>
          <div className="muted small">{a.debt ? 'less owed is good' : `in ${cur}`}</div>
        </div>}
        {a.matures_on && <div className="tile static"><div className="tile-head">{a.debt ? 'Fixed rate ends' : 'Matures'}</div><div className="tile-val">{fmtDate(a.matures_on)}</div><div className="muted small">{a.days_to_mature != null ? (a.days_to_mature >= 0 ? `in ${a.days_to_mature} days` : 'passed') : ''}</div></div>}
      </div>

      {hist.length >= 2 && <Card title={a.debt ? 'What was owed' : 'Its value over time'}>
        <LineChart labels={hist.map(p => p.label)} series={[{ key: 'v', label: a.name, values: hist.map(p => p.value) }]} fmt={v => money(v, a.currency, true)} fmtAxis={v => moneyShort(v, a.currency)} height={180}
          ariaLabel={`${a.name} from ${hist[0].label} to today`} tip={i => <><b>{hist[i].label === 'Today' ? 'Today' : fmtDate(hist[i].date)}</b><span className="tnum">{money(hist[i].value, a.currency, true)}</span>{a.currency !== cur && <span className="muted small tnum">{money(hist[i].value_base, cur, true)}</span>}</>} />
      </Card>}

      {a.debt && !closed && <PayoffCard a={a} />}

      {(a.value_mode === 'positions' || data.positions.length > 0) && <Card title="Holdings" sub={a.value_mode === 'positions' ? 'Units × price make up its value' : 'Not counted: its value is typed in'} flush actions={<div className="row">
        {data.positions.some(p => p.price_auto && p.symbol) && <button type="button" className="btn ghost sm" disabled={updating} onClick={prices}><Icon name="refresh" size={13} />Prices</button>}
        <button type="button" className="btn ghost sm" onClick={() => setModal('position')}><Icon name="plus" size={13} />Holding</button></div>}>
        {data.positions.length === 0 ? <p className="muted pad">Add the funds, shares or coins in it: how many units and the price.</p>
          : <div className="list">{data.positions.map(p => <PositionRow key={p.id} p={p} onClick={() => setEditPos(p.id)} />)}</div>}
      </Card>}

      <div className="two-grid">
        <div className="stack">
          {data.activity.length > 0 && <Card title="Activity" flush actions={<button type="button" className="btn ghost sm" onClick={() => setModal('activity')}><Icon name="plus" size={13} />Record</button>}>
            <div className="list">{data.activity.slice(0, 30).map(x => <ActivityRow key={x.id} x={x} onDelete={() => delAct(x.id)} />)}</div>
          </Card>}
          <Card title="Values" sub={vals.length ? `${vals.length} kept${vals.some(v => v.source === 'auto') ? ', some written by Wealth itself' : ''}` : undefined} flush>
            {vals.length === 0 ? <p className="muted pad">{a.value_mode === 'estimate' ? 'Estimated from what you paid.' : 'No value yet.'}</p>
              : <div className="list">{vals.slice(0, 24).map((v, i) => {
                const prev = vals[i + 1]
                return <div key={v.id} className="list-row">
                  <span className="grow row-main"><span className="row-title">{fmtDate(v.date)}</span><span className="muted small ellipsis">{[v.source === 'auto' ? 'by itself' : SOURCE_NAME[v.source] && v.source !== 'web' ? SOURCE_NAME[v.source] : '', v.note].filter(Boolean).join(' · ') || ' '}</span></span>
                  {prev && <span className={`small tnum hide-xs ${(a.debt ? prev.value - v.value : v.value - prev.value) >= 0 ? 'ok' : 'bad'}`}>{signed(v.value - prev.value, a.currency)}</span>}
                  <span className="tnum row-amount">{money(v.value, a.currency, true)}</span>
                  <button type="button" className="btn ghost icon-only sm" aria-label="Delete this value" title="Delete" onClick={() => delVal(v.id)}><Icon name="trash" size={14} /></button>
                </div>
              })}</div>}
          </Card>
        </div>
        <div className="stack">
          <Card title="Details">
            <dl className="set-dl">
              <dt>Kind</dt><dd>{k.label}</dd>
              <dt>Its value</dt><dd>{MODE_NAME[a.value_mode]}{a.value_mode === 'estimate' && ` (${pct(a.growth ?? (a.rate && (a.kind === 'deposit' || a.kind === 'savings') ? a.rate : k.growth))} a year)`}</dd>
              {a.rate > 0 && <><dt>Interest</dt><dd>{a.rate}% a year</dd></>}
              {a.payment > 0 && <><dt>{a.debt ? 'Monthly payment' : 'You add each month'}</dt><dd className="tnum">{money(a.payment, a.currency)}</dd></>}
              {a.bought_price != null && <><dt>Paid</dt><dd className="tnum">{money(a.bought_price, a.currency, true)}{a.bought_on && ` on ${fmtDate(a.bought_on)}`}</dd></>}
              {a.invested != null && a.value_mode !== 'positions' && <><dt>Put in</dt><dd className="tnum">{money(a.invested, a.currency, true)}</dd></>}
              {a.share < 1 && <><dt>Your share</dt><dd>{Math.round(a.share * 100)}%</dd></>}
              {!a.include && <><dt>Net worth</dt><dd>Not counted</dd></>}
              {app && <><dt>Comes from</dt><dd>{a.source_url ? <a href={a.source_url} target="_blank" rel="noreferrer">{SOURCE_NAME[app]} <Icon name="external" size={12} /></a> : SOURCE_NAME[app]}{a.linked_at && a.value_mode === 'linked' && <span className="muted small"> · read {fmtDate(a.linked_at.slice(0, 10))}</span>}</dd></>}
            </dl>
            {a.notes && <p className="notes">{a.notes}</p>}
          </Card>
          <Card title="Statements and contracts" flush actions={<button type="button" className="btn ghost sm" onClick={() => setModal('attach')}><Icon name="plus" size={13} />Keep</button>}>
            {data.attachments.length === 0 ? <p className="muted pad">Keep the loan contract or the last statement with it, from Pocket Documents or as a link.</p>
              : <div className="list">{data.attachments.map(t => <div key={t.id} className="list-row">
                <span className="list-ico"><Icon name="file" size={15} /></span>
                <a href={t.url} target="_blank" rel="noreferrer" className="grow row-main"><span className="row-title"><span className="ellipsis">{t.title}</span></span><span className="muted small">{t.document_id ? 'Pocket Documents' : 'Link'}</span></a>
                <button type="button" className="btn ghost icon-only sm" aria-label="Remove" onClick={() => delAtt(t.id)}><Icon name="x" size={14} /></button>
              </div>)}</div>}
          </Card>
        </div>
      </div>

      {modal === 'edit' && <AssetModal asset={a} onClose={() => setModal('')} />}
      {modal === 'value' && <ValueModal asset={a} onClose={() => setModal('')} />}
      {modal === 'read' && <ReadStatementModal asset={a} onClose={() => setModal('')} />}
      {modal === 'activity' && <ActivityModal asset={a} positions={data.positions} onClose={() => setModal('')} />}
      {modal === 'position' && <PositionModal assetId={a.id} onClose={() => setModal('')} />}
      {modal === 'attach' && <AttachModal asset={a} onClose={() => setModal('')} />}
      {editPos && <PositionModal position={data.positions.find(p => p.id === editPos)} onClose={() => setEditPos(null)} />}
      {modal === 'delete' && <Confirm title={`Delete ${a.name}?`} message={<>Its values, holdings and activity go too. {a.debt ? 'Paid off?' : 'Sold it?'} Set the date under Edit instead and its history stays.{app ? ` It will not be offered again from ${SOURCE_NAME[app]}.` : ''}</>} confirmLabel="Delete" onCancel={() => setModal('')} onConfirm={del} />}
    </div>
  )
}
