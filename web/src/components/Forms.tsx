// Every add/edit dialog: an asset or debt, a new value, a holding, a buy,
// sell or dividend, a statement read by AI, a document kept with an asset.
import React, { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useData } from '../lib/data'
import { fmtDate, todayISO } from '../lib/format'
import { ACTIVITY, KINDS, MODES, POSITION_KINDS, kindOf, money, parseAmount, toMajor, toMinor } from '../lib/wealth'
import type { Activity, Asset, Doc, Position, Valuation } from '../lib/types'
import { CoreHint, Field, Loading, Modal, Segmented, errMsg, useDebounce, useToast } from './ui'
import { Select } from './Select'
import { Icon } from './icons'

function Actions({ onClose, busy, label, extra }: { onClose: () => void; busy: boolean; label: string; extra?: React.ReactNode }) {
  return <div className="form-actions">{extra}<span className="grow" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Saving…' : label}</button></div>
}

/** A money amount as typed: "" or the major units with the currency's decimals. */
const amountIn = (minor: number | null | undefined, cur: string) => (minor == null ? '' : String(toMajor(minor, cur)))

const CURRENCIES = ['EUR', 'BAM', 'USD', 'GBP', 'CHF', 'RSD', 'HRK', 'SEK', 'NOK', 'DKK', 'PLN', 'CZK', 'HUF', 'RON', 'BGN', 'TRY', 'JPY', 'CAD', 'AUD', 'CNY']

export function CurrencySelect({ value, onChange, ariaLabel = 'Currency' }: { value: string; onChange: (v: string) => void; ariaLabel?: string }) {
  const opts = Array.from(new Set([value, ...CURRENCIES].filter(Boolean))).map(c => ({ value: c, label: c }))
  return <Select ariaLabel={ariaLabel} value={value} onChange={onChange} options={opts} onCreate={c => c.toUpperCase().slice(0, 3)} createLabel="Use" />
}

function MoneyField({ label, value, onChange, cur, help, placeholder }: { label: string; value: string; onChange: (v: string) => void; cur: string; help?: React.ReactNode; placeholder?: string }) {
  return <Field label={label} help={help}><div className="input-affix"><input inputMode="decimal" value={value} onChange={e => onChange(e.target.value)} placeholder={placeholder || '0'} /><span className="affix">{cur}</span></div></Field>
}

// ---------- an asset or a debt ----------

