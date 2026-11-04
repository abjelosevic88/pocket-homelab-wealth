// Investments: every holding with what it gained, the mix, income from dividends and interest, and all the activity.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { announceChange, useOnChange } from '../lib/data'
import { POSITION_KIND_NAME, money, pct, signed } from '../lib/wealth'
import type { Activity, Position } from '../lib/types'
import { Card, Empty, Loading, errMsg, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { BarList } from '../components/Chart'
import { ActivityRow, PositionRow } from '../components/Rows'
import { PositionModal } from '../components/Forms'

interface Data { currency: string; positions: Position[]; value: number; cost: number; gain: number; by_kind: Record<string, number>; income_year: number; online: boolean; missing_rates: string[] }

export default function Investments() {
  const { toast } = useToast()
  const { data, reload, error } = useAsync(() => api.get<Data>(`${V1}/positions`), [])
  const { data: acts, reload: reloadA } = useAsync(() => api.get<Activity[]>(`${V1}/activity?limit=60`), [])
  useOnChange(() => { reload(); reloadA() })
  const [edit, setEdit] = useState<Position | 'new' | null>(null)
  const [busy, setBusy] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error}</div>
  if (!data) return <Loading />
  const cur = data.currency
  const prices = async () => {
    setBusy(true)
    try { const r = await api.post<{ updated: number; failed: number }>(`${V1}/prices/update`); toast(`${r.updated} prices updated${r.failed ? `, ${r.failed} not found` : ''}`); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const kinds = Object.entries(data.by_kind).sort((a, b) => b[1] - a[1])
  const auto = data.positions.some(p => p.price_auto && p.symbol)
  return (
    <div className="stack loose">
      <div className="page-head">
        <div className="grow"><h1 className="page-title">Investments</h1><p className="muted">Funds, shares, bonds and coins in all your accounts{data.online && auto ? ' · prices come in by themselves' : ''}</p></div>
        {auto && <button type="button" className="btn" disabled={busy || !data.online} title={data.online ? '' : 'Looking prices up online is off (Settings → General)'} onClick={prices}><Icon name="refresh" size={14} />{busy ? 'Updating…' : 'Update prices'}</button>}
        <button type="button" className="btn primary" onClick={() => setEdit('new')}><Icon name="plus" size={15} />Holding</button>
      </div>
      {data.positions.length === 0 ? <Empty title="No holdings yet">Add an investment account (a broker, a fund, a crypto wallet), then the funds, shares or coins in it.</Empty> : <>
        <div className="tiles">
          <div className="tile static"><div className="tile-head">Worth</div><div className="tile-val tnum">{money(data.value, cur, true)}</div><div className="muted small">{data.positions.length} holdings</div></div>
          <div className="tile static"><div className="tile-head">Paid</div><div className="tile-val tnum">{money(data.cost, cur, true)}</div><div className="muted small">what they cost</div></div>
          <div className="tile static"><div className="tile-head">Gain</div><div className={`tile-val tnum ${data.gain >= 0 ? 'ok' : 'bad'}`}>{signed(data.gain, cur)}</div><div className="muted small">{data.cost ? pct((data.gain / data.cost) * 100) : ''}</div></div>
          <div className="tile static"><div className="tile-head">Income, 12 months</div><div className="tile-val tnum">{money(data.income_year, cur, true)}</div><div className="muted small">dividends and interest</div></div>
        </div>
        <div className="two-grid">
          <Card title="Holdings" flush>
            <div className="list">{data.positions.map(p => <PositionRow key={p.id} p={p} showAsset onClick={() => setEdit(p)} />)}</div>
          </Card>
          <div className="stack">
            <Card title="The mix" sub="By kind of holding">
              <BarList fmt={v => money(v, cur, true)} rows={kinds.map(([k, v]) => ({ key: k, label: POSITION_KIND_NAME[k] || k, value: v, sub: data.value ? `${pct((v / data.value) * 100, false)}` : undefined }))} />
            </Card>
            <Card title="Activity" sub="Buys, sells, dividends, money in and out" flush>
              {!acts ? <Loading /> : acts.length === 0 ? <p className="muted pad">Record a buy or a dividend on an account's page.</p>
                : <div className="list">{acts.slice(0, 25).map(x => <Link key={x.id} to={`/a/${x.asset_id}`} className="row-link"><ActivityRow x={x} showAsset /></Link>)}</div>}
            </Card>
          </div>
        </div>
      </>}
      {edit && <PositionModal position={edit === 'new' ? undefined : edit} onClose={() => setEdit(null)} />}
    </div>
  )
}
