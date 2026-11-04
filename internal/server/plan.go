package server

import (
	"context"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// ---- the overview ----

type change struct {
	Amount int64    `json:"amount"`
	Pct    *float64 `json:"pct"`
	Since  string   `json:"since"`
}

func changeOf(now, then int64, since string) change {
	c := change{Amount: now - then, Since: since}
	if then != 0 {
		p := math.Round(float64(now-then)/math.Abs(float64(then))*1000) / 10
		c.Pct = &p
	}
	return c
}

func (s *Server) overview(ctx context.Context, uid int64) (map[string]any, error) {
	b, err := s.loadBook(ctx, uid, true)
	if err != nil {
		return nil, err
	}
	now := b.totalsAt(b.today)
	month, year := b.totalsAt(lastMonthEnd(b.today)), b.totalsAt(lastYearEnd(b.today))
	groups := []map[string]any{}
	counts := map[string]int{}
	for _, g := range store.Groups {
		n := 0
		for i := range b.assets {
			a := &b.assets[i]
			if a.Group == g.Value && !a.Closed(b.today) {
				n++
			}
		}
		counts[g.Value] = n
		if n == 0 {
			continue
		}
		v := now.Groups[g.Value]
		item := map[string]any{"group": g.Value, "label": g.Label, "value": v, "count": n, "month_change": v - month.Groups[g.Value]}
		if now.Assets > 0 && g.Value != "debts" {
			item["pct"] = math.Round(float64(v)/float64(now.Assets)*1000) / 10
		}
		groups = append(groups, item)
	}
	stale, maturing, linkedErrors := []assetView{}, []assetView{}, []assetView{}
	open := 0
	for i := range b.assets {
		a := &b.assets[i]
		if a.Closed(b.today) {
			continue
		}
		open++
		v := b.view(a)
		if v.Stale && a.Include {
			stale = append(stale, v)
		}
		if v.DaysToMature != nil && *v.DaysToMature >= -7 && *v.DaysToMature <= 90 {
			maturing = append(maturing, v)
		}
		if a.LinkedError != "" && a.SourceRef != "" {
			linkedErrors = append(linkedErrors, v)
		}
	}
	sortViews(stale)
	sort.SliceStable(maturing, func(i, j int) bool { return *maturing[i].DaysToMature < *maturing[j].DaysToMature })
	ms := b.milestones()
	out := map[string]any{
		"currency": b.base, "today": b.today, "net": now.Net, "assets": now.Assets, "debts": now.Debts, "liquid": now.Liquid,
		"month": changeOf(now.Net, month.Net, month.Date), "year": changeOf(now.Net, year.Net, year.Date),
		"groups": groups, "counts": counts, "open": open, "stale": stale, "maturing": maturing, "linked_errors": linkedErrors,
		"milestones": ms, "missing_rates": b.fx.Missing(), "first_day": b.firstDay(),
	}
	if now.Assets > 0 {
		out["debt_ratio"] = math.Round(float64(now.Debts)/float64(now.Assets)*1000) / 10
	}
	return out, nil
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	out, err := s.overview(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "overview")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleHistory is net worth at each month end (months back, default 24).
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.loadBook(ctx, actorID(ctx), true)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	n := queryInt(r, "months")
	if n <= 0 {
		n = 24
	}
	n = min(n, 600)
	writeJSON(w, http.StatusOK, map[string]any{"currency": b.base, "points": b.series(n), "missing_rates": b.fx.Missing()})
}

// ---- milestones ----

// milestoneSteps are the round numbers worth a moment, in major units.
var milestoneSteps = []float64{10e3, 25e3, 50e3, 75e3, 100e3, 150e3, 200e3, 250e3, 300e3, 400e3, 500e3, 750e3, 1e6, 1.5e6, 2e6, 3e6, 5e6, 10e6}

type milestoneView struct {
	Amount    int64  `json:"amount"`
	ReachedOn string `json:"reached_on,omitempty"` // the month end (or today) it was first above
	Next      bool   `json:"next,omitempty"`
}

// milestones are the steps reached (with when) and the next one.
func (b *book) milestones() []milestoneView {
	pts := b.series(600)
	out := []milestoneView{}
	for _, step := range milestoneSteps {
		m := minorOf(step, b.base)
		reached := ""
		for _, p := range pts {
			if p.Net >= m {
				reached = p.Date
				break
			}
		}
		if reached != "" {
			out = append(out, milestoneView{Amount: m, ReachedOn: reached})
			continue
		}
		out = append(out, milestoneView{Amount: m, Next: true})
		break
	}
	return out
}

// ---- debts ----

type payoff struct {
	Balance       int64   `json:"balance"`
	Rate          float64 `json:"rate"`
	Payment       int64   `json:"payment"`
	Extra         int64   `json:"extra"`
	Months        int     `json:"months"` // -1 = the payment does not cover the interest
	PaidOffOn     string  `json:"paid_off_on"`
	InterestLeft  int64   `json:"interest_left"`
	InterestMonth int64   `json:"interest_month"` // this month's interest
}

// payoffOf works out how long a debt takes to pay off at a monthly payment
// (plus extra) and what interest is still to come.
func payoffOf(balance int64, rate float64, payment int64, today string, extra int64) payoff {
	p := payoff{Balance: balance, Rate: rate, Payment: payment, Extra: extra}
	r := rate / 100 / 12
	p.InterestMonth = int64(math.Round(float64(balance) * r))
	pay := float64(payment + extra)
	if balance <= 0 {
		return p
	}
	if pay <= 0 || pay <= float64(balance)*r {
		p.Months = -1
		return p
	}
	bal := float64(balance)
	var interest float64
	n := 0
	for bal > 0.5 && n < 1200 {
		i := bal * r
		interest += i
		bal = bal + i - pay
		n++
	}
	p.Months = n
	p.InterestLeft = int64(math.Round(interest))
	if t, err := store.ParseDate(today); err == nil {
		p.PaidOffOn = t.AddDate(0, n, 0).Format("2006-01-02")
	}
	return p
}

func (s *Server) handleDebts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.loadBook(ctx, actorID(ctx), false)
	if err != nil {
		s.fail(w, err, "debts")
		return
	}
	extra := int64(queryInt(r, "extra"))
	type debt struct {
		assetView
		Payoff      payoff  `json:"payoff"`
		PayoffExtra *payoff `json:"payoff_extra,omitempty"`
	}
	list := []debt{}
	var total, monthly, interest int64
	for i := range b.assets {
		a := &b.assets[i]
		if !a.Debt {
			continue
		}
		v := b.view(a)
		d := debt{assetView: v, Payoff: payoffOf(v.Mine, a.Rate, a.Payment, b.today, 0)}
		if extra > 0 {
			pe := payoffOf(v.Mine, a.Rate, a.Payment, b.today, extra)
			d.PayoffExtra = &pe
		}
		list = append(list, d)
		total += v.ValueBase
		monthly += b.fx.toBase(a.Payment, a.Currency, b.today)
		interest += b.fx.toBase(d.Payoff.InterestMonth, a.Currency, b.today)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Rate > list[j].Rate })
	writeJSON(w, http.StatusOK, map[string]any{"currency": b.base, "debts": list, "total": total, "monthly": monthly, "interest_month": interest,
		"missing_rates": b.fx.Missing()})
}

