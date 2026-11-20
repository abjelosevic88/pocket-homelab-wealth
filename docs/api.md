# API

JSON over HTTP at `/api/v1`. Authenticate with the session cookie, a personal token (`Authorization: Bearer pwl_…`, Settings → API & shortcuts), a Pocket Core device or user token, or a Pocket Core app-link token. Everything is per person: another person's ids answer 404. Money is in **minor units** (cents) of the currency it is in; dates are `YYYY-MM-DD`. PUT bodies are partial: only the keys you send change.

## Net worth

| Call | What |
|---|---|
| `GET /summary` | `{currency, net, assets, debts, liquid, month, year, groups, needs_update, maturing, url}` — a short picture (Homepage, Pocket AI) |
| `GET /overview` | The summary plus `stale`, `maturing`, `linked_errors` (assets), `milestones`, `counts`, `missing_rates`, `first_day`, `debt_ratio` |
| `GET /history?months=24` | `{points: [{date, label, net, assets, debts, liquid, groups}]}` — each month end, then today |
| `GET /debts?extra=` | Each debt with `payoff` `{months, paid_off_on, interest_left, interest_month}` (with `extra` a month: `payoff_extra`), totals |
| `GET /plan` | The projection: `years [{year, age, nominal, real}]`, `fi_number`, `target`, `reach_on`, `reach_years`, `passive_month`, `emergency_months`, the inputs used and where they came from (`money`) |
| `GET /maturing?days=` | Deposits that mature, fixed rates that end, money due back (`matures_on`) — what Pocket Renewals reads |
| `GET /day/{date}` | `{items}`: activity and values typed in that day (Pocket Day) |
| `GET /search?q` | Assets and holdings matching `q` |
| `GET /kinds` | Kinds (group, debt, liquid, default value mode and growth), groups, holding and activity kinds |

## Assets and debts

| Call | What |
|---|---|
| `GET /assets?closed=1&q&group` | Each with `value` (whole, its currency), `mine` (your share), `value_base`, `month_change`, `updated`, `stale`, `cost`, `gain`, `gain_pct`, `days_to_mature` |
| `POST /assets` | `name`, `kind` (cash, savings, deposit, investment, pension, crypto, property, vehicle, valuables, business, receivable, other, mortgage, loan, credit_card, debt), `currency`, `institution`, `number`, `notes`, `value_mode` (manual, positions, estimate, linked), `include`, `share` (0–1), `rate`, `payment`, `growth` (% a year, null = the kind's), `matures_on`, `bought_on`, `bought_price`, `invested`; optional first `value` and `date` |
| `GET /assets/{id}` | `{asset, valuations, positions, activity, attachments, history, payoff}` |
| `PUT /assets/{id}`, `DELETE /assets/{id}` | `closed_on` = sold or paid off (its history stays). Deleting a linked one is remembered, so it is not suggested again |
| `POST /assets/{id}/valuations` | `{value, date, note}` — one per day; a second the same day replaces it. Debts: what is still owed (positive) |
| `DELETE /valuations/{id}` | |
| `POST /assets/{id}/attachments` | `{document_id}` (Pocket Documents) or `{url, title}`; `DELETE /attachments/{id}` |
| `POST /assets/{id}/read` | `{text}` or `{document_id}` → `{date, value, currency, positions, note}` read by AI; nothing is saved |
| `POST /assets/order` | `{ids: […]}` |

## Investments

| Call | What |
|---|---|
| `GET /positions` | Every holding with `value`, `value_base`, `gain`, `gain_pct`; totals, `by_kind`, `income_year` (dividends and interest, 12 months) |
| `POST /positions`, `PUT /positions/{id}`, `DELETE /positions/{id}` | `asset_id`, `name`, `symbol` (Yahoo Finance: `VWCE.DE`, `AAPL`, `BTC-EUR`), `kind`, `units`, `cost` (all of them), `currency`, `price` (each, major units), `price_auto` |
| `GET /activity?asset&kind&from&to&limit` | Newest first |
| `POST /activity` | `{asset_id, position_id, kind (buy, sell, dividend, interest, deposit, withdrawal, fee), date, units, amount, currency, note, apply}`; with `apply` (default) a buy or sell changes the holding's units and cost, a deposit or withdrawal what was invested and a typed-in value |
| `DELETE /activity/{id}` | Does not change the holding back |
| `GET /lookup?symbol=` | A price from Yahoo Finance (when looking up online is on) |
| `POST /prices/update` | Look up every holding with a symbol now |
| `GET /rates`, `POST /rates/update` | The rates in use (European Central Bank, Yahoo, fixed, your own) |

## The other Pocket apps

| Call | What |
|---|---|
| `GET /suggestions` | Per app (money, garage, home, inventory, rentals): what could be added, and what already is (`asset_id`) |
| `POST /suggestions/add` | `{refs: ["money:account:4", "garage:vehicle:2", "home:home:1", "inventory:items", "rentals:property:3"]}` |
| `POST /suggestions/dismiss` | `{ref, title, restore}` |
| `POST /linked/refresh` | Read every linked value again now (it also happens every hour, and seconds after a change with the events link) |

What is read: Money `GET /accounts` (balances; debts are what is owed) and `GET /transactions?from&to&limit=1` (the month's totals, for the plan); Garage `GET /vehicles` (purchase price and date, sold); Home `GET /homes`; Inventory `GET /items` (price × quantity of the items worth at least the minimum); Rentals `GET /properties` (`value` of each place that has one, linked as property `rentals:property:<id>` with a link to `/properties/<id>`, `bought_on`/`bought_for` as bought; `rent_roll` is only shown, as rent already arrives in Money).

## For other apps (Pocket Core links)

Wealth offers `wealth:read` (summary, overview, history, assets, positions, activity, debts, plan, maturing, kinds, search, day) and `wealth:write` (`POST /assets`, `PUT/DELETE /assets/{id}`, `POST /assets/{id}/valuations`, `DELETE /valuations/{id}`, `POST /activity`, `DELETE /activity/{id}`). An app may change or delete only what it added itself.

Pocket Move can copy your assets, values and investments out and bring them in: `move:export` reads `GET /export.json` and `GET /export.csv`, `move:import` posts `POST /import` (adds; a second run of the same file adds nothing). `GET /pocket/portable` describes both.

Events, each with `user_email`: `wealth.asset_saved`, `wealth.valuation_added`, `wealth.activity_added`, `wealth.milestone_reached`, `wealth.monthly` (on the 1st: last month's net worth and change), `wealth.matures_soon` (30 and 7 days before, and on the day).

## Everything else

`/auth/*`, `/users`, `/settings`, `/tokens`, `/backup` (admins: the whole database), `/export.json`, `/export.csv` (every value), `/import`, `/integrations/test`, `/notify/test`, `/notify/check`, `/ai/status`, `/ai/models` — as in every Pocket app. `GET /healthz` (with `core_outbox`), `GET /readyz`, `GET /metrics` (`METRICS_ENABLED`).
