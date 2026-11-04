package server

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// ---- what everything is worth, on any day ----
//
// An asset's value on a day is its newest valuation up to that day (typed
// in, read from a statement, or written by Wealth itself from the positions
// or the linked app), except today, when positions and linked apps give the
// live figure, and except estimates, which grow or shrink at the asset's
// rate a year from the last known value (a valuation or the price paid).
// Each value is converted at that day's exchange rate, so a backfilled
// valuation corrects the history by itself.

// decimals is how many minor units a currency has.
func decimals(cur string) int {
	switch cur {
	case "JPY", "KRW", "HUF", "ISK", "CLP", "VND", "IDR", "XAF", "XOF", "UGX", "PYG":
		return 0
	case "KWD", "BHD", "OMR", "JOD", "TND":
		return 3
	}
	return 2
}

func pow10(n int) float64 { return math.Pow(10, float64(n)) }

// minorOf turns a major amount into minor units.
func minorOf(v float64, cur string) int64 { return int64(math.Round(v * pow10(decimals(cur)))) }

// majorOf turns minor units into a major amount.
func majorOf(v int64, cur string) float64 { return float64(v) / pow10(decimals(cur)) }

// pegged currencies have a fixed rate to the euro.
var pegged = map[string]float64{"EUR": 1, "BAM": 1.95583, "XAF": 655.957, "XOF": 655.957, "KMF": 491.96775, "CVE": 110.265, "BGN": 1.95583}

// fxTable converts amounts between currencies on a day.
type fxTable struct {
	base    string
	rates   map[string][]store.Rate // per currency, oldest first
	own     map[string]float64      // a person's own: 1 unit = this many of base
	missing map[string]bool
}

// perEUR is units of cur per euro on day (the newest rate up to the day,
// else the oldest one known).
func (f *fxTable) perEUR(cur, day string) (float64, bool) {
	if v, ok := pegged[cur]; ok {
		return v, true
	}
	list := f.rates[cur]
	if len(list) == 0 {
		return 0, false
	}
	i := sort.Search(len(list), func(i int) bool { return list[i].Date > day })
	if i == 0 {
		return list[0].PerEUR, true
	}
	return list[i-1].PerEUR, true
}

// unitBase is what one unit of cur is worth in the base currency on day.
func (f *fxTable) unitBase(cur, day string) (float64, bool) {
	if cur == f.base {
		return 1, true
	}
	if v, ok := f.own[cur]; ok && v > 0 {
		return v, true
	}
	c, ok1 := f.perEUR(cur, day)
	b, ok2 := f.perEUR(f.base, day)
	if !ok1 || !ok2 || c <= 0 {
		return 0, false
	}
	return b / c, true
}

// convert turns minor units of from into minor units of to.
func (f *fxTable) convert(amount int64, from, to, day string) (int64, bool) {
	if from == to || amount == 0 {
		return amount, true
	}
	a, ok1 := f.unitBase(from, day)
	b, ok2 := f.unitBase(to, day)
	if !ok1 || !ok2 || b == 0 {
		if !ok1 {
			f.missing[from] = true
		}
		if !ok2 {
			f.missing[to] = true
		}
		return 0, false
	}
	return minorOf(majorOf(amount, from)*a/b, to), true
}

// toBase converts minor units of cur into the base currency.
func (f *fxTable) toBase(amount int64, cur, day string) int64 {
	v, _ := f.convert(amount, cur, f.base, day)
	return v
}

