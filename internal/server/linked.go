package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// ---- what the other Pocket apps already know ----
//
// Money accounts and loans, Garage vehicles, Home homes, the valuable
// things in Inventory and the places let out in Rentals are offered as
// suggestions; added, they stay linked: a Money balance and the Inventory
// total are read again every hour (and a few seconds after a change, with
// the events link), a Rentals place's value every hour, a vehicle follows
// its purchase price and is closed when Garage says it was sold, a home
// keeps its name. Source refs: money:account:<id>, garage:vehicle:<id>,
// home:home:<id>, inventory:items, rentals:property:<id>.

// suggestion is something in another app that could be an asset.
type suggestion struct {
	Ref       string `json:"ref"`
	App       string `json:"app"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	Kind      string `json:"kind"`
	Value     *int64 `json:"value"`
	Currency  string `json:"currency"`
	URL       string `json:"url"`
	AssetID   *int64 `json:"asset_id"`
	Dismissed bool   `json:"dismissed"`

	asset   store.Asset // what adding it creates
	homeRef string      // a Rentals place that is also a Pocket Home home: its id there
}

type appSuggestions struct {
	App       string       `json:"app"`
	Name      string       `json:"name"`
	Connected bool         `json:"connected"`
	Error     string       `json:"error,omitempty"`
	Items     []suggestion `json:"items"`
}

// moneyKind maps a Money account kind to a Wealth kind.
func moneyKind(a pocket.Account) string {
	switch a.Kind {
	case "checking", "cash":
		return "cash"
	case "savings":
		return "savings"
	case "credit":
		return "credit_card"
	case "investment":
		return "investment"
	case "loan", "mortgage", "debt":
		if a.Direction == "owed" {
			return "receivable"
		}
		return a.Kind
	}
	return "other"
}

// moneyValue is what an account is worth to Wealth: what is in it, or for a
// debt what is still owed (both positive).
func moneyValue(a pocket.Account) int64 {
	if store.KindOf(moneyKind(a)).Debt || a.Kind == "credit" {
		if a.Balance < 0 {
			return -a.Balance
		}
		return 0
	}
	if a.Balance < 0 && moneyKind(a) == "receivable" {
		return -a.Balance
	}
	return max(0, a.Balance)
}

func appName(slug string) string {
	if a, ok := appBySlug(slug); ok {
		return a.name
	}
	return slug
}

// sourceAppName is the app a source ref points into.
func sourceAppName(ref string) string { return appName(strings.SplitN(ref, ":", 2)[0]) }

// suggestionsFor lists, per app, what could be added (nothing is written).
func (s *Server) suggestionsFor(ctx context.Context, uid int64) ([]appSuggestions, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	assets, err := s.store.ListAssets(ctx, uid, true)
	if err != nil {
		return nil, err
	}
	have := map[string]int64{}
	for _, a := range assets {
		if a.SourceRef != "" {
			have[a.SourceRef] = a.ID
		}
	}
	dismissed, _ := s.store.Dismissed(ctx, uid)
	c := s.clients(ctx, uid)
	out := []appSuggestions{}
	for _, app := range linkedApps {
		g := appSuggestions{App: app.slug, Name: app.name, Items: []suggestion{}}
		items, err := s.appSuggestions(ctx, c, st, app)
		if err != nil {
			if !isNotConnected(err) {
				g.Connected = true
			}
			g.Error = err.Error()
		} else {
			g.Connected = true
		}
		for _, it := range items {
			if id, ok := have[it.Ref]; ok {
				it.AssetID = &id
			}
			_, it.Dismissed = dismissed[it.Ref]
			if _, ok := have["home:home:"+it.homeRef]; ok && it.homeRef != "" && it.AssetID == nil {
				it.Subtitle += " · already here from Pocket Home: add only one"
			}
			g.Items = append(g.Items, it)
		}
		out = append(out, g)
	}
	return out, nil
}

// linkedApps are the apps whose things can become linked assets, in the
// order From my apps shows them.
var linkedApps = []pocketApp{appMoney, appGarage, appHome, appInventory, appRentals}

// appSuggestions reads one app.
func (s *Server) appSuggestions(ctx context.Context, c *clients, st store.Settings, app pocketApp) ([]suggestion, error) {
	switch app.slug {
	case "money":
		sib, err := c.conn(ctx, appMoney, scopeAccountsRead)
		if err != nil {
			return nil, err
		}
		accts, err := sib.client.Accounts(ctx)
		if err != nil {
			return nil, err
		}
		out := []suggestion{}
		for _, a := range accts {
			k := moneyKind(a)
			v := moneyValue(a)
			sub := store.KindOf(k).Label
			if a.Institution != "" {
				sub += " · " + a.Institution
			}
			if a.Role != "" && a.Role != "owner" {
				sub += " · shared with you"
			}
			ref := "money:account:" + strconv.FormatInt(a.ID, 10)
			url := strings.TrimRight(sib.public, "/") + "/accounts/" + strconv.FormatInt(a.ID, 10)
			out = append(out, suggestion{Ref: ref, App: "money", Title: a.Name, Subtitle: sub, Kind: k, Value: &v, Currency: a.Currency, URL: url,
				asset: store.Asset{Name: a.Name, Kind: k, Currency: a.Currency, Institution: a.Institution, Number: a.Number, ValueMode: "linked",
					Include: a.IncludeNetWorth || store.KindOf(k).Debt, Share: 1, Rate: a.InterestRate, Payment: abs64(a.Payment), Source: "money", SourceRef: ref,
					SourceURL: url, LinkedValue: &v, LinkedAt: store.Now()}})
		}
		return out, nil
	case "garage":
		sib, err := c.conn(ctx, appGarage, scopeVehiclesRead)
		if err != nil {
			return nil, err
		}
		vs, err := sib.client.Vehicles(ctx)
		if err != nil {
			return nil, err
		}
		out := []suggestion{}
		for _, v := range vs {
			if v.Status == "archived" {
				continue
			}
			ref := "garage:vehicle:" + strconv.FormatInt(v.ID, 10)
			url := strings.TrimRight(sib.public, "/") + "/vehicles/" + strconv.FormatInt(v.ID, 10)
			a := store.Asset{Name: v.Title(), Kind: "vehicle", Currency: firstNonEmpty(v.Currency, st.Currency), Number: v.Plate, ValueMode: "estimate",
				Include: true, Share: 1, BoughtOn: v.PurchaseDate, Source: "garage", SourceRef: ref, SourceURL: url}
			if v.PurchasePrice > 0 {
				p := v.PurchasePrice
				a.BoughtPrice = &p
			}
			if v.Status == "sold" {
				a.ClosedOn = firstNonEmpty(v.SoldDate, store.Today())
			}
			sub := "Vehicle"
			if v.Plate != "" {
				sub += " · " + v.Plate
			}
			if v.Status == "sold" {
				sub += " · sold"
			}
			var val *int64
			if a.BoughtPrice != nil {
				e := estimate(*a.BoughtPrice, firstNonEmpty(a.BoughtOn, store.Today()), store.Today(), a.GrowthOr())
				val = &e
			} else {
				sub += " · no purchase price in Garage"
			}
			out = append(out, suggestion{Ref: ref, App: "garage", Title: a.Name, Subtitle: sub, Kind: "vehicle", Value: val, Currency: a.Currency, URL: url, asset: a})
		}
		return out, nil
	case "home":
		sib, err := c.conn(ctx, appHome, scopeHomeRead)
		if err != nil {
			return nil, err
		}
		hs, err := sib.client.Homes(ctx)
		if err != nil {
			return nil, err
		}
		out := []suggestion{}
		for _, h := range hs {
			if h.Archived {
				continue
			}
			ref := "home:home:" + strconv.FormatInt(h.ID, 10)
			url := strings.TrimRight(sib.public, "/") + "/homes/" + strconv.FormatInt(h.ID, 10)
			sub := "Home"
			if h.Address != "" {
				sub += " · " + h.Address
			}
			if h.AreaM2 != nil && *h.AreaM2 > 0 {
				sub += fmt.Sprintf(" · %.0f m²", *h.AreaM2)
			}
			out = append(out, suggestion{Ref: ref, App: "home", Title: h.Name, Subtitle: sub + " · you add its value", Kind: "property",
				Currency: firstNonEmpty(h.Currency, st.Currency), URL: url,
				asset: store.Asset{Name: h.Name, Kind: "property", Currency: firstNonEmpty(h.Currency, st.Currency), Notes: h.Address, ValueMode: "manual",
					Include: true, Share: 1, Source: "home", SourceRef: ref, SourceURL: url}})
		}
		return out, nil
	case "rentals":
		sib, err := c.conn(ctx, appRentals, scopeRentalsRead)
		if err != nil {
			return nil, err
		}
		props, err := sib.client.Properties(ctx)
		if err != nil {
			return nil, err
		}
		out := []suggestion{}
		for _, p := range props {
			if p.Archived || p.Value <= 0 {
				continue // nothing to count without a value there
			}
			ref := "rentals:property:" + strconv.FormatInt(p.ID, 10)
			url := strings.TrimRight(sib.public, "/") + "/properties/" + strconv.FormatInt(p.ID, 10)
			cur := firstNonEmpty(p.Currency, st.Currency)
			v := p.Value
			a := store.Asset{Name: p.Name, Kind: "property", Currency: cur, Notes: strings.Trim(strings.TrimSpace(p.Address)+", "+strings.TrimSpace(p.City), ", "),
				ValueMode: "linked", Include: true, Share: 1, BoughtOn: p.BoughtOn, Source: "rentals", SourceRef: ref, SourceURL: url, LinkedValue: &v, LinkedAt: store.Now()}
			if p.BoughtFor > 0 {
				b := p.BoughtFor
				a.BoughtPrice = &b
			}
			out = append(out, suggestion{Ref: ref, App: "rentals", Title: p.Name, Subtitle: rentalsSubtitle(p), Kind: "property", Value: &v, Currency: cur, URL: url,
				asset: a, homeRef: strings.TrimSpace(p.HomeRef)})
		}
		return out, nil
	case "inventory":
		sib, err := c.conn(ctx, appInventory, scopeItemsRead)
		if err != nil {
			return nil, err
		}
		total, n, err := inventoryTotal(ctx, sib.client, st)
		if err != nil {
			return nil, err
		}
		url := strings.TrimRight(sib.public, "/") + "/"
		sub := fmt.Sprintf("%d things worth %.0f %s or more each, at what they cost", n, st.InventoryMin, st.Currency)
		return []suggestion{{Ref: "inventory:items", App: "inventory", Title: "Things in my inventory", Subtitle: sub, Kind: "valuables", Value: &total,
			Currency: st.Currency, URL: url,
			asset: store.Asset{Name: "Things in my inventory", Kind: "valuables", Currency: st.Currency, ValueMode: "linked", Include: true, Share: 1,
				Source: "inventory", SourceRef: "inventory:items", SourceURL: url, LinkedValue: &total, LinkedAt: store.Now(),
				Notes: "The items in Pocket Inventory with a price, at what they cost."}}}, nil
	}
	return nil, nil
}

// rentalsSubtitle says what a place let out is: "Let out · Ilica 10, Zagreb ·
// 2 of 3 let · rent 1,350 EUR a month".
func rentalsSubtitle(p pocket.Property) string {
	sub := "Let out"
	if where := strings.Trim(strings.TrimSpace(p.Address)+", "+strings.TrimSpace(p.City), ", "); where != "" {
		sub += " · " + where
	}
	if p.UnitCount > 1 {
		sub += fmt.Sprintf(" · %d of %d let", p.Let, p.UnitCount)
	} else if p.Let == 0 {
		sub += " · empty now"
	}
	if p.RentRoll > 0 {
		sub += " · rent " + money(p.RentRoll, p.Currency) + " a month"
	}
	return sub
}

// inventoryTotal adds up what the valuable items cost (Inventory keeps one
// currency per person; it is taken to be the same as here).
func inventoryTotal(ctx context.Context, c *pocket.Client, st store.Settings) (int64, int, error) {
	items, err := c.Items(ctx)
	if err != nil {
		return 0, 0, err
	}
	var sum float64
	n := 0
	for _, it := range items {
		if it.Archived || it.Price == nil || *it.Price < st.InventoryMin || *it.Price <= 0 {
			continue
		}
		q := it.Quantity
		if q <= 0 {
			q = 1
		}
		sum += *it.Price * q
		n++
	}
	return minorOf(math.Round(sum*100)/100, st.Currency), n, nil
}

func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	out, err := s.suggestionsFor(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "suggestions")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddSuggestion adds what another app knows as a linked asset.
func (s *Server) handleAddSuggestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Refs []string `json:"refs"`
	}
	if err := decode(r, &in); err != nil || len(in.Refs) == 0 {
		writeErr(w, http.StatusBadRequest, "say what to add")
		return
	}
	want := map[string]bool{}
	for _, ref := range in.Refs {
		want[ref] = true
	}
	st, _ := s.store.GetSettings(ctx, uid)
	c := s.clients(ctx, uid)
	added := []int64{}
	for _, app := range linkedApps {
		need := false
		for ref := range want {
			if strings.HasPrefix(ref, app.slug+":") {
				need = true
			}
		}
		if !need {
			continue
		}
		items, err := s.appSuggestions(ctx, c, st, app)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		for _, it := range items {
			if !want[it.Ref] {
				continue
			}
			if have, err := s.store.AssetBySource(ctx, uid, it.Ref); err == nil {
				added = append(added, have.ID)
				continue
			}
			a := it.asset
			if err := s.store.CreateAsset(ctx, uid, &a); err != nil {
				s.fail(w, err, "add")
				return
			}
			_ = s.store.Undismiss(ctx, uid, it.Ref)
			added = append(added, a.ID)
			s.publish(ctx, uid, "wealth.asset_saved", a.Name, map[string]any{"id": a.ID, "name": a.Name, "kind": a.Kind, "created": true, "source": a.Source})
		}
	}
	s.snapshotSoon(uid)
	writeJSON(w, http.StatusOK, map[string]any{"added": added})
}

func (s *Server) handleDismissSuggestion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ref     string `json:"ref"`
		Title   string `json:"title"`
		Restore bool   `json:"restore"`
	}
	if err := decode(r, &in); err != nil || in.Ref == "" {
		writeErr(w, http.StatusBadRequest, "say which one")
		return
	}
	ctx := r.Context()
	var err error
	if in.Restore {
		err = s.store.Undismiss(ctx, actorID(ctx), in.Ref)
	} else {
		err = s.store.Dismiss(ctx, actorID(ctx), in.Ref, in.Title)
	}
	if err != nil {
		s.fail(w, err, "dismiss")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refreshLinked reads the current values of a person's linked assets
// (only one app's with app set) and writes a valuation when one changed.
func (s *Server) refreshLinked(ctx context.Context, uid int64, only string) (int, error) {
	assets, err := s.store.ListAssets(ctx, uid, false)
	if err != nil {
		return 0, err
	}
	st, _ := s.store.GetSettings(ctx, uid)
	c := s.clients(ctx, uid)
	var accts map[int64]pocket.Account
	var vehicles map[int64]pocket.Vehicle
	var homes map[int64]pocket.Home
	var places map[int64]pocket.Property
	var errs = map[string]error{}
	changed := 0
	for i := range assets {
		a := &assets[i]
		parts := strings.Split(a.SourceRef, ":")
		if len(parts) < 2 || (only != "" && parts[0] != only) {
			continue
		}
		id, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		fail := func(e error) { _ = s.store.SetLinked(ctx, uid, a.ID, nil, plainErr(e), "") }
		switch parts[0] {
		case "money":
			if accts == nil && errs["money"] == nil {
				accts = map[int64]pocket.Account{}
				if sib, err := c.conn(ctx, appMoney, scopeAccountsRead); err != nil {
					errs["money"] = err
				} else if list, err := sib.client.Accounts(ctx); err != nil {
					errs["money"] = err
				} else {
					for _, x := range list {
						accts[x.ID] = x
					}
				}
			}
			if e := errs["money"]; e != nil {
				fail(e)
				continue
			}
			acc, ok := accts[id]
			if !ok {
				fail(fmt.Errorf("this account is no longer in Pocket Money (deleted or archived)"))
				continue
			}
			v := moneyValue(acc)
			if a.LinkedValue == nil || *a.LinkedValue != v || a.LinkedError != "" {
				changed++
			}
			_ = s.store.SetLinked(ctx, uid, a.ID, &v, "", "")
			if acc.InterestRate != a.Rate || (acc.Payment != 0 && abs64(acc.Payment) != a.Payment) {
				a.Rate = acc.InterestRate
				if acc.Payment != 0 {
					a.Payment = abs64(acc.Payment)
				}
				_ = s.store.UpdateAsset(ctx, uid, a)
			}
		case "inventory":
			sib, err := c.conn(ctx, appInventory, scopeItemsRead)
			if err != nil {
				fail(err)
				continue
			}
			total, _, err := inventoryTotal(ctx, sib.client, st)
			if err != nil {
				fail(err)
				continue
			}
			if a.LinkedValue == nil || *a.LinkedValue != total {
				changed++
			}
			_ = s.store.SetLinked(ctx, uid, a.ID, &total, "", "")
		case "garage":
			if vehicles == nil && errs["garage"] == nil {
				vehicles = map[int64]pocket.Vehicle{}
				if sib, err := c.conn(ctx, appGarage, scopeVehiclesRead); err != nil {
					errs["garage"] = err
				} else if list, err := sib.client.Vehicles(ctx); err != nil {
					errs["garage"] = err
				} else {
					for _, x := range list {
						vehicles[x.ID] = x
					}
				}
			}
			if e := errs["garage"]; e != nil {
				fail(e)
				continue
			}
			v, ok := vehicles[id]
			if !ok {
				fail(fmt.Errorf("this vehicle is no longer in Pocket Garage"))
				continue
			}
			dirty := false
			if v.PurchasePrice > 0 && (a.BoughtPrice == nil || *a.BoughtPrice != v.PurchasePrice) {
				p := v.PurchasePrice
				a.BoughtPrice, dirty = &p, true
			}
			if v.PurchaseDate != "" && v.PurchaseDate != a.BoughtOn {
				a.BoughtOn, dirty = v.PurchaseDate, true
			}
			if v.Status == "sold" && a.ClosedOn == "" {
				a.ClosedOn, dirty = firstNonEmpty(v.SoldDate, store.Today()), true
				if v.SoldPrice > 0 {
					_ = s.store.PutValuation(ctx, uid, &store.Valuation{AssetID: a.ID, Date: store.AddDays(a.ClosedOn, -1), Value: v.SoldPrice, Note: "sold", Source: "garage"})
				}
			}
			if dirty {
				changed++
				_ = s.store.UpdateAsset(ctx, uid, a)
			}
			_ = s.store.SetLinked(ctx, uid, a.ID, nil, "", "")
		case "home":
			if homes == nil && errs["home"] == nil {
				homes = map[int64]pocket.Home{}
				if sib, err := c.conn(ctx, appHome, scopeHomeRead); err != nil {
					errs["home"] = err
				} else if list, err := sib.client.Homes(ctx); err != nil {
					errs["home"] = err
				} else {
					for _, x := range list {
						homes[x.ID] = x
					}
				}
			}
			if e := errs["home"]; e != nil {
				fail(e)
				continue
			}
			if _, ok := homes[id]; !ok {
				fail(fmt.Errorf("this home is no longer in Pocket Home"))
				continue
			}
			_ = s.store.SetLinked(ctx, uid, a.ID, nil, "", "")
		case "rentals":
			if places == nil && errs["rentals"] == nil {
				places = map[int64]pocket.Property{}
				if sib, err := c.conn(ctx, appRentals, scopeRentalsRead); err != nil {
					errs["rentals"] = err
				} else if list, err := sib.client.Properties(ctx); err != nil {
					errs["rentals"] = err
				} else {
					for _, x := range list {
						places[x.ID] = x
					}
				}
			}
			if e := errs["rentals"]; e != nil {
				fail(e)
				continue
			}
			p, ok := places[id]
			if !ok || p.Archived {
				fail(fmt.Errorf("this place is no longer in Pocket Rentals (deleted or archived)"))
				continue
			}
			if p.Value <= 0 {
				fail(fmt.Errorf("it has no value for this place now"))
				continue
			}
			v := p.Value
			if a.LinkedValue == nil || *a.LinkedValue != v || a.LinkedError != "" {
				changed++
			}
			_ = s.store.SetLinked(ctx, uid, a.ID, &v, "", "")
			if p.BoughtFor > 0 && (a.BoughtPrice == nil || *a.BoughtPrice != p.BoughtFor) || (p.BoughtOn != "" && p.BoughtOn != a.BoughtOn) {
				if p.BoughtFor > 0 {
					b := p.BoughtFor
					a.BoughtPrice = &b
				}
				a.BoughtOn = firstNonEmpty(p.BoughtOn, a.BoughtOn)
				_ = s.store.UpdateAsset(ctx, uid, a)
			}
		}
	}
	if changed > 0 {
		s.snapshot(ctx, uid)
	}
	return changed, nil
}

// plainErr turns a connection error into one line.
func plainErr(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(err.Error(), "\n", 2)[0])
}

func (s *Server) handleRefreshLinked(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	n, err := s.refreshLinked(ctx, actorID(r.Context()), "")
	if err != nil {
		s.fail(w, err, "refresh")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": n})
}
