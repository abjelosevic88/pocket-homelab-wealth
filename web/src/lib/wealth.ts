// Names, icons and money formatting for Pocket Wealth's own words.
import type { Group, Kind } from './types'

// Kinds and their defaults; the server has the same list (store/kinds.go).
export const KINDS: Kind[] = [
  { value: 'cash', label: 'Current account or cash', group: 'cash', debt: false, liquid: true, mode: 'manual', growth: 0, icon: 'wallet', hint: 'Everyday money: a current account, cash at home' },
  { value: 'savings', label: 'Savings account', group: 'cash', debt: false, liquid: true, mode: 'manual', growth: 1.5, icon: 'piggy', hint: 'Money put aside you can reach at any time' },
  { value: 'deposit', label: 'Term deposit', group: 'cash', debt: false, liquid: false, mode: 'estimate', growth: 3, icon: 'lock', hint: 'Fixed for a term at an interest rate; set the date it matures' },
  { value: 'investment', label: 'Investments', group: 'investments', debt: false, liquid: false, mode: 'positions', growth: 6, icon: 'trending-up', hint: 'A broker account, funds, shares, bonds' },
  { value: 'pension', label: 'Pension', group: 'pension', debt: false, liquid: false, mode: 'manual', growth: 5, icon: 'sunset', hint: 'A pension fund or retirement savings' },
  { value: 'crypto', label: 'Crypto', group: 'investments', debt: false, liquid: false, mode: 'positions', growth: 0, icon: 'bitcoin', hint: 'Coins in a wallet or on an exchange' },
  { value: 'property', label: 'Home or property', group: 'property', debt: false, liquid: false, mode: 'manual', growth: 2, icon: 'home', hint: 'A flat, a house, land' },
  { value: 'vehicle', label: 'Vehicle', group: 'vehicles', debt: false, liquid: false, mode: 'estimate', growth: -15, icon: 'car', hint: 'A car or a motorbike; its value drops every year' },
  { value: 'valuables', label: 'Valuables', group: 'valuables', debt: false, liquid: false, mode: 'manual', growth: 0, icon: 'gem', hint: 'Jewellery, art, collections, the things you own' },
  { value: 'business', label: 'Business', group: 'other', debt: false, liquid: false, mode: 'manual', growth: 0, icon: 'briefcase', hint: 'A share in a company' },
  { value: 'receivable', label: 'Money owed to me', group: 'other', debt: false, liquid: false, mode: 'manual', growth: 0, icon: 'hand-coins', hint: 'A loan you gave someone' },
  { value: 'other', label: 'Something else', group: 'other', debt: false, liquid: false, mode: 'manual', growth: 0, icon: 'box', hint: 'Anything else of value' },
  { value: 'mortgage', label: 'Mortgage', group: 'debts', debt: true, liquid: false, mode: 'manual', growth: 0, icon: 'home', hint: 'A loan on a home' },
  { value: 'loan', label: 'Loan', group: 'debts', debt: true, liquid: false, mode: 'manual', growth: 0, icon: 'landmark', hint: 'A car loan, a personal loan, a student loan' },
  { value: 'credit_card', label: 'Credit card', group: 'debts', debt: true, liquid: false, mode: 'manual', growth: 0, icon: 'card', hint: 'What you owe on a card' },
  { value: 'debt', label: 'Other debt', group: 'debts', debt: true, liquid: false, mode: 'manual', growth: 0, icon: 'receipt', hint: 'Money you owe someone' },
]
export const kindOf = (v: string): Kind => KINDS.find(k => k.value === v) || KINDS[11]

export const GROUPS: { value: Group; label: string; icon: string }[] = [
  { value: 'cash', label: 'Cash and savings', icon: 'wallet' },
  { value: 'investments', label: 'Investments', icon: 'trending-up' },
  { value: 'pension', label: 'Pension', icon: 'sunset' },
  { value: 'property', label: 'Property', icon: 'home' },
  { value: 'vehicles', label: 'Vehicles', icon: 'car' },
  { value: 'valuables', label: 'Valuables', icon: 'gem' },
  { value: 'other', label: 'Other', icon: 'box' },
  { value: 'debts', label: 'Debts', icon: 'landmark' },
]
export const GROUP_NAME: Record<string, string> = Object.fromEntries(GROUPS.map(g => [g.value, g.label]))

export const MODES: { value: string; label: string; help: string }[] = [
  { value: 'manual', label: 'I type it in', help: 'Update it from a statement now and then; each update is kept, so you see how it moved.' },
  { value: 'positions', label: 'From its holdings', help: 'Units × price of each fund, share or coin in it; prices can come in by themselves.' },
  { value: 'estimate', label: 'An estimate', help: 'Grows or shrinks by a percentage a year from the last value you typed in (or what you paid).' },
  { value: 'linked', label: 'From the linked app', help: 'Read from the Pocket app it comes from, every hour and when it changes there.' },
]
export const MODE_NAME: Record<string, string> = { manual: 'Typed in', positions: 'From holdings', estimate: 'Estimate', linked: 'Linked' }