export function AssetModal({ asset, preset, onClose, onSaved }: { asset?: Asset; preset?: { kind?: string }; onClose: () => void; onSaved?: (a: Asset) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const start: Partial<Asset> = asset || preset || {}
  const cur0 = start.currency || settings?.currency || 'EUR'
  const [f, setF] = useState({
    name: start.name || '', kind: start.kind || '', currency: cur0, institution: start.institution || '', number: start.number || '', notes: start.notes || '',
    value_mode: start.value_mode || (start.kind ? kindOf(start.kind).mode : 'manual'), include: start.include ?? true, share: String(Math.round((start.share ?? 1) * 100)),
    rate: start.rate ? String(start.rate) : '', payment: amountIn(start.payment || null, cur0), growth: start.growth == null ? '' : String(start.growth),
    matures_on: start.matures_on || '', bought_on: start.bought_on || '', bought_price: amountIn(start.bought_price, cur0), invested: amountIn(start.invested, cur0),
    value: '', date: todayISO(), closed_on: start.closed_on || '',
  })
  const [more, setMore] = useState(!!asset)
  const [busy, setBusy] = useState(false)
  const kind = kindOf(f.kind || 'other')
  const pickKind = (v: string) => {
    const k = kindOf(v)
    setF(x => ({ ...x, kind: v, value_mode: asset ? x.value_mode : k.mode, name: x.name || (v === 'other' ? '' : k.label.replace(/ or .*$/, '')) }))
  }
  const linked = !!asset?.source_ref
  const modes = MODES.filter(m => m.value !== 'linked' || linked)
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const num = (s: string) => (s.trim() ? parseAmount(s) : null)
    const fields: [string, string][] = [['value', f.value], ['bought price', f.bought_price], ['invested', f.invested], ['monthly amount', f.payment]]
    for (const [n, v] of fields) if (v.trim() && num(v) == null) { toast(`The ${n} is a number like 1500 or 1,250.50`, 'err'); return }
    const share = Number(f.share)
    if (!(share > 0 && share <= 100)) { toast('Your share is 1 to 100%', 'err'); return }
    const c = f.currency
    const body: Record<string, unknown> = {
      name: f.name, kind: f.kind || 'other', currency: c, institution: f.institution, number: f.number, notes: f.notes, value_mode: f.value_mode, include: f.include,
      share: share / 100, rate: Number(f.rate.replace(',', '.')) || 0, payment: f.payment.trim() ? toMinor(num(f.payment)!, c) : 0,
      growth: f.growth.trim() === '' ? null : Number(f.growth.replace(',', '.')), matures_on: f.matures_on, bought_on: f.bought_on,
      bought_price: f.bought_price.trim() ? toMinor(num(f.bought_price)!, c) : null, invested: f.invested.trim() ? toMinor(num(f.invested)!, c) : null,
    }
    if (asset) body.closed_on = f.closed_on
    if (!asset && f.value.trim()) { body.value = toMinor(num(f.value)!, c); body.date = f.date }
    setBusy(true)
    try {
      const r = asset ? await api.put<{ asset: Asset }>(`${V1}/assets/${asset.id}`, body) : await api.post<{ asset: Asset }>(`${V1}/assets`, body)
      toast(asset ? 'Saved' : `${r.asset.name} added`); announceChange(); onSaved?.(r.asset); onClose()
    } catch (err) { toast(errMsg(err), 'err') } finally { setBusy(false) }
  }
  const debt = kind.debt
  return (
    <Modal title={asset ? `Edit ${asset.name}` : f.kind && debt ? 'Add a debt' : 'Add something you own or owe'} onClose={onClose}>
      <form onSubmit={save} className="grid form-grid">
        <Field group label="What is it" help={f.kind ? kind.hint : undefined}>
          <Select ariaLabel="Kind" value={f.kind} onChange={pickKind} placeholder="Choose…" options={KINDS.map(k => ({ value: k.value, label: k.label, group: k.debt ? 'What you owe' : 'What you own' }))} />
        </Field>
        <div className="two-col tight">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required maxLength={200} placeholder={debt ? 'Mortgage, Car loan' : 'Savings, Broker account, Flat'} /></Field>
          <Field group label="Currency"><CurrencySelect value={f.currency} onChange={v => setF({ ...f, currency: v })} /></Field>
        </div>
        {!asset && f.value_mode !== 'positions' && <div className="two-col tight">
          <MoneyField label={debt ? 'Still owed' : f.value_mode === 'estimate' ? 'Worth now (if you know)' : 'Worth now'} value={f.value} onChange={v => setF({ ...f, value: v })} cur={f.currency} />
          <Field label="On"><input type="date" value={f.date} max={todayISO()} onChange={e => setF({ ...f, date: e.target.value })} /></Field>
        </div>}
        {!asset && f.value_mode === 'positions' && <p className="muted small">You add its holdings (funds, shares, coins) on its page next; their prices make up the value.</p>}
        <Field group label="Its value" help={modes.find(m => m.value === f.value_mode)?.help}>
          <Segmented size="sm" value={f.value_mode} onChange={v => setF({ ...f, value_mode: v as Asset['value_mode'] })} options={modes.map(m => ({ value: m.value, label: m.label }))} />
        </Field>
        {(f.value_mode === 'estimate' || ['vehicle', 'property', 'valuables'].includes(f.kind)) && <div className="two-col tight">
          <MoneyField label="What you paid" value={f.bought_price} onChange={v => setF({ ...f, bought_price: v })} cur={f.currency} />
          <Field label="When"><input type="date" value={f.bought_on} max={todayISO()} onChange={e => setF({ ...f, bought_on: e.target.value })} /></Field>
        </div>}
        {(debt || ['deposit', 'savings'].includes(f.kind)) && <div className="two-col tight">
          <Field label="Interest a year"><div className="input-affix"><input inputMode="decimal" value={f.rate} onChange={e => setF({ ...f, rate: e.target.value })} placeholder="0" /><span className="affix">%</span></div></Field>
          {debt ? <MoneyField label="Monthly payment" value={f.payment} onChange={v => setF({ ...f, payment: v })} cur={f.currency} />
            : <Field label={f.kind === 'deposit' ? 'Matures on' : 'Ends on'}><input type="date" value={f.matures_on} onChange={e => setF({ ...f, matures_on: e.target.value })} /></Field>}
        </div>}
        {debt && <Field label="Fixed rate ends or last payment" help="Pocket Wealth reminds you a month before (and Pocket Renewals can track it)."><input type="date" value={f.matures_on} onChange={e => setF({ ...f, matures_on: e.target.value })} /></Field>}
        {!more && <button type="button" className="link-btn small" onClick={() => setMore(true)}><Icon name="plus" size={13} /> More: bank, share, what you put in, expected growth</button>}
        {more && <>
          <div className="two-col tight">
            <Field label={debt ? 'Lender' : 'Bank, broker or fund'}><input value={f.institution} onChange={e => setF({ ...f, institution: e.target.value })} maxLength={200} /></Field>
            <Field label="Account or contract number"><input value={f.number} onChange={e => setF({ ...f, number: e.target.value })} maxLength={100} /></Field>
          </div>
          <div className="two-col tight">
            <Field label="Your share" help="Half of a shared flat is 50%."><div className="input-affix"><input inputMode="numeric" value={f.share} onChange={e => setF({ ...f, share: e.target.value })} /><span className="affix">%</span></div></Field>
            <Field label={f.value_mode === 'estimate' ? 'Change a year (the estimate)' : 'Expected change a year (the plan)'} help={`Empty = ${kind.growth}% for ${kind.label.toLowerCase()}.`}>
              <div className="input-affix"><input inputMode="decimal" value={f.growth} onChange={e => setF({ ...f, growth: e.target.value })} placeholder={String(kind.growth)} /><span className="affix">%</span></div>
            </Field>
          </div>
          {!debt && f.value_mode !== 'positions' && <div className="two-col tight">
            <MoneyField label="What you put in" help="For the gain: deposits minus withdrawals. Updated by itself when you record them." value={f.invested} onChange={v => setF({ ...f, invested: v })} cur={f.currency} />
            {!debt && !['deposit', 'savings'].includes(f.kind) && <MoneyField label="You add each month" value={f.payment} onChange={v => setF({ ...f, payment: v })} cur={f.currency} />}
          </div>}
          {!debt && !['deposit', 'savings'].includes(f.kind) && <Field label="Matures or ends on" help="A bond, a fixed-term product, a loan paid back to you."><input type="date" value={f.matures_on} onChange={e => setF({ ...f, matures_on: e.target.value })} /></Field>}
          <Field label="Notes"><textarea rows={2} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} maxLength={4000} /></Field>
          <label className="check-row"><input type="checkbox" checked={f.include} onChange={e => setF({ ...f, include: e.target.checked })} />Count it in my net worth</label>
          {asset && <Field label={debt ? 'Paid off on' : 'Sold or closed on'} help="From this day it no longer counts; its history stays."><input type="date" value={f.closed_on} onChange={e => setF({ ...f, closed_on: e.target.value })} /></Field>}
        </>}
        <Actions onClose={onClose} busy={busy} label={asset ? 'Save' : 'Add'} />
      </form>
    </Modal>
  )
}

