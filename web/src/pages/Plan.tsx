// The plan: where net worth is heading at today's saving, when it reaches
// financial independence (or your own target), how long the cash at hand lasts.
import React, { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useOnChange } from '../lib/data'
import { fmtDate } from '../lib/format'
import { money, moneyShort, parseAmount, pct, toMajor, toMinor } from '../lib/wealth'
import type { Plan as PlanT, Settings } from '../lib/types'
import { Card, CoreHint, Field, Loading, Switch, errMsg, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { LineChart } from '../components/Chart'

export default function Plan() {
  const { settings, setSettings } = useApp()
  const { toast } = useToast()
  const { data: p, reload, error } = useAsync(() => api.get<PlanT>(`${V1}/plan`), [])
  useOnChange(reload)
  const [f, setF] = useState<Record<string, string>>({})
  const [auto, setAuto] = useState(true)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!settings || !p) return
    const c = p.currency
    setAuto(settings.plan_saving_auto)
    setF({
      saving: settings.plan_saving ? String(toMajor(settings.plan_saving, c)) : '', spending: settings.plan_spending ? String(toMajor(settings.plan_spending, c)) : '',
      ret: settings.plan_return ? String(settings.plan_return) : '', inflation: String(settings.plan_inflation), withdraw: String(settings.plan_withdraw),
      target: settings.plan_target ? String(toMajor(settings.plan_target, c)) : '', birth: settings.plan_birth_year ? String(settings.plan_birth_year) : '',
    })
  }, [settings, p?.currency]) // eslint-disable-line react-hooks/exhaustive-deps
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error}</div>
  if (!p || !settings) return <Loading />
  const cur = p.currency
  const coreOn = settings.core_configured && settings.core_registered !== false
  const save = async (e?: React.FormEvent) => {
    e?.preventDefault()
    const num = (s: string) => (s.trim() ? parseAmount(s) : 0)
    const body: Partial<Settings> = {
      plan_saving_auto: auto, plan_saving: toMinor(num(f.saving) || 0, cur), plan_spending: toMinor(num(f.spending) || 0, cur), plan_return: num(f.ret) || 0,
      plan_inflation: num(f.inflation) ?? 2.5, plan_withdraw: num(f.withdraw) || 4, plan_target: toMinor(num(f.target) || 0, cur), plan_birth_year: Number(f.birth) || 0,
    }
    setBusy(true)
    try { setSettings(await api.put<Settings>(`${V1}/settings`, body)); toast('Plan updated'); reload() } catch (err) { toast(errMsg(err), 'err') } finally { setBusy(false) }
  }
  const yrs = p.years
  const showYears = yrs.slice(0, Math.min(yrs.length, Math.max(11, Math.ceil(p.reach_years > 0 ? p.reach_years + 6 : 25))))
  const fromMoney = p.money.used
  const reach = p.reach_on
  return (
    <div className="stack loose">
      <div className="page-head">
        <div className="grow"><h1 className="page-title">The plan</h1>
          <p className="muted">{reach ? <>At this pace you reach <b>{money(p.target, cur, true)}</b> {p.reach_years === 0 ? 'already' : <>in <b>{p.reach_years} years</b> ({fmtDate(reach)}{p.reach_age ? `, at ${p.reach_age}` : ''})</>}.</>
            : p.target > 0 ? <>At this pace the target of {money(p.target, cur, true)} is more than 40 years away.</> : 'Tell it what you spend a month to see when you could live from what you own.'}</p>
        </div>
      </div>

      <div className="tiles">
        <div className="tile static"><div className="tile-head">{p.target_custom ? 'Your target' : 'Financial independence'}</div><div className="tile-val tnum">{p.target ? money(p.target, cur, true) : '—'}</div>
          <div className="muted small">{p.target_custom ? 'set below' : p.spending ? `${money(p.spending * 12, cur, true)} a year ÷ ${p.withdraw}%` : 'needs your spending'}</div></div>
        <div className="tile static"><div className="tile-head">On the way</div><div className="tile-val tnum">{p.target ? pct(Math.min(100, (p.net / p.target) * 100), false) : '—'}</div><div className="muted small">{money(p.net, cur, true)} now</div></div>
        <div className="tile static"><div className="tile-head">Would pay you</div><div className="tile-val tnum">{money(p.passive_month, cur, true)}</div><div className="muted small">a month, at {p.withdraw}% a year</div></div>
        {p.emergency_months != null && <div className="tile static"><div className="tile-head">Cash at hand lasts</div><div className="tile-val tnum">{p.emergency_months} months</div><div className="muted small">{money(p.liquid, cur, true)} · 3–6 is a good cushion</div></div>}
      </div>

      <Card title="Where it is heading" sub={`${money(p.saving, cur, true)} saved a month, growing ${p.return}% a year${p.return_mix === p.return ? ' (the mix of what you own)' : ''}`}>
        <LineChart labels={showYears.map(y => String(y.year))} series={[{ key: 'n', label: 'Net worth', values: showYears.map(y => y.nominal) }, { key: 'r', label: "In today's money", values: showYears.map(y => y.real), dashed: true }]}
          refLine={p.target ? { value: p.target, label: p.target_custom ? 'Target' : 'Independence' } : undefined} area={false}
          fmt={v => money(v, cur, true)} fmtAxis={v => moneyShort(v, cur)} ariaLabel="Projected net worth by year"
          tip={i => <><b>{showYears[i].year}{showYears[i].age ? ` · age ${showYears[i].age}` : ''}</b><span className="tnum">{money(showYears[i].nominal, cur, true)}</span><span className="muted small tnum">{money(showYears[i].real, cur, true)} in today's money</span></>} />
        <div className="legend small muted mt"><span><i className="lg-solid" />Net worth</span><span><i className="lg-dash" />In today's money ({p.inflation}% inflation)</span>{p.target > 0 && <span><i className="lg-ref" />{p.target_custom ? 'Your target' : 'Financial independence'}</span>}</div>
      </Card>

      <div className="two-grid">
        <Card title="Your numbers" sub="Change any of them and see the plan move">
          <form onSubmit={save} className="grid form-grid">
            <div className="setting-row-inline">
              <Switch checked={auto && settings.money_mode !== 'off'} disabled={settings.money_mode === 'off'} onChange={setAuto} label="From Pocket Money" />
              <span className="small">Saving and spending from Pocket Money{fromMoney && p.money.months ? `: the average of the last ${p.money.months} months` : ''}</span>
            </div>
            {auto && !fromMoney && <p className="small muted">{p.money.error ? `Pocket Money: ${p.money.error}. The numbers below are used meanwhile.` : 'Pocket Money has no months to average yet; the numbers below are used.'}{!coreOn && <CoreHint />}</p>}
            {fromMoney && <p className="small">In {money(p.money.income || 0, cur, true)} · out {money(p.money.spending || 0, cur, true)} · saved <b>{money(p.money.saving || 0, cur, true)}</b> a month</p>}
            <div className="two-col tight">
              <Field label="Saved a month"><input inputMode="decimal" disabled={fromMoney} value={f.saving || ''} onChange={e => setF({ ...f, saving: e.target.value })} placeholder="0" /></Field>
              <Field label="Spent a month"><input inputMode="decimal" disabled={fromMoney} value={f.spending || ''} onChange={e => setF({ ...f, spending: e.target.value })} placeholder="0" /></Field>
            </div>
            <div className="three-col">
              <Field label="Growth a year" help={`Empty = ${p.return_mix}%, the mix`}><div className="input-affix"><input inputMode="decimal" value={f.ret || ''} onChange={e => setF({ ...f, ret: e.target.value })} placeholder={String(p.return_mix)} /><span className="affix">%</span></div></Field>
              <Field label="Inflation"><div className="input-affix"><input inputMode="decimal" value={f.inflation || ''} onChange={e => setF({ ...f, inflation: e.target.value })} /><span className="affix">%</span></div></Field>
              <Field label="Withdrawal" help="The 4% rule"><div className="input-affix"><input inputMode="decimal" value={f.withdraw || ''} onChange={e => setF({ ...f, withdraw: e.target.value })} /><span className="affix">%</span></div></Field>
            </div>
            <div className="two-col tight">
              <Field label="Own target" help="Empty = financial independence"><div className="input-affix"><input inputMode="decimal" value={f.target || ''} onChange={e => setF({ ...f, target: e.target.value })} placeholder={p.fi_number ? String(Math.round(toMajor(p.fi_number, cur))) : ''} /><span className="affix">{cur}</span></div></Field>
              <Field label="Born in" help="To show your age"><input inputMode="numeric" value={f.birth || ''} onChange={e => setF({ ...f, birth: e.target.value })} placeholder="1985" maxLength={4} /></Field>
            </div>
            <div className="form-actions"><span className="grow" /><button className="btn primary" disabled={busy}>{busy ? 'Saving…' : 'Update the plan'}</button></div>
          </form>
        </Card>
        <div className="stack">
          <Card title="Milestones">
            <div className="milestones">
              {p.milestones.map(m => <div key={m.amount} className={`ms ${m.next ? 'next' : 'done'}`}><Icon name={m.next ? 'milestone' : 'check-circle'} size={15} /><span className="tnum ms-amt">{moneyShort(m.amount, cur)}</span>
                <span className="muted small">{m.next ? `${money(m.amount - p.net, cur, true)} to go` : m.reached_on === p.first_day ? 'before the history starts' : fmtDate(m.reached_on!)}</span></div>)}
            </div>
          </Card>
          <Card title="How it is worked out">
            <ul className="explain small muted">
              <li>What you own grows by its expected change a year (set on each asset; a home 2%, investments 6%, a car −15%…), here {p.return}% on the whole.</li>
              <li>Each month the saving is added and each debt is paid down by its payment, with its interest.</li>
              <li>Financial independence: when what you own, taking {p.withdraw}% a year, covers what you spend.</li>
              <li>A rough guide, not advice: markets move, and so will your numbers. <Link to="/assets">Keep the values fresh</Link> and it stays honest.</li>
            </ul>
          </Card>
        </div>
      </div>
    </div>
  )
}