export const POSITION_KINDS: { value: string; label: string }[] = [
  { value: 'etf', label: 'ETF' }, { value: 'fund', label: 'Fund' }, { value: 'stock', label: 'Share' }, { value: 'bond', label: 'Bond' },
  { value: 'crypto', label: 'Crypto' }, { value: 'cash', label: 'Cash' }, { value: 'other', label: 'Other' },
]
export const POSITION_KIND_NAME: Record<string, string> = Object.fromEntries(POSITION_KINDS.map(k => [k.value, k.label]))

export const ACTIVITY: Record<string, { label: string; icon: string; past: string }> = {
  buy: { label: 'Buy', icon: 'arrow-down', past: 'Bought' },
  sell: { label: 'Sell', icon: 'arrow-up', past: 'Sold' },
  dividend: { label: 'Dividend', icon: 'coins', past: 'Dividend' },
  interest: { label: 'Interest', icon: 'percent', past: 'Interest' },
  deposit: { label: 'Put money in', icon: 'plus', past: 'Put in' },
  withdrawal: { label: 'Take money out', icon: 'minus', past: 'Took out' },
  fee: { label: 'Fee', icon: 'receipt', past: 'Fee' },
}

export const SOURCE_NAME: Record<string, string> = {
  money: 'Pocket Money', garage: 'Pocket Garage', home: 'Pocket Home', inventory: 'Pocket Inventory', documents: 'Pocket Documents',
  web: 'Added here', demo: 'Example', import: 'Imported', api: 'API', auto: 'By itself', ai: 'Read by AI',
}
export const SOURCE_ICON: Record<string, string> = { money: 'wallet', garage: 'car', home: 'home', inventory: 'package', documents: 'file' }
export const linkedApp = (ref: string) => (ref || '').split(':')[0]

// ---- money ----

const ZERO = new Set(['JPY', 'KRW', 'HUF', 'ISK', 'CLP', 'VND', 'IDR', 'XAF', 'XOF', 'UGX', 'PYG'])
const THREE = new Set(['KWD', 'BHD', 'OMR', 'JOD', 'TND'])
export const decimals = (cur: string) => (ZERO.has(cur) ? 0 : THREE.has(cur) ? 3 : 2)
export const toMajor = (minor: number, cur: string) => minor / 10 ** decimals(cur)
export const toMinor = (major: number, cur: string) => Math.round(major * 10 ** decimals(cur))

/** 12345678 EUR → "€123,456.78"; whole = no cents. */
export function money(minor: number, cur: string, whole = false): string {
  const v = toMajor(minor, cur)
  const d = whole ? 0 : decimals(cur)
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency: cur, minimumFractionDigits: d, maximumFractionDigits: d }).format(v)
  } catch {
    return `${v.toLocaleString(undefined, { minimumFractionDigits: d, maximumFractionDigits: d })} ${cur}`
  }
}

/** Short for axes and tight places: "€182k", "€1.2M". */
export function moneyShort(minor: number, cur: string): string {
  const v = toMajor(minor, cur)
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency: cur, notation: 'compact', maximumFractionDigits: Math.abs(v) >= 1e6 ? 1 : 0 }).format(v)
  } catch {
    return `${Math.round(v).toLocaleString()} ${cur}`
  }
}

/** "+€1,234" / "−€56". */
export function signed(minor: number, cur: string, whole = true): string {
  if (minor === 0) return money(0, cur, whole)
  return (minor > 0 ? '+' : '−') + money(Math.abs(minor), cur, whole)
}

export function pct(p: number | null | undefined, sign = true): string {
  if (p == null || !Number.isFinite(p)) return ''
  const s = Math.abs(p).toLocaleString(undefined, { maximumFractionDigits: 1 }) + '%'
  return sign ? (p > 0 ? '+' : p < 0 ? '−' : '') + s : s
}

/** A number typed by a person: "1.234,56", "1,234.56", "1234" → 1234.56. */
export function parseAmount(s: string): number | null {
  let t = (s || '').replace(/[^\d,.\-]/g, '')
  if (!t) return null
  const lastComma = t.lastIndexOf(','), lastDot = t.lastIndexOf('.')
  if (lastComma > lastDot) t = t.replace(/\./g, '').replace(',', '.')
  else t = t.replace(/,/g, '')
  const n = Number(t)
  return Number.isFinite(n) ? n : null
}

/** Units without trailing zeros: 98.2010 → "98.201". */
export function units(n: number): string {
  return n.toLocaleString(undefined, { maximumFractionDigits: 6 })
}

/** "in 41 days" / "today" / "12 days ago". */
export function inDays(d: number): string {
  if (d === 0) return 'today'
  if (d === 1) return 'tomorrow'
  if (d > 0) return d > 60 ? `in ${Math.round(d / 30.4)} months` : `in ${d} days`
  return d < -60 ? `${Math.round(-d / 30.4)} months ago` : `${-d} days ago`
}

/** "5 years 3 months". */
export function months(n: number): string {
  if (n < 0) return 'never at this payment'
  const y = Math.floor(n / 12), m = n % 12
  return [y && `${y} ${y === 1 ? 'year' : 'years'}`, m && `${m} ${m === 1 ? 'month' : 'months'}`].filter(Boolean).join(' ') || 'this month'
}
