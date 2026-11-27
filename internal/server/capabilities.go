package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Wealth (the capabilities
// value in links.go): one route each, in the shape every Pocket app answers
// in. They read what /search, /day and the Overview read, as the request's
// person, from what is stored here: the values typed in or read from a
// statement, the prices and exchange rates the six-hourly update keeps, and
// the values the hourly run brought from the other apps. None of them looks
// up a price or asks another app; nothing is changed, and nothing is added
// (there is no capture: an asset or a value needs its form).

// dots joins the parts of a line that are there: "12,345 EUR · Savings account · last 3 Nov 2026".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// count is "1 change" or "3 changes".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// longDay writes a day the way the app's pages do: "3 Nov 2026".
func longDay(d string) string {
	t, err := store.ParseDate(d)
	if err != nil {
		return d
	}
	return t.Format("2 Jan 2006")
}

// assetURL opens an asset or a debt in the app.
func assetURL(id int64) string { return "/a/" + itoa(id) }

// hasWord reports whether a query has a letter or a digit to look for.
func hasWord(q string) bool {
	return strings.IndexFunc(q, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// worthLine is what an asset or a debt is worth now, in its own currency
// and the person's part of it: "12,345 EUR", "120,000 EUR owed", "no value yet".
func worthLine(v assetView) string {
	switch {
	case !v.Known:
		return "no value yet"
	case v.Debt:
		return money(v.Mine, v.Currency) + " owed"
	}
	return money(v.Mine, v.Currency)
}

// monthLine is a change since the end of last month, as the Overview writes
// it: "+1,234 EUR (+1.2%)". Empty when nothing moved.
func monthLine(c change, cur string) string {
	if c.Amount == 0 {
		return ""
	}
	s := signed(c.Amount, cur)
	if c.Pct != nil {
		s += fmt.Sprintf(" (%+.1f%%)", *c.Pct)
	}
	return s
}

// netLine is "Net worth 182,400 EUR (+1.2% this month)", the percent only
// when there is one to give and the amount when there is not.
func netLine(net int64, c change, cur string) string {
	s := "Net worth " + money(net, cur)
	switch {
	case c.Amount == 0:
	case c.Pct != nil:
		s += fmt.Sprintf(" (%+.1f%% this month)", *c.Pct)
	default:
		s += " (" + signed(c.Amount, cur) + " this month)"
	}
	return s
}

// ---------- search ----------

// pocketSearch answers Pocket Search with what /search finds: the assets and
// debts whose name, institution, number, notes, kind or currency has every
// word (closed and paid-off ones too, as there), then the holdings by name
// or symbol, opened on the asset they are in. Values are the ones stored.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	out := []core.SearchItem{}
	if !hasWord(q) {
		return out, nil
	}
	ctx := r.Context()
	b, err := s.loadBook(ctx, actorID(ctx), true)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for i := range b.assets {
		a := &b.assets[i]
		names[a.ID] = a.Name
		if !matches(a, q) {
			continue
		}
		v := b.view(a)
		kind, closed := "asset", ""
		if a.Debt {
			kind = "debt"
		}
		if a.Closed(b.today) {
			closed = "closed " + longDay(a.ClosedOn)
			if a.Debt {
				closed = "paid off " + longDay(a.ClosedOn)
			}
		}
		worth, updated := worthLine(v), ""
		switch {
		case closed != "":
			worth = "" // it no longer counts
		case v.Updated != "":
			updated = "last " + longDay(v.Updated)
		}
		out = append(out, core.SearchItem{ID: "asset:" + itoa(a.ID), Title: a.Name, Kind: kind, Date: v.Updated,
			Snippet: dots(worth, store.KindOf(a.Kind).Label, a.Institution, updated, closed), URL: assetURL(a.ID)})
	}
	ql := strings.ToLower(q)
	for i := range b.assets {
		for _, p := range b.pos[b.assets[i].ID] {
			if !strings.Contains(strings.ToLower(p.Name+" "+p.Symbol), ql) {
				continue
			}
			price := ""
			if p.PriceOn != "" {
				price = "price of " + longDay(p.PriceOn)
			}
			out = append(out, core.SearchItem{ID: "holding:" + itoa(p.ID), Title: firstNonEmpty(p.Name, p.Symbol), Kind: "holding", Date: p.PriceOn,
				Snippet: dots(money(positionValue(p), p.Currency), "in "+names[p.AssetID], price), URL: assetURL(p.AssetID)})
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- day ----------

// pocketDay answers Pocket Day with what /day lists (the activity and the
// values typed in or read from a statement that day) and, from the same
// book, what was sold or paid off that day. Above them, net worth at the end
// of that day and how it moved since the month began, whenever a value was
// written down that day (by the person or by Wealth's own hourly snapshot
// of prices and linked values): a day with neither is nothing. Days carry no
// clock time here, so the time zone is not used.
func (s *Server) pocketDay(r *http.Request, date string, _ *time.Location) (*core.Day, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	b, err := s.loadBook(ctx, uid, true)
	if err != nil {
		return nil, err
	}
	if date > b.today {
		return nil, nil
	}
	acts, err := s.store.ListActivity(ctx, uid, store.ActivityFilter{From: date, To: date})
	if err != nil {
		return nil, err
	}
	day := &core.Day{Date: date}
	for i := len(acts) - 1; i >= 0; i-- { // oldest first
		x := acts[i]
		it := core.DayItem{ID: "activity:" + itoa(x.ID), Title: activityTitle(x), Kind: x.Kind, Amount: x.Amount, Currency: x.Currency,
			URL: assetURL(x.AssetID), Note: strings.TrimSpace(x.Note)}
		if x.PositionName != "" {
			it.Sub = x.AssetName
		}
		day.Items = append(day.Items, it)
	}
	// a value someone wrote down that day; the hourly "auto" values Wealth keeps
	// as prices move are not news, so a day with only those stays empty
	recorded := false
	var paidOff []string
	for i := range b.assets {
		a := &b.assets[i]
		if v, ok := b.latest(a.ID, date); ok && v.Date == date {
			if v.Source != "auto" {
				recorded = true
				it := core.DayItem{ID: "value:" + itoa(v.ID), Title: a.Name, Kind: "value", Amount: v.Value, Currency: a.Currency, URL: assetURL(a.ID), Note: strings.TrimSpace(v.Note)}
				was, had := b.latest(a.ID, store.AddDays(date, -1))
				switch {
				case a.Debt && had && v.Value < was.Value:
					it.Kind = "paid"
					it.Sub = "paid down " + money(was.Value-v.Value, a.Currency) + ", " + money(v.Value, a.Currency) + " left"
				case a.Debt:
					it.Sub = money(v.Value, a.Currency) + " owed"
				case had && v.Value != was.Value:
					it.Sub = "new value " + money(v.Value, a.Currency) + " (" + signed(v.Value-was.Value, a.Currency) + ")"
				default:
					it.Sub = "new value " + money(v.Value, a.Currency)
				}
				day.Items = append(day.Items, it)
			}
		}
		if a.ClosedOn == date {
			it := core.DayItem{ID: "closed:" + itoa(a.ID), Title: a.Name, Kind: "closed", Sub: "sold or closed", URL: assetURL(a.ID)}
			if a.Debt {
				it.Kind, it.Sub = "paid off", "paid off"
				paidOff = append(paidOff, a.Name)
			}
			day.Items = append(day.Items, it)
		}
	}
	if len(day.Items) == 0 && !recorded {
		return nil, nil
	}
	var parts []string
	if recorded || (len(day.Items) > 0 && date >= b.firstDay()) {
		now, then := b.totalsAt(date), b.totalsAt(lastMonthEnd(date))
		c := changeOf(now.Net, then.Net, then.Date)
		parts = append(parts, netLine(now.Net, c, b.base))
		day.Metrics = append(day.Metrics, core.Metric{Label: "Net worth", Value: money(now.Net, b.base)})
		if m := monthLine(c, b.base); m != "" {
			day.Metrics = append(day.Metrics, core.Metric{Label: "This month", Value: m})
		}
	}
	if n := len(day.Items); n > 0 {
		parts = append(parts, count(n, "change", "changes"))
	}
	if len(paidOff) > 0 {
		parts = append(parts, "paid off "+strings.Join(paidOff, " and "))
	}
	day.Summary = strings.Join(parts, ", ")
	return day, nil
}

// ---------- widget ----------

// pocketWidget answers Pocket Hub with the Overview's top: net worth now and
// how it moved this month, what is owned and owed, and the biggest few
// assets and debts with what each is worth. The Overview shows every amount
// in full, so the widget does too. Nothing when there is nothing here yet.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	// closed ones too, as on the Overview: what was paid off this month is in the month's change
	b, err := s.loadBook(ctx, actorID(ctx), true)
	if err != nil {
		return nil, err
	}
	var owned, owed []assetView
	for i := range b.assets {
		a := &b.assets[i]
		if a.Closed(b.today) || !a.Include {
			continue
		}
		v := b.view(a)
		switch {
		case !v.Known || v.ValueBase == 0:
		case a.Debt:
			owed = append(owed, v)
		default:
			owned = append(owned, v)
		}
	}
	if len(owned) == 0 && len(owed) == 0 {
		return nil, nil
	}
	now, then := b.totalsAt(b.today), b.totalsAt(lastMonthEnd(b.today))
	c := changeOf(now.Net, then.Net, then.Date)
	w := &core.WidgetData{URL: "/", Line: netLine(now.Net, c, b.base), Metrics: []core.Metric{
		{Label: "You own", Value: money(now.Assets, b.base)},
		{Label: "You owe", Value: money(now.Debts, b.base)},
		{Label: "Net worth", Value: money(now.Net, b.base)},
	}}
	if m := monthLine(c, b.base); m != "" {
		w.Metrics = append(w.Metrics, core.Metric{Label: "This month", Value: m})
	}
	sortViews(owned)
	sortViews(owed)
	// five rows: the debts take two, or more when there are fewer assets
	debts := min(len(owed), max(2, 5-len(owned)))
	assets := min(len(owned), 5-debts)
	for _, v := range owned[:assets] {
		w.Items = append(w.Items, core.WidgetItem{ID: itoa(v.ID), Title: v.Name, Detail: worthLine(v), URL: assetURL(v.ID)})
	}
	for _, v := range owed[:debts] {
		w.Items = append(w.Items, core.WidgetItem{ID: itoa(v.ID), Title: v.Name, Detail: worthLine(v), URL: assetURL(v.ID)})
	}
	return w, nil
}
