package server

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// ---- assets and debts ----

// assetIn is an asset as sent by the app; nil = not sent.
type assetIn struct {
	Name        *string  `json:"name"`
	Kind        *string  `json:"kind"`
	Currency    *string  `json:"currency"`
	Institution *string  `json:"institution"`
	Number      *string  `json:"number"`
	Notes       *string  `json:"notes"`
	ValueMode   *string  `json:"value_mode"`
	Include     *bool    `json:"include"`
	Share       *float64 `json:"share"`
	Rate        *float64 `json:"rate"`
	Payment     *int64   `json:"payment"`
	Growth      *float64 `json:"growth"`
	MaturesOn   *string  `json:"matures_on"`
	BoughtOn    *string  `json:"bought_on"`
	BoughtPrice *int64   `json:"bought_price"`
	Invested    *int64   `json:"invested"`
	SourceURL   *string  `json:"source_url"`
	ClosedOn    *string  `json:"closed_on"`
	// a first value, with the new asset
	Value *int64  `json:"value"`
	Date  *string `json:"date"`
}

// cleanCurrency upper-cases a three-letter currency code ("" when it is not one).
func cleanCurrency(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	if len(c) != 3 {
		return ""
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return ""
		}
	}
	return c
}

// apply copies what was sent onto a; the error text is for the person.
func (in assetIn) apply(a *store.Asset, keys map[string]bool) string {
	str := func(k string, dst *string, v *string) {
		if keys[k] && v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	str("name", &a.Name, in.Name)
	str("institution", &a.Institution, in.Institution)
	str("number", &a.Number, in.Number)
	str("notes", &a.Notes, in.Notes)
	str("source_url", &a.SourceURL, in.SourceURL)
	if keys["kind"] && in.Kind != nil {
		if !store.ValidKind(*in.Kind) {
			return "unknown kind"
		}
		a.Kind = *in.Kind
	}
	if keys["currency"] && in.Currency != nil {
		c := cleanCurrency(*in.Currency)
		if c == "" {
			return "currency is a three-letter code like EUR"
		}
		a.Currency = c
	}
	if keys["value_mode"] && in.ValueMode != nil {
		if !store.In(*in.ValueMode, store.ValueModes) {
			return "the value comes in by hand, from positions, from a linked app or as an estimate"
		}
		a.ValueMode = *in.ValueMode
	}
	if keys["include"] && in.Include != nil {
		a.Include = *in.Include
	}
	if keys["share"] && in.Share != nil {
		if *in.Share <= 0 || *in.Share > 1 {
			return "your share is more than 0% and at most 100%"
		}
		a.Share = *in.Share
	}
	if keys["rate"] && in.Rate != nil {
		if *in.Rate < 0 || *in.Rate > 100 {
			return "the interest rate is 0 to 100%"
		}
		a.Rate = *in.Rate
	}
	if keys["payment"] && in.Payment != nil {
		if *in.Payment < 0 {
			return "the monthly amount cannot be negative"
		}
		a.Payment = *in.Payment
	}
	if keys["growth"] {
		if in.Growth != nil && (*in.Growth < -100 || *in.Growth > 100) {
			return "the change a year is -100 to 100%"
		}
		a.Growth = in.Growth
	}
	for _, d := range []struct {
		k   string
		dst *string
		v   *string
	}{{"matures_on", &a.MaturesOn, in.MaturesOn}, {"bought_on", &a.BoughtOn, in.BoughtOn}, {"closed_on", &a.ClosedOn, in.ClosedOn}} {
		if keys[d.k] && d.v != nil {
			if !validDate(*d.v) {
				return "a date is YYYY-MM-DD"
			}
			*d.dst = *d.v
		}
	}
	if keys["bought_price"] {
		a.BoughtPrice = in.BoughtPrice
	}
	if keys["invested"] {
		a.Invested = in.Invested
	}
	if a.Name == "" {
		return "give it a name"
	}
	if a.ValueMode == "linked" && a.SourceRef == "" {
		a.ValueMode = "manual"
	}
	return ""
}

func (s *Server) handleListAssets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.loadBook(ctx, actorID(ctx), queryBool(r, "closed"))
	if err != nil {
		s.fail(w, err, "assets")
		return
	}
	q, group := r.URL.Query().Get("q"), r.URL.Query().Get("group")
	out := []assetView{}
	for i := range b.assets {
		a := &b.assets[i]
		if !matches(a, q) || (group != "" && a.Group != group) {
			continue
		}
		out = append(out, b.view(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"currency": b.base, "assets": out, "missing_rates": b.fx.Missing()})
}

func (s *Server) handleCreateAsset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in assetIn
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	a := store.Asset{Kind: "other", Currency: st.Currency, Include: true, Share: 1}
	if in.Kind != nil && store.ValidKind(*in.Kind) {
		a.Kind = *in.Kind
		a.ValueMode = store.KindOf(a.Kind).Mode
	}
	if msg := in.apply(&a, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if app := linkApp(ctx); app != "" {
		a.Source = app
	} else if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		a.Source = "api"
	}
	if err := s.store.CreateAsset(ctx, uid, &a); err != nil {
		s.fail(w, err, "add")
		return
	}
	if in.Value != nil {
		day := store.Today()
		if in.Date != nil && *in.Date != "" && validDate(*in.Date) {
			day = *in.Date
		}
		_ = s.store.PutValuation(ctx, uid, &store.Valuation{AssetID: a.ID, Date: day, Value: abs64(*in.Value), Source: a.Source})
	}
	s.publish(ctx, uid, "wealth.asset_saved", a.Name, map[string]any{"id": a.ID, "name": a.Name, "kind": a.Kind, "created": true})
	s.writeAsset(w, r, uid, a.ID, http.StatusCreated)
}