// ---- the plan ----

type planYear struct {
	Year    int   `json:"year"`
	Age     int   `json:"age,omitempty"`
	Nominal int64 `json:"nominal"`
	Real    int64 `json:"real"` // in today's money
}

// handlePlan projects net worth forward: what is owned grows at its
// expected rate, the monthly saving is added, and the target is financial
// independence (yearly spending ÷ the withdrawal rate) unless one is set.
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	out, err := s.plan(ctx, uid)
	if err != nil {
		s.fail(w, err, "plan")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) plan(ctx context.Context, uid int64) (map[string]any, error) {
	b, err := s.loadBook(ctx, uid, false)
	if err != nil {
		return nil, err
	}
	st := b.st
	now := b.totalsAt(b.today)

	// what you own, weighted by its expected change a year
	var weighted, owned float64
	for i := range b.assets {
		a := &b.assets[i]
		if !a.Include || a.Debt {
			continue
		}
		v, ok := b.baseAt(a, b.today)
		if !ok || v <= 0 {
			continue
		}
		weighted += float64(v) * a.GrowthOr()
		owned += float64(v)
	}
	mix := 0.0
	if owned > 0 {
		mix = math.Round(weighted/owned*10) / 10
	}
	ret := mix
	if st.PlanReturn != 0 {
		ret = st.PlanReturn
	}

	saving, spending := st.PlanSaving, st.PlanSpending
	money := map[string]any{"used": false}
	if st.PlanSavingAuto {
		if m, err := s.moneyStats(ctx, uid, b); err == nil && m.Months > 0 {
			saving, spending = m.Saving, m.Spending
			money = map[string]any{"used": true, "months": m.Months, "income": m.Income, "spending": m.Spending, "saving": m.Saving, "from": m.From, "to": m.To}
		} else if err != nil {
			money["error"] = err.Error()
		}
	}
	withdraw := st.PlanWithdraw
	if withdraw <= 0 {
		withdraw = 4
	}
	target, fi := st.PlanTarget, int64(0)
	if spending > 0 {
		fi = int64(math.Round(float64(spending*12) / (withdraw / 100)))
	}
	if target <= 0 {
		target = fi
	}

	// year by year, monthly compounding, debts paid down by their payments
	years := 40
	start, _ := store.ParseDate(b.today)
	r := ret / 100 / 12
	infl := st.PlanInflation / 100
	assets := float64(now.Assets)
	type owed struct{ bal, rate, pay float64 }
	var owes []owed
	for i := range b.assets {
		a := &b.assets[i]
		if !a.Debt || !a.Include {
			continue
		}
		if v, ok := b.baseAt(a, b.today); ok && v > 0 {
			owes = append(owes, owed{float64(v), a.Rate / 100 / 12, float64(b.fx.toBase(a.Payment, a.Currency, b.today))})
		}
	}
	proj := []planYear{{Year: start.Year(), Nominal: now.Net, Real: now.Net}}
	reach, reachYears := "", -1.0
	if target > 0 && now.Net >= target {
		reach, reachYears = b.today, 0
	}
	for m := 1; m <= years*12; m++ {
		assets = assets*(1+r) + float64(saving)
		debts := 0.0
		for i := range owes {
			o := &owes[i]
			if o.bal > 0 {
				o.bal = math.Max(0, o.bal*(1+o.rate)-o.pay) // the payment is already in what you spend
			}
			debts += o.bal
		}
		net := assets - debts
		if reach == "" && target > 0 && net >= float64(target) {
			reach = start.AddDate(0, m, 0).Format("2006-01-02")
			reachYears = math.Round(float64(m)/12*10) / 10
		}
		if m%12 == 0 {
			y := planYear{Year: start.Year() + m/12, Nominal: int64(net), Real: int64(net / math.Pow(1+infl, float64(m)/12))}
			if st.PlanBirthYear > 1900 {
				y.Age = y.Year - st.PlanBirthYear
			}
			proj = append(proj, y)
		}
	}
	out := map[string]any{
		"currency": b.base, "net": now.Net, "assets": now.Assets, "debts": now.Debts, "liquid": now.Liquid,
		"return": ret, "return_mix": mix, "inflation": st.PlanInflation, "withdraw": withdraw,
		"saving": saving, "spending": spending, "money": money,
		"fi_number": fi, "target": target, "target_custom": st.PlanTarget > 0,
		"reach_on": reach, "reach_years": reachYears, "years": proj, "birth_year": st.PlanBirthYear,
		"passive_month": int64(math.Round(float64(now.Net) * withdraw / 100 / 12)),
		"milestones":    b.milestones(),
	}
	if spending > 0 {
		out["emergency_months"] = math.Round(float64(now.Liquid)/float64(spending)*10) / 10
	}
	if st.PlanBirthYear > 1900 && reach != "" {
		if t, err := store.ParseDate(reach); err == nil {
			out["reach_age"] = t.Year() - st.PlanBirthYear
		}
	}
	return out, nil
}