// Missing lists the currencies there was no rate for.
func (f *fxTable) Missing() []string {
	out := []string{}
	for c := range f.missing {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// book is one person's assets with everything needed to value them.
type book struct {
	st     store.Settings
	base   string
	today  string
	assets []store.Asset
	vals   map[int64][]store.Valuation // per asset, oldest first
	pos    map[int64][]store.Position
	fx     *fxTable
}

// loadBook reads one person's assets, valuations, positions and the rates they need.
func (s *Server) loadBook(ctx context.Context, uid int64, closed bool) (*book, error) {
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return nil, err
	}
	assets, err := s.store.ListAssets(ctx, uid, closed)
	if err != nil {
		return nil, err
	}
	vals, err := s.store.Valuations(ctx, uid, 0)
	if err != nil {
		return nil, err
	}
	pos, err := s.store.Positions(ctx, uid, 0)
	if err != nil {
		return nil, err
	}
	b := &book{st: st, base: st.Currency, today: store.Today(), assets: assets, vals: map[int64][]store.Valuation{}, pos: map[int64][]store.Position{}}
	for _, v := range vals {
		b.vals[v.AssetID] = append(b.vals[v.AssetID], v)
	}
	for _, p := range pos {
		b.pos[p.AssetID] = append(b.pos[p.AssetID], p)
	}
	curs := map[string]bool{st.Currency: true}
	for _, a := range assets {
		curs[a.Currency] = true
	}
	for _, p := range pos {
		curs[p.Currency] = true
	}
	list := make([]string, 0, len(curs))
	for c := range curs {
		list = append(list, c)
	}
	rates, err := s.store.AllRates(ctx, list)
	if err != nil {
		return nil, err
	}
	b.fx = &fxTable{base: st.Currency, rates: rates, own: st.Rates, missing: map[string]bool{}}
	return b, nil
}

// latest is an asset's newest valuation up to day.
func (b *book) latest(id int64, day string) (store.Valuation, bool) {
	list := b.vals[id]
	i := sort.Search(len(list), func(i int) bool { return list[i].Date > day })
	if i == 0 {
		return store.Valuation{}, false
	}
	return list[i-1], true
}

// positionValue is a holding's value in its own currency (minor units).
func positionValue(p store.Position) int64 { return minorOf(p.Units*p.Price, p.Currency) }

// positionsValue is the live value of an asset's holdings, in the asset's currency.
func (b *book) positionsValue(a *store.Asset) int64 {
	var sum int64
	for _, p := range b.pos[a.ID] {
		v, _ := b.fx.convert(positionValue(p), p.Currency, a.Currency, b.today)
		sum += v
	}
	return sum
}

// estimate grows (or shrinks) a value at g % a year from one day to another.
func estimate(v int64, from, to string, g float64) int64 {
	tf, err1 := store.ParseDate(from)
	tt, err2 := store.ParseDate(to)
	if err1 != nil || err2 != nil || !tt.After(tf) || g == 0 {
		return v
	}
	years := tt.Sub(tf).Hours() / 24 / 365.25
	return int64(math.Round(float64(v) * math.Pow(1+g/100, years)))
}

// valueAt is an asset's whole value (before its share) on day, in its own
// currency; false when it did not exist yet or no longer counts.
func (b *book) valueAt(a *store.Asset, day string) (int64, bool) {
	if a.Closed(day) {
		return 0, false
	}
	live := day >= b.today
	switch a.ValueMode {
	case "positions":
		if live {
			if len(b.pos[a.ID]) == 0 {
				if v, ok := b.latest(a.ID, day); ok {
					return v.Value, true
				}
			}
			return b.positionsValue(a), true
		}
	case "linked":
		if live && a.LinkedValue != nil {
			return *a.LinkedValue, true
		}
	case "estimate":
		g := a.GrowthOr()
		if v, ok := b.latest(a.ID, day); ok {
			return estimate(v.Value, v.Date, day, g), true
		}
		if a.BoughtPrice != nil && a.BoughtOn != "" && a.BoughtOn <= day {
			return estimate(*a.BoughtPrice, a.BoughtOn, day, g), true
		}
		if a.BoughtPrice != nil && a.BoughtOn == "" && live {
			return *a.BoughtPrice, true
		}
		return 0, false
	}
	if v, ok := b.latest(a.ID, day); ok {
		return v.Value, true
	}
	if live && a.ValueMode == "linked" {
		return 0, false
	}
	return 0, false
}

// mine is the part of a value that is the person's (the share), still in the asset's currency.
func mine(a *store.Asset, v int64) int64 {
	if a.Share <= 0 || a.Share >= 1 {
		return v
	}
	return int64(math.Round(float64(v) * a.Share))
}

// baseAt is the person's part of an asset on day, in the base currency;
// debts are positive (what is owed).
func (b *book) baseAt(a *store.Asset, day string) (int64, bool) {
	v, ok := b.valueAt(a, day)
	if !ok {
		return 0, false
	}
	return b.fx.toBase(mine(a, v), a.Currency, day), true
}

// totals is net worth on one day.
type totals struct {
	Date   string           `json:"date"`
	Label  string           `json:"label,omitempty"`
	Net    int64            `json:"net"`
	Assets int64            `json:"assets"`
	Debts  int64            `json:"debts"`
	Liquid int64            `json:"liquid"`
	Groups map[string]int64 `json:"groups"`
}

// totalsAt adds up everything that counts on day.
func (b *book) totalsAt(day string) totals {
	t := totals{Date: day, Groups: map[string]int64{}}
	for i := range b.assets {
		a := &b.assets[i]
		if !a.Include {
			continue
		}
		v, ok := b.baseAt(a, day)
		if !ok {
			continue
		}
		t.Groups[a.Group] += v
		if a.Debt {
			t.Debts += v
		} else {
			t.Assets += v
			if a.Liquid {
				t.Liquid += v
			}
		}
	}
	t.Net = t.Assets - t.Debts
	return t
}

// firstDay is the earliest valuation (else the earliest purchase of an
// estimate, else today): the history starts there.
func (b *book) firstDay() string {
	first, bought := "", ""
	for _, a := range b.assets {
		if list := b.vals[a.ID]; len(list) > 0 && (first == "" || list[0].Date < first) {
			first = list[0].Date
		}
		if a.ValueMode == "estimate" && a.BoughtPrice != nil && a.BoughtOn != "" && (bought == "" || a.BoughtOn < bought) {
			bought = a.BoughtOn
		}
	}
	return firstNonEmpty(first, bought, b.today)
}

// monthEnd is the last day of the month that contains t.
func monthEnd(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
}

var monthNames = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// series is net worth at each month end from the first known month (at
// most months back), and today.
func (b *book) series(months int) []totals {
	today, _ := store.ParseDate(b.today)
	first, _ := store.ParseDate(b.firstDay())
	start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -months, 0)
	if fm := time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC); fm.After(start) {
		start = fm
	}
	out := []totals{}
	for m := start; ; m = m.AddDate(0, 1, 0) {
		end := monthEnd(m)
		if !end.Before(today) {
			break
		}
		t := b.totalsAt(end.Format("2006-01-02"))
		t.Label = monthNames[end.Month()-1] + " " + end.Format("06")
		out = append(out, t)
	}
	t := b.totalsAt(b.today)
	t.Label = "Today"
	out = append(out, t)
	return out
}

