// From my apps: what Pocket Money, Garage, Home and Inventory already know, to add as linked assets.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useOnChange } from '../lib/data'
import { SOURCE_ICON, kindOf, money } from '../lib/wealth'
import type { AppSuggestions, Suggestion } from '../lib/types'
import { Card, CoreHint, Loading, errMsg, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'

const WHAT: Record<string, string> = {
  money: 'Accounts, cards and loans, with their balances as they change.',
  garage: 'Vehicles, valued from what you paid and how old they are; closed when sold.',
  home: 'Your homes as property; you add what each is worth.',
  inventory: 'The valuable things you own, at what they cost.',
}

export default function Connect() {
  const { settings } = useApp()
  const { toast } = useToast()
  const { data, reload, error } = useAsync(() => api.get<AppSuggestions[]>(`${V1}/suggestions`), [])
  useOnChange(reload)
  const [busy, setBusy] = useState('')
  const [shown, setShown] = useState<Record<string, boolean>>({})
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error}</div>
  if (!data || !settings) return <Loading />
  const coreOn = settings.core_configured && settings.core_registered !== false
  const add = async (refs: string[]) => {
    setBusy(refs.join(','))
    try { const r = await api.post<{ added: number[] }>(`${V1}/suggestions/add`, { refs }); toast(`${r.added.length} added`); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  const dismiss = async (s: Suggestion, restore = false) => {
    try { await api.post(`${V1}/suggestions/dismiss`, { ref: s.ref, title: s.title, restore }); reload() } catch (e) { toast(errMsg(e), 'err') }
  }
  const refresh = async () => {
    setBusy('refresh')
    try { const r = await api.post<{ changed: number }>(`${V1}/linked/refresh`); toast(r.changed ? `${r.changed} values changed` : 'Everything is up to date'); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  return (
    <div className="stack loose">
      <div className="page-head">
        <div className="grow"><h1 className="page-title">From my apps</h1><p className="muted">What your other Pocket apps already know. Added ones stay linked and keep themselves up to date.</p></div>
        <button type="button" className="btn" disabled={busy === 'refresh'} onClick={refresh}><Icon name="refresh" size={14} />Read again</button>
      </div>
      {data.map(g => {
        const fresh = g.items.filter(i => !i.asset_id && !i.dismissed)
        const done = g.items.filter(i => i.asset_id)
        const hidden = g.items.filter(i => !i.asset_id && i.dismissed)
        return (
          <Card key={g.app} title={<span className="row"><span className={`list-ico sm app-${g.app}`}><Icon name={SOURCE_ICON[g.app] || 'layers'} size={14} /></span>{g.name}</span>} sub={WHAT[g.app]} flush
            actions={fresh.length > 1 ? <button type="button" className="btn ghost sm" disabled={!!busy} onClick={() => add(fresh.map(i => i.ref))}><Icon name="plus" size={13} />Add all {fresh.length}</button> : undefined}>
            {g.error ? <div className="pad"><div className="conn-status bad"><span className="dot" /><span>{g.error}{!g.connected && <> · <Link to="/settings/apps">Connected apps</Link></>}</span></div>{!coreOn && !g.connected && <CoreHint />}</div>
              : g.items.length === 0 ? <p className="muted pad">Nothing there yet.</p>
                : <div className="list">
                  {[...fresh, ...done].map(s => (
                    <div key={s.ref} className="list-row">
                      <span className={`list-ico gr-${kindOf(s.kind).group}`}><Icon name={kindOf(s.kind).icon} size={15} /></span>
                      <span className="grow row-main"><span className="row-title"><span className="ellipsis">{s.title}</span></span><span className="muted small ellipsis">{s.subtitle}</span></span>
                      {s.value != null && <span className="tnum row-amount hide-xs">{kindOf(s.kind).debt ? '−' : ''}{money(s.value, s.currency, true)}</span>}
                      {s.asset_id ? <Link to={`/a/${s.asset_id}`} className="btn sm"><Icon name="check" size={13} />Added</Link> : <>
                        <button type="button" className="btn ghost icon-only sm" title="Not this one" aria-label={`Not ${s.title}`} onClick={() => dismiss(s)}><Icon name="x" size={14} /></button>
                        <button type="button" className="btn sm primary" disabled={!!busy} onClick={() => add([s.ref])}><Icon name="plus" size={13} />Add</button>
                      </>}
                    </div>))}
                  {hidden.length > 0 && <div className="list-row">
                    <span className="grow muted small">{hidden.length} turned down{shown[g.app] ? ':' : ''} {shown[g.app] && hidden.map(h => <button key={h.ref} type="button" className="link-btn small" onClick={() => dismiss(h, true)}>{h.title} ↺</button>)}</span>
                    {!shown[g.app] && <button type="button" className="btn ghost sm" onClick={() => setShown({ ...shown, [g.app]: true })}>Show</button>}
                  </div>}
                </div>}
          </Card>
        )
      })}
      <p className="muted small">A Pocket Money account counts at its balance; tick “include in net worth” there to have it pre-selected. Inventory items count when they are worth at least {settings.inventory_min} {settings.currency} each (Settings → General).</p>
    </div>
  )
}