// ---------- a new value ----------

export function ValueModal({ asset, onClose, onSaved, preset }: { asset: Asset; onClose: () => void; onSaved?: (v: Valuation) => void; preset?: { value?: number; date?: string; note?: string } }) {
  const { toast } = useToast()
  const [value, setValue] = useState(preset?.value != null ? String(preset.value) : '')
  const [date, setDate] = useState(preset?.date || todayISO())
  const [note, setNote] = useState(preset?.note || '')
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const v = parseAmount(value)
    if (v == null) { toast('The value is a number like 15000 or 15.000,50', 'err'); return }
    setBusy(true)
    try {
      const r = await api.post<Valuation>(`${V1}/assets/${asset.id}/valuations`, { value: toMinor(Math.abs(v), asset.currency), date, note })
      toast('Value saved'); announceChange(); onSaved?.(r); onClose()
    } catch (err) { toast(errMsg(err), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={`${asset.debt ? 'What is owed on' : 'What is it worth:'} ${asset.name}`} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid form-grid">
        <div className="two-col tight">
          <MoneyField label={asset.debt ? 'Still owed' : 'Worth'} value={value} onChange={setValue} cur={asset.currency} help={asset.share < 1 ? `The whole of it; your ${Math.round(asset.share * 100)}% is worked out.` : undefined} />
          <Field label="On"><input type="date" value={date} max={todayISO()} onChange={e => setDate(e.target.value)} /></Field>
        </div>
        <Field label="Note"><input value={note} onChange={e => setNote(e.target.value)} maxLength={300} placeholder="Statement, estimate from listings…" /></Field>
        {asset.value_mode === 'linked' && <p className="muted small"><Icon name="info" size={13} /> Typing a value stops reading it from the linked app.</p>}
        <Actions onClose={onClose} busy={busy} label="Save" />
      </form>
    </Modal>
  )
}

// ---------- a statement read by AI ----------

interface ReadResult { date: string; value: number | null; currency: string; positions: { name: string; symbol: string; units: number; price: number | null; value: number | null; currency: string }[]; note: string }

export function ReadStatementModal({ asset, onClose }: { asset: Asset; onClose: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [tab, setTab] = useState<'doc' | 'text'>(settings?.documents_via ? 'doc' : 'text')
  const [text, setText] = useState('')
  const [doc, setDoc] = useState<Doc | null>(null)
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<ReadResult | null>(null)
  const [val, setVal] = useState<{ value: number; date: string; note: string } | null>(null)
  const read = async () => {
    setBusy(true)
    try {
      const r = await api.post<ReadResult>(`${V1}/assets/${asset.id}/read`, tab === 'doc' ? { document_id: doc?.id } : { text })
      setRes(r)
    } catch (err) { toast(errMsg(err), 'err') } finally { setBusy(false) }
  }
  if (val) return <ValueModal asset={asset} preset={val} onClose={onClose} />
  return (
    <Modal title={`Read a statement: ${asset.name}`} onClose={onClose}>
      <div className="grid form-grid">
        <p className="muted small">AI reads the balance and the date (and holdings, if listed). Nothing is saved until you confirm it.</p>
        <Segmented size="sm" value={tab} onChange={v => setTab(v)} options={[
          { value: 'doc', label: 'From Pocket Documents', disabled: !settings?.documents_via, title: settings?.documents_via ? '' : 'Connect Pocket Documents in Settings → Connected apps' },
          { value: 'text', label: 'Paste the text' }]} />
        {tab === 'doc' ? <DocPicker value={doc} onChange={setDoc} /> : <Field label="The statement"><textarea rows={8} value={text} onChange={e => setText(e.target.value)} placeholder="Paste the text of the statement or the page of your bank or broker" /></Field>}
        {res && <div className="callout info"><Icon name="sparkles" size={15} /><div className="grow">
          {res.value != null ? <><b>{money(toMinor(res.value, res.currency), res.currency)}</b>{res.date && <> on {fmtDate(res.date)}</>}</> : 'No balance found.'}
          {res.positions?.length > 0 && <div className="small mt">{res.positions.length} holdings: {res.positions.slice(0, 5).map(p => `${p.name}${p.units ? ` (${p.units})` : ''}`).join(', ')}</div>}
          {res.note && <div className="small muted">{res.note}</div>}
        </div></div>}
        <div className="form-actions"><span className="grow" /><button type="button" className="btn" onClick={onClose}>Cancel</button>
          {res && res.value != null && asset.value_mode !== 'positions' ? <button type="button" className="btn primary" onClick={() => setVal({ value: res.value!, date: res.date || todayISO(), note: 'read from a statement' })}>Use this value</button>
            : <button type="button" className="btn primary" disabled={busy || (tab === 'doc' ? !doc : text.trim().length < 20)} onClick={read}>{busy ? 'Reading…' : <><Icon name="sparkles" size={14} />Read it</>}</button>}
        </div>
      </div>
    </Modal>
  )
}

/** Search Pocket Documents and pick one. */
export function DocPicker({ value, onChange }: { value: Doc | null; onChange: (d: Doc | null) => void }) {
  const { settings } = useApp()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 300)
  const [res, setRes] = useState<{ connected: boolean; detail?: string; items: Doc[] } | null>(null)
  useEffect(() => { api.get<{ connected: boolean; detail?: string; items: Doc[] }>(`${V1}/links/documents?q=${encodeURIComponent(dq)}`).then(setRes).catch(() => setRes({ connected: false, items: [] })) }, [dq])
  const coreOn = !!settings?.core_configured && settings?.core_registered !== false
  if (value) return <div className="pick-row picked"><Icon name="file" size={16} /><span className="grow ellipsis">{value.title}</span><button type="button" className="btn ghost sm" onClick={() => onChange(null)}>Change</button></div>
  return (
    <div className="stack">
      <input value={q} onChange={e => setQ(e.target.value)} placeholder="Search your documents…" aria-label="Search documents" />
      {!res ? <Loading /> : !res.connected || res.detail ? <p className="muted small">{res.detail || 'Pocket Documents is not connected.'}{!coreOn && <CoreHint />}</p>
        : res.items.length === 0 ? <p className="muted small">Nothing found.</p>
          : <div className="pick-list">{res.items.map(d => <button type="button" key={d.id} className="pick-row" onClick={() => onChange(d)}><Icon name="file" size={15} /><span className="grow ellipsis">{d.title}</span><span className="muted small">{d.created ? fmtDate(d.created.slice(0, 10)) : ''}</span></button>)}</div>}
    </div>
  )
}

export function AttachModal({ asset, onClose }: { asset: Asset; onClose: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [tab, setTab] = useState<'doc' | 'url'>(settings?.documents_via ? 'doc' : 'url')
  const [doc, setDoc] = useState<Doc | null>(null)
  const [url, setUrl] = useState('')
  const [title, setTitle] = useState('')
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post(`${V1}/assets/${asset.id}/attachments`, tab === 'doc' ? { document_id: doc?.id, title: doc?.title } : { url, title })
      toast('Kept with it'); announceChange(); onClose()
    } catch (err) { toast(errMsg(err), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Keep a statement or contract" onClose={onClose} size="sm">
      <form onSubmit={save} className="grid form-grid">
        <Segmented size="sm" value={tab} onChange={v => setTab(v)} options={[{ value: 'doc', label: 'From Pocket Documents', disabled: !settings?.documents_via }, { value: 'url', label: 'A link' }]} />
        {tab === 'doc' ? <DocPicker value={doc} onChange={setDoc} /> : <>
          <Field label="Link"><input type="url" value={url} onChange={e => setUrl(e.target.value)} placeholder="https://" required /></Field>
          <Field label="Name"><input value={title} onChange={e => setTitle(e.target.value)} placeholder="Loan contract" /></Field>
        </>}
        <Actions onClose={onClose} busy={busy || (tab === 'doc' && !doc)} label="Keep" />
      </form>
    </Modal>
  )
}

// ---------- a holding ----------

interface Quote { symbol: string; name: string; price: number; currency: string; date: string; exchange: string }

export function PositionModal({ position, assetId, onClose }: { position?: Position; assetId?: number; onClose: () => void }) {
  const { settings } = useApp()
  const { assets } = useData()
  const { toast } = useToast()
  const holders = assets.filter(a => !a.debt && (a.value_mode === 'positions' || ['investment', 'crypto', 'pension'].includes(a.kind)))
  const a0 = assets.find(a => a.id === (position?.asset_id || assetId))
  const cur0 = position?.currency || a0?.currency || settings?.currency || 'EUR'
  const [f, setF] = useState({
    asset_id: String(position?.asset_id || assetId || holders[0]?.id || ''), name: position?.name || '', symbol: position?.symbol || '',
    kind: position?.kind || (a0?.kind === 'crypto' ? 'crypto' : 'etf'), units: position ? String(position.units) : '', cost: amountIn(position?.cost ?? null, cur0),
    currency: cur0, price: position ? String(position.price) : '', price_auto: position?.price_auto ?? true, notes: position?.notes || '',
  })
  const [busy, setBusy] = useState(false)
  const [looking, setLooking] = useState(false)
  const online = !!settings?.online && !!settings?.online_allowed
  const look = async () => {
    if (!f.symbol.trim()) return
    setLooking(true)
    try {
      const q = await api.get<Quote>(`${V1}/lookup?symbol=${encodeURIComponent(f.symbol.trim())}`)
      setF(x => ({ ...x, name: x.name || q.name, currency: q.currency || x.currency, price: String(q.price), symbol: q.symbol }))
      toast(`${q.name || q.symbol}: ${q.price} ${q.currency}${q.exchange ? ` on ${q.exchange}` : ''}`)
    } catch (err) { toast(errMsg(err), 'err') } finally { setLooking(false) }
  }
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const units = parseAmount(f.units), price = parseAmount(f.price), cost = f.cost.trim() ? parseAmount(f.cost) : null
    if (units == null) { toast('How many units: a number like 12.5', 'err'); return }
    const body = { asset_id: Number(f.asset_id), name: f.name, symbol: f.symbol, kind: f.kind, units, currency: f.currency, price: price ?? 0, price_auto: f.price_auto, notes: f.notes,
      cost: cost != null ? toMinor(cost, f.currency) : position ? position.cost : toMinor(units * (price ?? 0), f.currency) }
    setBusy(true)
    try {
      if (position) await api.put(`${V1}/positions/${position.id}`, body); else await api.post(`${V1}/positions`, body)
      toast(position ? 'Saved' : 'Holding added'); announceChange(); onClose()
    } catch (err) { toast(errMsg(err), 'err') } finally { setBusy(false) }
  }
  const del = async () => {
    if (!position || !window.confirm(`Delete ${position.name}? Its buys and sells stay in the activity.`)) return
    try { await api.del(`${V1}/positions/${position.id}`); toast('Deleted'); announceChange(); onClose() } catch (err) { toast(errMsg(err), 'err') }
  }
  return (
    <Modal title={position ? `Edit ${position.name}` : 'Add a holding'} onClose={onClose}>
      <form onSubmit={save} className="grid form-grid">
        {!position && !assetId && <Field group label="In"><Select ariaLabel="Account" value={f.asset_id} onChange={v => setF({ ...f, asset_id: v })} options={holders.map(a => ({ value: String(a.id), label: a.name }))} placeholder="Add an investment account first" /></Field>}
        <div className="two-col tight">
          <Field label="Symbol" help={online ? 'As on Yahoo Finance: VWCE.DE, AAPL, BTC-EUR. The price then comes in by itself.' : 'Prices are looked up online only with that switched on (Settings → General).'}>
            <div className="row"><input value={f.symbol} onChange={e => setF({ ...f, symbol: e.target.value.toUpperCase() })} placeholder="VWCE.DE" maxLength={30} />
              {online && <button type="button" className="btn" disabled={looking || !f.symbol.trim()} onClick={look}>{looking ? '…' : 'Look up'}</button>}</div>
          </Field>
          <Field group label="Kind"><Select ariaLabel="Kind" value={f.kind} onChange={v => setF({ ...f, kind: v })} options={POSITION_KINDS} /></Field>
        </div>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="Vanguard FTSE All-World" maxLength={200} /></Field>
        <div className="three-col">
          <Field label="Units"><input inputMode="decimal" value={f.units} onChange={e => setF({ ...f, units: e.target.value })} placeholder="10" required /></Field>
          <Field label="Price each"><input inputMode="decimal" value={f.price} onChange={e => setF({ ...f, price: e.target.value })} placeholder="0" /></Field>
          <Field group label="Currency"><CurrencySelect value={f.currency} onChange={v => setF({ ...f, currency: v })} /></Field>
        </div>
        <MoneyField label="What they cost in all" help={position ? 'Buys and sells recorded below change it by themselves.' : 'Empty = units × price.'} value={f.cost} onChange={v => setF({ ...f, cost: v })} cur={f.currency} />
        <label className={`check-row ${online ? '' : 'disabled'}`}><input type="checkbox" checked={f.price_auto && online} disabled={!online} onChange={e => setF({ ...f, price_auto: e.target.checked })} />Update the price by itself (needs a symbol)</label>
        {position?.price_error && <p className="small warn-text"><Icon name="alert" size={13} /> Last lookup: {position.price_error}</p>}
        <Actions onClose={onClose} busy={busy} label={position ? 'Save' : 'Add'} extra={position && <button type="button" className="btn ghost danger-text" onClick={del}><Icon name="trash" size={14} />Delete</button>} />
      </form>
    </Modal>
  )
}

// ---------- activity ----------

export function ActivityModal({ asset, positions, preset, onClose }: { asset: Asset; positions: Position[]; preset?: { kind?: string; position_id?: number }; onClose: () => void }) {
  const { toast } = useToast()
  const hasPos = positions.length > 0
  const kinds = Object.entries(ACTIVITY).filter(([k]) => hasPos || (k !== 'buy' && k !== 'sell'))
  const [f, setF] = useState({ kind: preset?.kind || (hasPos ? 'buy' : 'deposit'), position_id: String(preset?.position_id || positions[0]?.id || ''), date: todayISO(), units: '', amount: '', note: '', apply: true })
  const [busy, setBusy] = useState(false)
  const pos = positions.find(p => String(p.id) === f.position_id)
  const cur = pos && (f.kind === 'buy' || f.kind === 'sell' || f.kind === 'dividend') ? pos.currency : asset.currency
  const needsPos = f.kind === 'buy' || f.kind === 'sell'
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const amount = parseAmount(f.amount), u = f.units.trim() ? parseAmount(f.units) : 0
    if (amount == null) { toast('The amount is a number like 300 or 1,250.50', 'err'); return }
    setBusy(true)
    try {
      await api.post<Activity>(`${V1}/activity`, { asset_id: asset.id, position_id: needsPos || (f.kind === 'dividend' && pos) ? Number(f.position_id) : null, kind: f.kind, date: f.date,
        units: u || 0, amount: toMinor(Math.abs(amount), cur), currency: cur, note: f.note, apply: f.apply })
      toast('Recorded'); announceChange(); onClose()
    } catch (err) { toast(errMsg(err), 'err') } finally { setBusy(false) }
  }
  const effect = f.kind === 'buy' ? 'Adds the units and the cost to the holding.' : f.kind === 'sell' ? 'Takes the units off the holding, and its cost in proportion.'
    : f.kind === 'deposit' ? `Adds to what you put in${asset.value_mode === 'manual' ? ', and to its value' : ''}.` : f.kind === 'withdrawal' ? `Takes it off what you put in${asset.value_mode === 'manual' ? ', and off its value' : ''}.`
      : 'Counted as income from what you own.'
  return (
    <Modal title={`Record: ${asset.name}`} onClose={onClose}>
      <form onSubmit={save} className="grid form-grid">
        <Field group label="What happened"><Select ariaLabel="Kind" value={f.kind} onChange={v => setF({ ...f, kind: v })} options={kinds.map(([k, v]) => ({ value: k, label: v.label }))} /></Field>
        {(needsPos || (f.kind === 'dividend' && hasPos)) && <Field group label="Holding"><Select ariaLabel="Holding" value={f.position_id} onChange={v => setF({ ...f, position_id: v })} options={positions.map(p => ({ value: String(p.id), label: p.name, hint: p.symbol }))} /></Field>}
        <div className={needsPos ? 'three-col' : 'two-col tight'}>
          <Field label="On"><input type="date" value={f.date} max={todayISO()} onChange={e => setF({ ...f, date: e.target.value })} /></Field>
          {needsPos && <Field label="Units"><input inputMode="decimal" value={f.units} onChange={e => setF({ ...f, units: e.target.value })} required placeholder="2.5" /></Field>}
          <MoneyField label={needsPos ? (f.kind === 'buy' ? 'Paid in all' : 'Received in all') : 'Amount'} value={f.amount} onChange={v => setF({ ...f, amount: v })} cur={cur} />
        </div>
        <Field label="Note"><input value={f.note} onChange={e => setF({ ...f, note: e.target.value })} maxLength={300} /></Field>
        {f.kind !== 'dividend' && f.kind !== 'interest' && f.kind !== 'fee' && <label className="check-row"><input type="checkbox" checked={f.apply} onChange={e => setF({ ...f, apply: e.target.checked })} />{effect}</label>}
        <Actions onClose={onClose} busy={busy} label="Record" />
      </form>
    </Modal>
  )
}

/** "Which one?" before updating a value from anywhere (the + menu). */
export function PickAssetModal({ onClose, onPick }: { onClose: () => void; onPick: (a: Asset) => void }) {
  const { assets } = useData()
  const [q, setQ] = useState('')
  const list = assets.filter(a => a.value_mode !== 'positions' && a.value_mode !== 'linked' && (q === '' || a.name.toLowerCase().includes(q.toLowerCase())))
  return (
    <Modal title="Update a value" onClose={onClose} size="sm">
      <div className="stack">
        <input value={q} onChange={e => setQ(e.target.value)} placeholder="Search…" autoFocus aria-label="Search" />
        <div className="pick-list">
          {list.map(a => <button key={a.id} type="button" className="pick-row" onClick={() => onPick(a)}>
            <span className={`list-ico sm gr-${a.group}`}><Icon name={kindOf(a.kind).icon} size={14} /></span>
            <span className="grow ellipsis">{a.name}</span>{a.stale && <span className="pill st-soon">Old</span>}<span className="muted small tnum">{money(a.mine, a.currency, true)}</span>
          </button>)}
          {list.length === 0 && <p className="muted small">Nothing to update by hand.</p>}
        </div>
      </div>
    </Modal>
  )
}