// writeAsset answers with one asset and all that belongs to it.
func (s *Server) writeAsset(w http.ResponseWriter, r *http.Request, uid, id int64, status int) {
	ctx := r.Context()
	b, err := s.loadBook(ctx, uid, true)
	if err != nil {
		s.fail(w, err, "asset")
		return
	}
	var a *store.Asset
	for i := range b.assets {
		if b.assets[i].ID == id {
			a = &b.assets[i]
		}
	}
	if a == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	v := b.view(a)
	acts, _ := s.store.ListActivity(ctx, uid, store.ActivityFilter{AssetID: id, Limit: 200})
	atts, _ := s.store.Attachments(ctx, uid, id)
	vals := b.vals[id]
	if vals == nil {
		vals = []store.Valuation{}
	}
	pos := []positionView{}
	for _, p := range b.pos[id] {
		pos = append(pos, b.positionView(p))
	}
	hist := []map[string]any{}
	for _, t := range b.assetSeries(a, 60) {
		hist = append(hist, t)
	}
	out := map[string]any{"asset": v, "valuations": vals, "positions": pos, "activity": acts, "attachments": atts, "history": hist,
		"currency": b.base, "missing_rates": b.fx.Missing()}
	if a.Debt {
		out["payoff"] = payoffOf(v.Mine, a.Rate, a.Payment, b.today, 0)
	}
	if a.SourceRef != "" {
		out["source_name"] = sourceAppName(a.SourceRef)
	}
	writeJSON(w, status, out)
}

// assetSeries is one asset's value at each month end and today.
func (b *book) assetSeries(a *store.Asset, months int) []map[string]any {
	out := []map[string]any{}
	for _, t := range b.series(months) {
		v, ok := b.valueAt(a, t.Date)
		if !ok {
			continue
		}
		out = append(out, map[string]any{"date": t.Date, "label": t.Label, "value": mine(a, v), "value_base": b.fx.toBase(mine(a, v), a.Currency, t.Date)})
	}
	return out
}

func (s *Server) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	s.writeAsset(w, r, actorID(r.Context()), idParam(r, "id"), http.StatusOK)
}

// ownedByLink says whether the app acting on a link token may change this row.
func ownedByLink(ctx context.Context, source string) bool {
	app := linkApp(ctx)
	return app == "" || app == source
}

