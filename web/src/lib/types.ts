export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'
export type AppSlug = 'money' | 'garage' | 'home' | 'inventory' | 'documents'

type ConnFields = { [K in AppSlug as `${K}_mode`]: ConnMode } & { [K in AppSlug as `${K}_via`]: ConnVia } & { [K in AppSlug as `${K}_detail` | `${K}_url` | `${K}_token` | `${K}_public_url` | `${K}_open_url`]: string } & { [K in AppSlug as `${K}_configured`]: boolean }

export type Settings = ConnFields & {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number; currency: string; stale_days: number
  online: boolean; online_allowed: boolean; rates: Record<string, number>
  notify_monthly: boolean; notify_stale: boolean; notify_milestones: boolean; notify_maturity: boolean; notify_hour: number
  notify_url: string; notify_format: string; notify_configured: boolean
  plan_saving_auto: boolean; plan_saving: number; plan_spending: number; plan_return: number; plan_inflation: number; plan_withdraw: number
  plan_target: number; plan_birth_year: number
  live: boolean; inventory_min: number; stream: { configured: boolean; connected: boolean; error: string; last_event_at?: string }
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string
}

export type ValueMode = 'manual' | 'positions' | 'linked' | 'estimate'
export type Group = 'cash' | 'investments' | 'pension' | 'property' | 'vehicles' | 'valuables' | 'other' | 'debts'

export interface Kind { value: string; label: string; group: Group; debt: boolean; liquid: boolean; mode: ValueMode; growth: number; icon: string; hint: string }

export interface Valuation { id: number; asset_id: number; date: string; value: number; note: string; source: string; created_at: string }

export interface Asset {
  id: number; name: string; kind: string; currency: string; institution: string; number: string; notes: string; value_mode: ValueMode
  include: boolean; share: number; rate: number; payment: number; growth: number | null; matures_on: string; bought_on: string
  bought_price: number | null; invested: number | null; source: string; source_ref: string; source_url: string; linked_value: number | null
  linked_at: string; linked_error: string; closed_on: string; sort: number; created_at: string; updated_at: string
  group: Group; debt: boolean; liquid: boolean; positions: number; last_valuation?: Valuation
  // derived
  value: number; mine: number; value_base: number; known: boolean; month_change: number; updated: string; stale: boolean
  cost: number | null; gain: number | null; gain_pct: number | null; days_to_mature?: number
}

export interface Position {
  id: number; asset_id: number; name: string; symbol: string; kind: string; units: number; cost: number; currency: string; price: number
  price_on: string; price_auto: boolean; price_error: string; notes: string; sort: number; asset_name?: string
  value: number; value_base: number; gain: number; gain_pct: number | null
}

export interface Activity {
  id: number; asset_id: number; position_id: number | null; date: string; kind: string; units: number; amount: number; currency: string
  note: string; source: string; created_at: string; asset_name?: string; position_name?: string
}

export interface Attachment { id: number; asset_id: number; document_id: number | null; title: string; url: string; created_at: string }

export interface Payoff { balance: number; rate: number; payment: number; extra: number; months: number; paid_off_on: string; interest_left: number; interest_month: number }

export interface Change { amount: number; pct: number | null; since: string }
export interface Milestone { amount: number; reached_on?: string; next?: boolean }
export interface GroupTotal { group: Group; label: string; value: number; count: number; pct?: number; month_change: number }

export interface Overview {
  currency: string; today: string; net: number; assets: number; debts: number; liquid: number; month: Change; year: Change
  groups: GroupTotal[]; counts: Record<string, number>; open: number; stale: Asset[]; maturing: Asset[]; linked_errors: Asset[]
  milestones: Milestone[]; missing_rates: string[]; first_day: string; debt_ratio?: number
}

export interface Point { date: string; label: string; net: number; assets: number; debts: number; liquid: number; groups: Record<string, number> }

export interface AssetDetail {
  asset: Asset; valuations: Valuation[]; positions: Position[]; activity: Activity[]; attachments: Attachment[]
  history: { date: string; label: string; value: number; value_base: number }[]; currency: string; missing_rates: string[]
  payoff?: Payoff; source_name?: string
}

export interface Plan {
  currency: string; net: number; assets: number; debts: number; liquid: number; return: number; return_mix: number; inflation: number; withdraw: number
  saving: number; spending: number; money: { used: boolean; months?: number; income?: number; spending?: number; saving?: number; from?: string; to?: string; error?: string }
  fi_number: number; target: number; target_custom: boolean; reach_on: string; reach_years: number; reach_age?: number
  years: { year: number; age?: number; nominal: number; real: number }[]; birth_year: number; passive_month: number
  milestones: Milestone[]; emergency_months?: number; first_day: string
}

export interface Suggestion { ref: string; app: string; title: string; subtitle: string; kind: string; value: number | null; currency: string; url: string; asset_id: number | null; dismissed: boolean }
export interface AppSuggestions { app: string; name: string; connected: boolean; error?: string; items: Suggestion[] }

export interface Doc { id: number; title: string; created: string; correspondent_name?: string; document_type_name?: string }
