// Debts: what is owed, what it costs each month, when each is paid off.
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useOnChange } from '../lib/data'
import { fmtDate } from '../lib/format'
import { kindOf, money, months } from '../lib/wealth'
import type { Asset, Payoff } from '../lib/types'
import { Card, Empty, Loading, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { AssetIcon } from '../components/Rows'
import { openCreate } from '../components/Layout'

interface Data { currency: string; debts: (Asset & { payoff: Payoff })[]; total: number; monthly: number; interest_month: number }

export default function Debts() {
  const { data, reload, error } = useAsync(() => api.get<Data>(`${V1}/debts`), [])
  useOnChange(reload)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error}</div>
  if (!data) return <Loading />
  const cur = data.currency
  const last = data.debts.filter(d => d.payoff.months > 0).sort((a, b) => b.payoff.months - a.payoff.months)[0]
  const top = data.debts.find(d => d.rate > 0)
  return (
    <div className="stack loose">
      <div className="page-head">
        <div className="grow"><h1 className="page-title">Debts</h1><p className="muted">What you still owe and when it is paid off</p></div>
        <button type="button" className="btn primary" onClick={() => openCreate('debt')}><Icon name="plus" size={15} />Debt</button>
      </div>
      {data.debts.length === 0 ? <Empty title="No debts">Nothing owed. If there is a mortgage, a loan or a card balance, add it to see when it is paid off.</Empty> : <>
        <div className="tiles">
          <div className="tile static"><div className="tile-head">Owed</div><div className="tile-val tnum">{money(data.total, cur, true)}</div><div className="muted small">{data.debts.length} {data.debts.length === 1 ? 'debt' : 'debts'}</div></div>
          <div className="tile static"><div className="tile-head">Payments a month</div><div className="tile-val tnum">{money(data.monthly, cur, true)}</div><div className="muted small">set on each debt</div></div>
          <div className="tile static"><div className="tile-head">Interest this month</div><div className="tile-val tnum">{money(data.interest_month, cur, true)}</div><div className="muted small">about</div></div>
          {last && <div className="tile static"><div className="tile-head">Debt-free</div><div className="tile-val">{fmtDate(last.payoff.paid_off_on)}</div><div className="muted small">at today's payments</div></div>}
        </div>
        {top && data.debts.length > 1 && <div className="callout info"><Icon name="info" size={15} /><span>Paying extra? It saves the most on <b>{top.name}</b>, the highest rate ({top.rate}%). Open a debt to try an extra payment.</span></div>}
        <Card title="Each debt" sub="Highest interest first" flush>
          <div className="list">{data.debts.map(d => (
            <Link key={d.id} to={`/a/${d.id}`} className="list-row">
              <AssetIcon a={d} />
              <span className="grow row-main">
                <span className="row-title"><span className="ellipsis">{d.name}</span></span>
                <span className="muted small ellipsis">{[kindOf(d.kind).label, d.rate ? `${d.rate}%` : 'no rate set', d.payment ? `${money(d.payment, d.currency, true)} a month` : 'no payment set'].join(' · ')}</span>
              </span>
              <span className="row-num">
                <span className="tnum row-amount">{money(d.mine, d.currency, true)}</span>
                <span className="muted small">{d.payoff.months > 0 ? `paid off ${fmtDate(d.payoff.paid_off_on)}` : d.payment ? months(-1) : ''}</span>
              </span>
            </Link>))}</div>
        </Card>
      </>}
    </div>
  )
}