func (s *Server) handleUpdateAsset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, err := s.store.GetAsset(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "asset")
		return
	}
	if !ownedByLink(ctx, a.Source) {
		writeErr(w, http.StatusForbidden, "an app may change only what it added")
		return
	}
	var in assetIn
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := in.apply(a, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateAsset(ctx, uid, a); err != nil {
		s.fail(w, err, "save")
		return
	}
	s.publish(ctx, uid, "wealth.asset_saved", a.Name, map[string]any{"id": a.ID, "name": a.Name, "kind": a.Kind})
	s.writeAsset(w, r, uid, a.ID, http.StatusOK)
}

func (s *Server) handleDeleteAsset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, err := s.store.GetAsset(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "asset")
		return
	}
	if !ownedByLink(ctx, a.Source) {
		writeErr(w, http.StatusForbidden, "an app may delete only what it added")
		return
	}
	if err := s.store.DeleteAsset(ctx, uid, a.ID); err != nil {
		s.fail(w, err, "delete")
		return
	}
	if a.SourceRef != "" {
		_ = s.store.Dismiss(ctx, uid, a.SourceRef, a.Name) // deleting a linked one: don't offer it again
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderAssets(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ReorderAssets(r.Context(), actorID(r.Context()), in.IDs); err != nil {
		s.fail(w, err, "order")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- valuations ----

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (s *Server) handleAddValuation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, err := s.store.GetAsset(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "asset")
		return
	}
	var in struct {
		Date  string `json:"date"`
		Value *int64 `json:"value"`
		Note  string `json:"note"`
	}
	if err := decode(r, &in); err != nil || in.Value == nil {
		writeErr(w, http.StatusBadRequest, "send a value")
		return
	}
	if in.Date == "" {
		in.Date = store.Today()
	}
	if !validDate(in.Date) || in.Date > store.AddDays(store.Today(), 1) {
		writeErr(w, http.StatusBadRequest, "the date is YYYY-MM-DD, today or earlier")
		return
	}
	src := firstNonEmpty(linkApp(ctx), "web")
	v := store.Valuation{AssetID: a.ID, Date: in.Date, Value: abs64(*in.Value), Note: strings.TrimSpace(in.Note), Source: src}
	if err := s.store.PutValuation(ctx, uid, &v); err != nil {
		s.fail(w, err, "save value")
		return
	}
	if a.ValueMode == "linked" && linkApp(ctx) == "" {
		// typing a value over a linked one: from now on it is typed in
		a.ValueMode = "manual"
		_ = s.store.UpdateAsset(ctx, uid, a)
	}
	s.publish(ctx, uid, "wealth.valuation_added", a.Name, map[string]any{"asset_id": a.ID, "name": a.Name, "date": v.Date, "value": v.Value, "currency": a.Currency})
	s.checkMilestonesSoon(uid)
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleDeleteValuation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	v, err := s.store.GetValuation(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "value")
		return
	}
	if !ownedByLink(ctx, v.Source) {
		writeErr(w, http.StatusForbidden, "an app may delete only what it added")
		return
	}
	if err := s.store.DeleteValuation(ctx, uid, v.ID); err != nil {
		s.fail(w, err, "delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- positions ----

type positionView struct {
	store.Position
	Value     int64    `json:"value"`      // own currency
	ValueBase int64    `json:"value_base"` // base currency (the asset's share applied)
	Gain      int64    `json:"gain"`
	GainPct   *float64 `json:"gain_pct"`
}

func (b *book) positionView(p store.Position) positionView {
	v := positionView{Position: p, Value: positionValue(p)}
	share := 1.0
	for _, a := range b.assets {
		if a.ID == p.AssetID && a.Share > 0 {
			share = a.Share
		}
	}
	v.ValueBase = b.fx.toBase(int64(math.Round(float64(v.Value)*share)), p.Currency, b.today)
	v.Gain = v.Value - p.Cost
	if p.Cost > 0 {
		g := math.Round(float64(v.Gain)/float64(p.Cost)*1000) / 10
		v.GainPct = &g
	}
	return v
}

type positionIn struct {
	AssetID   *int64   `json:"asset_id"`
	Name      *string  `json:"name"`
	Symbol    *string  `json:"symbol"`
	Kind      *string  `json:"kind"`
	Units     *float64 `json:"units"`
	Cost      *int64   `json:"cost"`
	Currency  *string  `json:"currency"`
	Price     *float64 `json:"price"`
	PriceOn   *string  `json:"price_on"`
	PriceAuto *bool    `json:"price_auto"`
	Notes     *string  `json:"notes"`
}

func (in positionIn) apply(p *store.Position) string {
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Symbol != nil {
		p.Symbol = strings.ToUpper(strings.TrimSpace(*in.Symbol))
	}
	if in.Kind != nil {
		if !store.In(*in.Kind, store.PositionKinds) {
			return "unknown kind of holding"
		}
		p.Kind = *in.Kind
	}
	if in.Units != nil {
		if *in.Units < 0 {
			return "units cannot be negative"
		}
		p.Units = *in.Units
	}
	if in.Cost != nil {
		p.Cost = abs64(*in.Cost)
	}
	if in.Currency != nil {
		c := cleanCurrency(*in.Currency)
		if c == "" {
			return "currency is a three-letter code like EUR"
		}
		p.Currency = c
	}
	if in.Price != nil {
		if *in.Price < 0 {
			return "the price cannot be negative"
		}
		if *in.Price != p.Price {
			p.PriceOn, p.PriceError = store.Today(), ""
		}
		p.Price = *in.Price
	}
	if in.PriceOn != nil && validDate(*in.PriceOn) {
		p.PriceOn = *in.PriceOn
	}
	if in.PriceAuto != nil {
		p.PriceAuto = *in.PriceAuto
	}
	if in.Notes != nil {
		p.Notes = strings.TrimSpace(*in.Notes)
	}
	if p.Name == "" {
		p.Name = p.Symbol
	}
	if p.Name == "" {
		return "give the holding a name or a symbol"
	}
	return ""
}

func (s *Server) handleListPositions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.loadBook(ctx, actorID(ctx), false)
	if err != nil {
		s.fail(w, err, "positions")
		return
	}
	out := []positionView{}
	byKind := map[string]int64{}
	var value, cost int64
	for _, a := range b.assets {
		for _, p := range b.pos[a.ID] {
			v := b.positionView(p)
			out = append(out, v)
			byKind[p.Kind] += v.ValueBase
			value += v.ValueBase
			cost += b.fx.toBase(int64(math.Round(float64(p.Cost)*shareOf(&a))), p.Currency, b.today)
		}
	}
	since := store.AddDays(b.today, -365)
	acts, _ := s.store.ListActivity(ctx, actorID(ctx), store.ActivityFilter{Kind: "dividend,interest", From: since})
	var income int64
	for _, x := range acts {
		income += b.fx.toBase(x.Amount, x.Currency, x.Date)
	}
	writeJSON(w, http.StatusOK, map[string]any{"currency": b.base, "positions": out, "value": value, "cost": cost, "gain": value - cost,
		"by_kind": byKind, "income_year": income, "online": b.st.Online && !s.cfg.DemoData, "missing_rates": b.fx.Missing()})
}

func shareOf(a *store.Asset) float64 {
	if a.Share <= 0 || a.Share > 1 {
		return 1
	}
	return a.Share
}

func (s *Server) handleCreatePosition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in positionIn
	if err := decode(r, &in); err != nil || in.AssetID == nil {
		writeErr(w, http.StatusBadRequest, "say which asset it is in")
		return
	}
	a, err := s.store.GetAsset(ctx, uid, *in.AssetID)
	if err != nil {
		s.fail(w, err, "asset")
		return
	}
	p := store.Position{AssetID: a.ID, Kind: "fund", Currency: a.Currency, PriceAuto: true}
	if a.Kind == "crypto" {
		p.Kind = "crypto"
	}
	if msg := in.apply(&p); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if p.Price > 0 && p.PriceOn == "" {
		p.PriceOn = store.Today()
	}
	if err := s.store.CreatePosition(ctx, uid, &p); err != nil {
		s.fail(w, err, "add holding")
		return
	}
	if a.ValueMode != "positions" {
		a.ValueMode = "positions"
		_ = s.store.UpdateAsset(ctx, uid, a)
	}
	if p.PriceAuto && p.Symbol != "" {
		s.pricesSoon(uid)
	}
	s.snapshotSoon(uid)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdatePosition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	p, err := s.store.GetPosition(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "holding")
		return
	}
	var in positionIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	symbol := p.Symbol
	if msg := in.apply(p); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdatePosition(ctx, uid, p); err != nil {
		s.fail(w, err, "save holding")
		return
	}
	if p.PriceAuto && p.Symbol != "" && p.Symbol != symbol {
		s.pricesSoon(uid)
	}
	s.snapshotSoon(uid)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePosition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeletePosition(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete")
		return
	}
	s.snapshotSoon(actorID(ctx))
	w.WriteHeader(http.StatusNoContent)
}

