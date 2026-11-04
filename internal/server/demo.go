package server

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// seedDemo fills the owner's account with three years of a household's
// money: current account, savings, a term deposit about to mature, a broker
// account and a crypto wallet with monthly buys and dividends, a pension,
// a flat (in BAM), a car valued as an estimate, valuables, money lent to a
// friend, a mortgage, a credit card and a car loan paid off along the way
// (DEMO_DATA, for `make run`, the CI smoke test and the screenshots). Dates
// are relative to today, so the demo always looks current.
func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	// with AUTH_MODE=none this runs inside the first request; finish even
	// when that client goes away
	ctx = context.WithoutCancel(ctx)
	if have, _ := s.store.ListAssets(ctx, uid, true); len(have) > 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(42))
	now := time.Now()
	today := store.Today()
	const months = 36
	// month ends, oldest first; the last one is today
	ends := make([]string, 0, months+1)
	for i := months; i >= 1; i-- {
		m := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -i, 0)
		ends = append(ends, monthEnd(m).Format("2006-01-02"))
	}
	ends = append(ends, today)
	n := len(ends)
	day := func(d int) string { return store.AddDays(today, d) }
	p := func(v int64) *int64 { return &v }
	f := func(v float64) *float64 { return &v }

	st, _ := s.store.GetSettings(ctx, uid)
	st.Currency = "EUR"
	st.PlanSavingAuto, st.PlanSaving, st.PlanSpending, st.PlanBirthYear = false, 90000, 210000, 1988
	_ = s.store.SaveUserSettings(ctx, uid, st)

	// USD rates, month by month, around 1.08
	var rates []store.Rate
	usd := 1.07
	for _, d := range ends {
		usd += (rng.Float64() - 0.5) * 0.03
		usd = math.Max(1.02, math.Min(1.14, usd))
		rates = append(rates, store.Rate{Currency: "USD", Date: d, PerEUR: math.Round(usd*10000) / 10000, Source: "demo"})
	}
	_ = s.store.PutRates(ctx, rates)

	add := func(a store.Asset) *store.Asset {
		a.Include, a.Source = true, "demo"
		if a.Share == 0 {
			a.Share = 1
		}
		if a.Currency == "" {
			a.Currency = "EUR"
		}
		if err := s.store.CreateAsset(ctx, uid, &a); err != nil {
			s.log.Warn("demo asset", "err", err)
		}
		return &a
	}
	val := func(a *store.Asset, d string, v int64, src, note string) {
		_ = s.store.PutValuation(ctx, uid, &store.Valuation{AssetID: a.ID, Date: d, Value: v, Source: src, Note: note})
	}
	act := func(a *store.Asset, pos *store.Position, d, kind string, units float64, amount int64, cur string) {
		x := store.Activity{AssetID: a.ID, Date: d, Kind: kind, Units: units, Amount: amount, Currency: cur, Source: "demo"}
		if pos != nil {
			x.PositionID = &pos.ID
		}
		_ = s.store.InsertActivity(ctx, uid, &x)
	}

	// ---- cash ----
	cur := add(store.Asset{Name: "Current account", Kind: "cash", Institution: "Raiffeisen Bank", Number: "1610…4471", ValueMode: "manual"})
	for _, d := range ends {
		val(cur, d, int64(180000+rng.Intn(240000)), "web", "")
	}
	sav := add(store.Asset{Name: "Savings", Kind: "savings", Institution: "Raiffeisen Bank", Rate: 1.5, Payment: 30000, ValueMode: "manual", Invested: p(1150000)})
	for i, d := range ends {
		val(sav, d, int64(620000+i*23500+rng.Intn(20000)), "web", "")
	}
	dep := add(store.Asset{Name: "Term deposit, 12 months", Kind: "deposit", Institution: "UniCredit", Rate: 3.2, MaturesOn: day(41), ValueMode: "estimate",
		Invested: p(1000000), Notes: "Rolls over unless I say otherwise a week before."})
	val(dep, day(-324), 1000000, "web", "opened")
	act(dep, nil, day(-324), "deposit", 0, 1000000, "EUR")

	// ---- the broker account ----
	brk := add(store.Asset{Name: "Broker account", Kind: "investment", Institution: "Interactive Brokers", Number: "U7731…", ValueMode: "positions"})
	vwce := store.Position{AssetID: brk.ID, Name: "Vanguard FTSE All-World (Acc)", Symbol: "VWCE.DE", Kind: "etf", Currency: "EUR", PriceAuto: true}
	aapl := store.Position{AssetID: brk.ID, Name: "Apple", Symbol: "AAPL", Kind: "stock", Currency: "USD", PriceAuto: true}
	aggh := store.Position{AssetID: brk.ID, Name: "iShares Global Aggregate Bond (EUR hedged)", Symbol: "AGGH.AS", Kind: "bond", Currency: "EUR", PriceAuto: true}
	for _, x := range []*store.Position{&vwce, &aapl, &aggh} {
		_ = s.store.CreatePosition(ctx, uid, x)
	}
	pv, pa, pb := 96.0, 148.0, 4.95
	var uv, ua, ub float64
	var cv, ca, cb int64
	for i, d := range ends {
		pv *= 1 + 0.0085 + (rng.Float64()-0.5)*0.07
		pa *= 1 + 0.011 + (rng.Float64()-0.5)*0.10
		pb *= 1 + 0.002 + (rng.Float64()-0.5)*0.015
		if i < n-1 { // a monthly buy of the world fund
			amt := int64(30000)
			u := math.Round(float64(amt)/100/pv*1000) / 1000
			uv += u
			cv += amt
			act(brk, &vwce, store.AddDays(d, -20), "buy", u, amt, "EUR")
		}
		if i == 12 {
			ua, ca = 15, int64(math.Round(15*pa*100))
			act(brk, &aapl, store.AddDays(d, -9), "buy", 15, ca, "USD")
		}
		if i == 18 {
			ub, cb = 400, int64(math.Round(400*pb*100))
			act(brk, &aggh, store.AddDays(d, -3), "buy", 400, cb, "EUR")
		}
		if ua > 0 && i%3 == 1 && i < n-1 {
			act(brk, &aapl, store.AddDays(d, -15), "dividend", 0, int64(math.Round(ua*0.25*100)), "USD")
		}
		if i < n-1 {
			v := uv*pv*100 + ua*pa*100/rates[i].PerEUR + ub*pb*100
			val(brk, d, int64(math.Round(v)), "auto", "")
		}
	}
	vwce.Units, vwce.Cost, vwce.Price, vwce.PriceOn = uv, cv, math.Round(pv*100)/100, today
	aapl.Units, aapl.Cost, aapl.Price, aapl.PriceOn = ua, ca, math.Round(pa*100)/100, today
	aggh.Units, aggh.Cost, aggh.Price, aggh.PriceOn = ub, cb, math.Round(pb*1000)/1000, today
	for _, x := range []*store.Position{&vwce, &aapl, &aggh} {
		_ = s.store.UpdatePosition(ctx, uid, x)
	}

	// ---- crypto ----
	cry := add(store.Asset{Name: "Crypto wallet", Kind: "crypto", Institution: "Ledger", ValueMode: "positions"})
	btc := store.Position{AssetID: cry.ID, Name: "Bitcoin", Symbol: "BTC-EUR", Kind: "crypto", Currency: "EUR", PriceAuto: true}
	eth := store.Position{AssetID: cry.ID, Name: "Ethereum", Symbol: "ETH-EUR", Kind: "crypto", Currency: "EUR", PriceAuto: true}
	_ = s.store.CreatePosition(ctx, uid, &btc)
	_ = s.store.CreatePosition(ctx, uid, &eth)
	pbtc, peth := 26500.0, 1650.0
	var ubtc, ueth float64
	var cbtc, ceth int64
	for i, d := range ends {
		pbtc *= 1 + 0.025 + (rng.Float64()-0.5)*0.22
		peth *= 1 + 0.012 + (rng.Float64()-0.5)*0.26
		if i == 6 || i == 20 {
			u := 0.08
			ubtc += u
			c := int64(math.Round(u * pbtc * 100))
			cbtc += c
			act(cry, &btc, store.AddDays(d, -11), "buy", u, c, "EUR")
		}
		if i == 9 {
			ueth = 1.2
			ceth = int64(math.Round(1.2 * peth * 100))
			act(cry, &eth, store.AddDays(d, -4), "buy", 1.2, ceth, "EUR")
		}
		if i < n-1 && (ubtc > 0 || ueth > 0) {
			val(cry, d, int64(math.Round(ubtc*pbtc*100+ueth*peth*100)), "auto", "")
		}
	}
	btc.Units, btc.Cost, btc.Price, btc.PriceOn = ubtc, cbtc, math.Round(pbtc), today
	eth.Units, eth.Cost, eth.Price, eth.PriceOn = ueth, ceth, math.Round(peth), today
	_ = s.store.UpdatePosition(ctx, uid, &btc)
	_ = s.store.UpdatePosition(ctx, uid, &eth)

	// ---- pension: a statement every quarter, the last one five months ago ----
	pen := add(store.Asset{Name: "Voluntary pension fund", Kind: "pension", Institution: "Raiffeisen Invest", Payment: 15000, ValueMode: "manual"})
	inv, value := int64(1200000), 1290000.0
	for i, d := range ends {
		inv += 15000
		value = value*(1+0.004+(rng.Float64()-0.5)*0.02) + 15000
		if i%3 == 0 && i < n-5 {
			val(pen, d, int64(math.Round(value)), "web", "quarterly statement")
		}
	}
	pen.Invested = &inv
	_ = s.store.UpdateAsset(ctx, uid, pen)

	// ---- property, in BAM ----
	flat := add(store.Asset{Name: "Flat in Sarajevo", Kind: "property", Currency: "BAM", BoughtOn: "2019-05-20", BoughtPrice: p(28000000),
		ValueMode: "manual", Growth: f(3), Notes: "72 m², Grbavica. Valued from the asking prices of similar flats."})
	for i, v := range []int64{29000000, 30500000, 31800000} {
		val(flat, ends[i*12], v, "web", "estimate from listings")
	}
	val(flat, day(-60), 32500000, "web", "estimate from listings")
	_ = s.store.AddAttachment(ctx, uid, &store.Attachment{AssetID: flat.ID, Title: "Purchase contract", URL: "https://example.com/contract.pdf"})

	// ---- the car: an estimate from its price, corrected by a dealer quote ----
	car := add(store.Asset{Name: "VW Golf 7", Kind: "vehicle", Number: "A12-K-345", BoughtOn: "2021-03-15", BoughtPrice: p(1850000), ValueMode: "estimate"})
	val(car, day(-380), 1050000, "web", "dealer quote")

	// ---- valuables, typed in two years ago; money lent ----
	jw := add(store.Asset{Name: "Watch and jewellery", Kind: "valuables", ValueMode: "manual"})
	val(jw, ends[10], 350000, "web", "")
	lent := add(store.Asset{Name: "Money lent to Marko", Kind: "receivable", ValueMode: "manual", MaturesOn: day(120)})
	val(lent, day(-200), 150000, "web", "to be paid back by spring")

	// ---- debts ----
	mort := add(store.Asset{Name: "Mortgage", Kind: "mortgage", Currency: "BAM", Institution: "UniCredit", Rate: 4.2, Payment: 145000, MaturesOn: day(236),
		ValueMode: "manual", Notes: "The fixed rate ends on the date below; after that it follows the 6-month EURIBOR."})
	bal := 17200000.0
	for _, d := range ends {
		bal = bal*(1+0.042/12) - 145000
		val(mort, d, int64(math.Round(bal)), "web", "")
	}
	card := add(store.Asset{Name: "Credit card", Kind: "credit_card", Institution: "Raiffeisen Bank", Rate: 17.9, ValueMode: "manual"})
	for _, d := range ends {
		val(card, d, int64(25000+rng.Intn(70000)), "web", "")
	}
	loan := add(store.Asset{Name: "Car loan", Kind: "loan", Institution: "Sparkasse", Rate: 6.9, Payment: 40000, ValueMode: "manual"})
	left := 600000.0
	for _, d := range ends {
		left = math.Max(0, left*(1+0.069/12)-40000)
		val(loan, d, int64(math.Round(left)), "web", "")
		if left == 0 {
			_ = s.store.SetClosed(ctx, uid, loan.ID, store.AddDays(d, 1))
			break
		}
	}
	s.checkMilestones(ctx, uid)
	return nil
}