// ---- Pocket Money: what comes in and goes out ----

type moneyStats struct {
	Months   int    `json:"months"`
	Income   int64  `json:"income"`   // a month, on average (base currency)
	Spending int64  `json:"spending"` // a month, on average
	Saving   int64  `json:"saving"`   // income - spending
	From     string `json:"from"`
	To       string `json:"to"`
}

// moneyStats averages the last six whole months in Pocket Money (cached for six hours).
func (s *Server) moneyStats(ctx context.Context, uid int64, b *book) (moneyStats, error) {
	var out moneyStats
	key := "money:stats:" + itoa(uid) + ":" + b.base
	if v, at, ok := s.store.CacheGet(ctx, key); ok {
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < 6*time.Hour {
			if remarshalString(v, &out) == nil {
				return out, nil
			}
		}
	}
	c := s.clients(ctx, uid)
	sib, err := c.conn(ctx, appMoney, scopeTxRead)
	if err != nil {
		return out, err
	}
	today, _ := store.ParseDate(b.today)
	first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	var inc, exp int64
	for i := 1; i <= 6; i++ {
		from := first.AddDate(0, -i, 0)
		to := from.AddDate(0, 1, -1)
		t, err := sib.client.Totals(ctx, from.Format("2006-01-02"), to.Format("2006-01-02"))
		if err != nil {
			return out, err
		}
		if t.Count == 0 {
			continue
		}
		cur := firstNonEmpty(t.Currency, b.base)
		inc += b.fx.toBase(abs64(t.Income), cur, to.Format("2006-01-02"))
		exp += b.fx.toBase(abs64(t.Expense), cur, to.Format("2006-01-02"))
		out.Months++
		if out.To == "" {
			out.To = to.Format("2006-01-02")
		}
		out.From = from.Format("2006-01-02")
	}
	if out.Months > 0 {
		out.Income, out.Spending = inc/int64(out.Months), exp/int64(out.Months)
		out.Saving = out.Income - out.Spending
	}
	_ = s.store.CachePut(ctx, key, mustJSON(out))
	return out, nil
}