// ---- activity ----

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := store.ActivityFilter{Kind: q.Get("kind"), From: q.Get("from"), To: q.Get("to"), Limit: queryInt(r, "limit")}
	f.AssetID = int64(queryInt(r, "asset"))
	if f.Limit <= 0 || f.Limit > 2000 {
		f.Limit = 500
	}
	list, err := s.store.ListActivity(ctx, actorID(ctx), f)
	if err != nil {
		s.fail(w, err, "activity")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleAddActivity records a buy, sell, dividend, deposit… and, unless
// told not to, applies it: a buy or sell changes the holding's units and
// cost, money put in or taken out changes what was invested and (for a
// value typed in by hand) the value itself.
func (s *Server) handleAddActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		store.Activity
		Apply *bool `json:"apply"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	x := in.Activity
	a, err := s.store.GetAsset(ctx, uid, x.AssetID)
	if err != nil {
		s.fail(w, err, "asset")
		return
	}
	if !store.In(x.Kind, store.ActivityKinds) {
		writeErr(w, http.StatusBadRequest, "it is a buy, sell, dividend, interest, deposit, withdrawal or fee")
		return
	}
	if x.Date == "" {
		x.Date = store.Today()
	}
	if !validDate(x.Date) {
		writeErr(w, http.StatusBadRequest, "the date is YYYY-MM-DD")
		return
	}
	x.Amount, x.Units = abs64(x.Amount), math.Abs(x.Units)
	if x.Currency = cleanCurrency(x.Currency); x.Currency == "" {
		x.Currency = a.Currency
	}
	var p *store.Position
	if x.PositionID != nil && *x.PositionID > 0 {
		if p, err = s.store.GetPosition(ctx, uid, *x.PositionID); err != nil || p.AssetID != a.ID {
			writeErr(w, http.StatusBadRequest, "that holding is not in this asset")
			return
		}
		x.Currency = p.Currency
	} else {
		x.PositionID = nil
	}
	if (x.Kind == "buy" || x.Kind == "sell") && (p == nil || x.Units <= 0) {
		writeErr(w, http.StatusBadRequest, "a buy or sell needs a holding and how many units")
		return
	}
	if p != nil && x.Kind == "sell" && x.Units > p.Units+1e-9 {
		writeErr(w, http.StatusBadRequest, "you have fewer units than that")
		return
	}
	x.Note = strings.TrimSpace(x.Note)
	x.Source = firstNonEmpty(linkApp(ctx), "web")
	if err := s.store.InsertActivity(ctx, uid, &x); err != nil {
		s.fail(w, err, "save")
		return
	}
	if in.Apply == nil || *in.Apply {
		s.applyActivity(ctx, uid, a, p, x)
	}
	s.publish(ctx, uid, "wealth.activity_added", a.Name, map[string]any{"id": x.ID, "asset_id": a.ID, "name": a.Name, "kind": x.Kind,
		"date": x.Date, "amount": x.Amount, "currency": x.Currency, "position": x.PositionName, "units": x.Units})
	s.snapshotSoon(uid)
	writeJSON(w, http.StatusCreated, x)
}

func (s *Server) applyActivity(ctx context.Context, uid int64, a *store.Asset, p *store.Position, x store.Activity) {
	switch x.Kind {
	case "buy":
		p.Units += x.Units
		p.Cost += x.Amount
		if x.Units > 0 && x.Date >= p.PriceOn {
			p.Price, p.PriceOn = majorOf(x.Amount, p.Currency)/x.Units, x.Date
		}
		_ = s.store.UpdatePosition(ctx, uid, p)
	case "sell":
		if p.Units > 0 {
			p.Cost -= int64(math.Round(float64(p.Cost) * x.Units / p.Units))
		}
		p.Units = math.Max(0, p.Units-x.Units)
		if p.Units < 1e-9 {
			p.Units, p.Cost = 0, 0
		}
		_ = s.store.UpdatePosition(ctx, uid, p)
	case "deposit", "withdrawal":
		amt, _ := s.convertNow(ctx, uid, x.Amount, x.Currency, a.Currency)
		sign := int64(1)
		if x.Kind == "withdrawal" {
			sign = -1
		}
		inv := sign * amt
		if a.Invested != nil {
			inv += *a.Invested
		}
		inv = max(0, inv)
		a.Invested = &inv
		_ = s.store.UpdateAsset(ctx, uid, a)
		if a.ValueMode == "manual" {
			b, err := s.loadBook(ctx, uid, true)
			if err != nil {
				return
			}
			if last, ok := b.latest(a.ID, x.Date); ok {
				_ = s.store.PutValuation(ctx, uid, &store.Valuation{AssetID: a.ID, Date: x.Date, Value: max(0, last.Value+sign*amt), Note: x.Kind, Source: x.Source})
			}
		}
	}
}

// convertNow converts at today's rate for a person.
func (s *Server) convertNow(ctx context.Context, uid int64, v int64, from, to string) (int64, bool) {
	if from == to {
		return v, true
	}
	b, err := s.loadBook(ctx, uid, false)
	if err != nil {
		return v, false
	}
	return b.fx.convert(v, from, to, b.today)
}

func (s *Server) handleDeleteActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	x, err := s.store.GetActivity(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "activity")
		return
	}
	if !ownedByLink(ctx, x.Source) {
		writeErr(w, http.StatusForbidden, "an app may delete only what it added")
		return
	}
	if err := s.store.DeleteActivity(ctx, uid, x.ID); err != nil {
		s.fail(w, err, "delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- attachments ----

func (s *Server) handleAddAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in store.Attachment
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.AssetID = idParam(r, "id")
	in.Title, in.URL = strings.TrimSpace(in.Title), strings.TrimSpace(in.URL)
	if in.DocumentID == nil && in.URL == "" {
		writeErr(w, http.StatusBadRequest, "pick a document or paste a link")
		return
	}
	if in.URL != "" && !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
		writeErr(w, http.StatusBadRequest, "a link starts with https://")
		return
	}
	if in.DocumentID != nil && in.URL == "" {
		in.URL = s.siblingLink(ctx, uid, appDocuments, "/documents/"+itoa(*in.DocumentID))
	}
	if in.Title == "" {
		in.Title = in.URL
	}
	if err := s.store.AddAttachment(ctx, uid, &in); err != nil {
		s.fail(w, err, "attach")
		return
	}
	writeJSON(w, http.StatusCreated, in)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteAttachment(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- kinds ----

func (s *Server) handleKinds(w http.ResponseWriter, r *http.Request) {
	groups := []map[string]string{}
	for _, g := range store.Groups {
		groups = append(groups, map[string]string{"value": g.Value, "label": g.Label})
	}
	writeJSON(w, http.StatusOK, map[string]any{"kinds": store.Kinds, "groups": groups, "position_kinds": store.PositionKinds, "activity_kinds": store.ActivityKinds})
}

// sortViews puts the biggest first.
func sortViews(list []assetView) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].ValueBase > list[j].ValueBase })
}