// lastUpdated is the day an asset's value was last known to be right.
func (b *book) lastUpdated(a *store.Asset) string {
	day := ""
	if v, ok := b.latest(a.ID, "9999-12-31"); ok {
		day = v.Date
	}
	switch a.ValueMode {
	case "linked":
		if a.LinkedAt != "" && a.LinkedError == "" {
			return b.today
		}
	case "positions":
		for _, p := range b.pos[a.ID] {
			if p.PriceOn > day {
				day = p.PriceOn
			}
		}
	case "estimate":
		if day == "" {
			day = a.BoughtOn
		}
	}
	return day
}

// stale reports whether a typed-in value is too old to trust.
func (b *book) stale(a *store.Asset) bool {
	if a.Closed(b.today) || a.ValueMode == "estimate" || a.ValueMode == "linked" {
		return false
	}
	d := b.lastUpdated(a)
	if d == "" {
		return true
	}
	limit := max(7, b.st.StaleDays)
	if store.Slow(a.Kind) {
		limit = max(limit, 365)
	}
	return store.DaysBetween(d, b.today) > limit
}

// cost is what was put into an asset (for the gain), in its own currency.
func (b *book) cost(a *store.Asset) (int64, bool) {
	if a.Debt {
		return 0, false
	}
	if a.ValueMode == "positions" && len(b.pos[a.ID]) > 0 {
		var sum int64
		for _, p := range b.pos[a.ID] {
			v, _ := b.fx.convert(p.Cost, p.Currency, a.Currency, b.today)
			sum += v
		}
		return sum, sum > 0
	}
	if a.Invested != nil {
		return *a.Invested, true
	}
	if a.BoughtPrice != nil {
		return *a.BoughtPrice, true
	}
	return 0, false
}

// assetView is an asset with what it is worth now.
type assetView struct {
	store.Asset
	Value        int64    `json:"value"`      // whole, own currency
	Mine         int64    `json:"mine"`       // the person's share, own currency
	ValueBase    int64    `json:"value_base"` // the person's share, base currency
	Known        bool     `json:"known"`      // there is a value at all
	MonthChange  int64    `json:"month_change"`
	Updated      string   `json:"updated"`
	Stale        bool     `json:"stale"`
	Cost         *int64   `json:"cost"`
	Gain         *int64   `json:"gain"`
	GainPct      *float64 `json:"gain_pct"`
	DaysToMature *int     `json:"days_to_mature,omitempty"`
}

func (b *book) view(a *store.Asset) assetView {
	v := assetView{Asset: *a}
	val, ok := b.valueAt(a, b.today)
	v.Known = ok
	v.Value = val
	v.Mine = mine(a, val)
	v.ValueBase = b.fx.toBase(v.Mine, a.Currency, b.today)
	if prev, ok2 := b.baseAt(a, lastMonthEnd(b.today)); ok2 && ok {
		v.MonthChange = v.ValueBase - prev
	} else if ok {
		v.MonthChange = v.ValueBase
	}
	v.Updated = b.lastUpdated(a)
	v.Stale = b.stale(a)
	if c, ok := b.cost(a); ok && v.Known {
		c = mine(a, c)
		g := v.Mine - c
		v.Cost, v.Gain = &c, &g
		if c > 0 {
			p := math.Round(float64(g)/float64(c)*1000) / 10
			v.GainPct = &p
		}
	}
	if a.MaturesOn != "" && !a.Closed(b.today) {
		d := store.DaysBetween(b.today, a.MaturesOn)
		v.DaysToMature = &d
	}
	if lv, ok := b.latest(a.ID, "9999-12-31"); ok {
		v.LastValue = &lv
	}
	return v
}

// lastMonthEnd is the last day of the month before day.
func lastMonthEnd(day string) string {
	t, err := store.ParseDate(day)
	if err != nil {
		return day
	}
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1).Format("2006-01-02")
}

// lastYearEnd is 31 December of the year before day.
func lastYearEnd(day string) string {
	t, err := store.ParseDate(day)
	if err != nil {
		return day
	}
	return time.Date(t.Year()-1, 12, 31, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// matches is a loose, case-insensitive search over an asset's words.
func matches(a *store.Asset, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return true
	}
	hay := strings.ToLower(strings.Join([]string{a.Name, a.Institution, a.Number, a.Notes, store.KindOf(a.Kind).Label, a.Currency}, " "))
	for _, w := range strings.Fields(q) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}
