// Overview: net worth now and how it moved, what makes it up, what needs an update, what matures soon, the milestones.
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useOnChange } from '../lib/data'
import { fmtDate } from '../lib/format'
import { inDays, money, moneyShort, pct, signed } from '../lib/wealth'
import type { Group, Overview as OverviewT, Point } from '../lib/types'
import { Card, Loading, Segmented, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { BarList, LineChart } from '../components/Chart'
import { AssetIcon, AssetRow } from '../components/Rows'
import { openCreate } from '../components/Layout'

const RANGES = [{ value: 12, label: '1Y' }, { value: 36, label: '3Y' }, { value: 60, label: '5Y' }, { value: 600, label: 'All' }]
const GROUP_KIND: Record<Group, string> = { cash: 'cash', investments: 'investment', pension: 'pension', property: 'property', vehicles: 'vehicle', valuables: 'valuables', other: 'other', debts: 'loan' }

export default function Overview() {
  const { user } = useApp()
  const navigate = useNavigate()
  const [range, setRange] = useState(() => Number(localStorage.getItem('pwl-range')) || 36)
  const { data: o, reload, error } = useAsync(() => api.get<OverviewT>(`${V1}/overview`), [])
  const { data: h, reload: reloadH } = useAsync(() => api.get<{ points: Point[] }>(`${V1}/history?months=${range}`), [range])
  useOnChange(() => { reload(); reloadH() })
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error}</div>
  if (!o) return <Loading />
  const cur = o.currency
  const first = (user?.name || '').split(' ')[0]
  if (o.open === 0) return (
    <div className="card welcome">
      <div className="welcome-body">
        <img src="/favicon.svg" alt="" width={56} height={56} />
        <div className="grow">
          <h2>Welcome to Pocket Wealth</h2>
          <p className="muted">Everything you own and owe, in one number: your accounts and savings, investments, pension, home and car, minus what you still owe. Update the values now and then and you see how your net worth moves, what each investment gained, when each loan is paid off and where it is all heading.</p>
          <p className="muted">Start by bringing in what your other Pocket apps already know (your Money accounts, your Garage vehicles, your homes), or add the first one yourself.</p>
          <div className="row wrap mt">
            <Link to="/connect" className="btn primary"><Icon name="layers" size={15} />From my apps</Link>
            <button type="button" className="btn" onClick={() => openCreate('asset')}><Icon name="plus" size={15} />Add one</button>
          </div>
        </div>
      </div>
    </div>
  )
  const pts = h?.points || []
  const next = o.milestones.find(m => m.next)
  const reached = o.milestones.filter(m => !m.next)
  const assetGroups = o.groups.filter(g => g.group !== 'debts')
  const debts = o.groups.find(g => g.group === 'debts')
  const chg = (v: number) => (v >= 0 ? 'ok' : 'bad')
  const owned = o.open - (o.counts.debts || 0)
  return (
    <div className="stack loose">
      <div className="page-head">
        <div className="grow">
          <p className="muted">{first && first !== 'Admin' ? `${first}, your net worth` : 'Your net worth'}</p>
          <div className="hero-num tnum">{money(o.net, cur, true)}</div>
          <div className="hero-sub">
            <span className={`chg ${chg(o.month.amount)}`}><Icon name={o.month.amount >= 0 ? 'trending-up' : 'trending-down'} size={14} />{signed(o.month.amount, cur)}{o.month.pct != null && ` (${pct(o.month.pct)})`}</span><span className="muted">this month</span>
            <span className="sep" aria-hidden="true">·</span>
            <span className={`chg ${chg(o.year.amount)}`}>{signed(o.year.amount, cur)}{o.year.pct != null && ` (${pct(o.year.pct)})`}</span><span className="muted">this year</span>
          </div>
        </div>
      </div>

      {o.missing_rates.length > 0 && <div className="callout warn"><Icon name="alert" size={15} /><span>There is no exchange rate for {o.missing_rates.join(', ')}, so those are left out of the totals. <Link to="/settings/general">Add a rate in Settings</Link>.</span></div>}

      <Card title="Net worth over time" actions={<Segmented size="sm" value={range} onChange={v => { setRange(v); localStorage.setItem('pwl-range', String(v)) }} options={RANGES} label="Range" />}>
        {pts.length < 2 ? <p className="muted">The history grows from here: every value you add, and every month, is a point.</p> :
          <LineChart labels={pts.map(p => p.label)} series={[{ key: 'net', label: 'Net worth', values: pts.map(p => p.net) }]} fmt={v => money(v, cur, true)} fmtAxis={v => moneyShort(v, cur)}
            ariaLabel={`Net worth from ${pts[0].label} to today: ${money(pts[0].net, cur, true)} to ${money(o.net, cur, true)}`}
            tip={i => <><b>{pts[i].label === 'Today' ? 'Today' : fmtDate(pts[i].date)}</b><span className="tnum">{money(pts[i].net, cur, true)}</span><span className="muted small tnum">own {moneyShort(pts[i].assets, cur)} · owe {moneyShort(pts[i].debts, cur)}</span></>} />}
      </Card>

      <div className="tiles">
        <Link to="/assets" className="tile"><div className="tile-head"><span className="tile-ico"><Icon name="wallet" size={14} /></span>You own</div><div className="tile-val tnum">{money(o.assets, cur, true)}</div><div className="muted small">{owned} {owned === 1 ? 'thing' : 'things'}</div></Link>
        <Link to="/debts" className="tile"><div className="tile-head"><span className="tile-ico"><Icon name="landmark" size={14} /></span>You owe</div><div className="tile-val tnum">{money(o.debts, cur, true)}</div><div className="muted small">{o.debt_ratio != null && o.debts > 0 ? `${pct(o.debt_ratio, false)} of what you own` : 'no debts'}</div></Link>
        <Link to="/plan" className="tile"><div className="tile-head"><span className="tile-ico"><Icon name="piggy" size={14} /></span>At hand</div><div className="tile-val tnum">{money(o.liquid, cur, true)}</div><div className="muted small">cash and savings</div></Link>
        <Link to="/plan" className="tile"><div className="tile-head"><span className="tile-ico"><Icon name="milestone" size={14} /></span>Next milestone</div><div className="tile-val tnum">{next ? moneyShort(next.amount, cur) : '—'}</div><div className="muted small">{next ? `${money(next.amount - o.net, cur, true)} to go` : 'all reached'}</div></Link>
      </div>

      <div className="two-grid">
        <Card title="What it is made of" sub="Your part of each, in today's money">
          <BarList fmt={v => money(v, cur, true)} rows={assetGroups.map(g => ({
            key: g.group, label: <Link to={`/assets?group=${g.group}`}>{g.label}</Link>, value: g.value,
            icon: <AssetIcon a={{ kind: GROUP_KIND[g.group], group: g.group }} size={14} />,
            sub: <>{pct(g.pct, false)} of what you own · {g.count} {g.count === 1 ? 'item' : 'items'}{g.month_change ? <> · <span className={chg(g.month_change)}>{signed(g.month_change, cur)}</span> this month</> : null}</>,
          }))} />
          {debts && <div className="bl-debts"><span className="muted">Minus what you owe</span><Link to="/debts" className="tnum">−{money(debts.value, cur, true)}</Link></div>}
        </Card>
        <div className="stack">
          {o.linked_errors.length > 0 && <Card title="Links that need a look" sub="The app these come from did not answer" flush>
            <div className="list">{o.linked_errors.map(a => <AssetRow key={a.id} a={a} base={cur} showChange={false} />)}</div>
          </Card>}
          {o.maturing.length > 0 && <Card title="Coming up" sub="Deposits that mature, fixed rates that end, money due back" flush>
            <div className="list">{o.maturing.map(a => (
              <Link key={a.id} to={`/a/${a.id}`} className="list-row">
                <AssetIcon a={a} /><span className="grow row-main"><span className="row-title"><span className="ellipsis">{a.name}</span></span><span className="muted small">{fmtDate(a.matures_on)}</span></span>
                <span className={`pill ${(a.days_to_mature ?? 99) <= 30 ? 'st-soon' : 'st-ok'}`}><Icon name="calendar" size={12} />{inDays(a.days_to_mature ?? 0)}</span>
              </Link>))}</div>
          </Card>}
          <Card title="Needs an update" sub={o.stale.length ? 'Typed in a while ago: what are they worth now?' : undefined} flush>
            {o.stale.length === 0 ? <p className="muted pad">Every value is recent.</p>
              : <div className="list">{o.stale.map(a => (
                <button key={a.id} type="button" className="list-row as-btn" onClick={() => navigate(`/a/${a.id}?update=1`)}>
                  <AssetIcon a={a} /><span className="grow row-main"><span className="row-title"><span className="ellipsis">{a.name}</span></span><span className="muted small">{a.updated ? `last ${fmtDate(a.updated)}` : 'no value yet'}</span></span>
                  <span className="tnum row-amount">{money(a.mine, a.currency, true)}</span><Icon name="edit" size={14} className="muted" />
                </button>))}</div>}
          </Card>
          {reached.length > 0 && <Card title="Milestones" actions={<Link to="/plan" className="btn ghost sm">The plan</Link>}>
            <div className="milestones">
              {o.milestones.slice(-4).map(m => <div key={m.amount} className={`ms ${m.next ? 'next' : 'done'}`}>
                <Icon name={m.next ? 'milestone' : 'check-circle'} size={15} /><span className="tnum ms-amt">{moneyShort(m.amount, cur)}</span>
                <span className="muted small">{m.next ? `${pct(Math.min(99.9, (o.net / m.amount) * 100), false)} of the way` : m.reached_on === o.first_day ? 'before the history starts' : fmtDate(m.reached_on!)}</span>
              </div>)}
            </div>
          </Card>}
        </div>
      </div>
    </div>
  )
}
