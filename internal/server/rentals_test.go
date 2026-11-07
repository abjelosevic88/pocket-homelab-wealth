package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeRentals answers as one person's Pocket Rentals; its places can change.
type fakeRentals struct {
	mu    sync.Mutex
	props []map[string]any
	ts    *httptest.Server
}

func newFakeRentals(t *testing.T) *fakeRentals {
	fr := &fakeRentals{}
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { j(w, map[string]any{"status": "ok", "version": "0.1.0"}) })
	mux.HandleFunc("/api/v1/properties", func(w http.ResponseWriter, r *http.Request) {
		fr.mu.Lock()
		defer fr.mu.Unlock()
		out := []map[string]any{}
		if fr.props != nil {
			out = fr.props
		}
		j(w, out)
	})
	fr.ts = httptest.NewServer(mux)
	t.Cleanup(fr.ts.Close)
	return fr
}

func (fr *fakeRentals) set(p []map[string]any) {
	fr.mu.Lock()
	fr.props = p
	fr.mu.Unlock()
}

func rentalsFixture() []map[string]any {
	return []map[string]any{
		{"id": 1, "name": "Ilica 10", "kind": "building", "address": "Ilica 10", "city": "Zagreb", "currency": "EUR", "value": 42000000, "bought_on": "2019-05-01",
			"bought_for": 30000000, "home_ref": "", "unit_count": 3, "let": 2, "rent_roll": 135000, "owed": 0},
		{"id": 2, "name": "Sea house", "kind": "house", "address": "", "city": "Split", "currency": "EUR", "value": 25000000, "home_ref": "7", "unit_count": 1, "let": 0, "rent_roll": 0},
		{"id": 3, "name": "Garage box", "kind": "garage", "currency": "EUR", "value": 0, "unit_count": 1, "let": 1, "rent_roll": 8000}, // no value there
	}
}

func rentalsGroup(t *testing.T, a *app) map[string]any {
	t.Helper()
	for _, x := range a.list(a.alice, "/api/v1/suggestions") {
		if g := x.(map[string]any); g["app"] == "rentals" {
			return g
		}
	}
	t.Fatalf("no rentals in the suggestions")
	return nil
}

func TestRentalsPlacesAsLinkedProperty(t *testing.T) {
	a := newApp(t, nil)
	fr := newFakeRentals(t)
	fr.set(rentalsFixture())
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"rentals_mode": "direct", "rentals_url": fr.ts.URL, "rentals_token": "tok-rentals-123456"})

	g := rentalsGroup(t, a)
	items := g["items"].([]any)
	if g["connected"] != true || len(items) != 2 {
		t.Fatalf("rentals suggestions: %v", g)
	}
	s1 := items[0].(map[string]any)
	if s1["ref"] != "rentals:property:1" || s1["kind"] != "property" || s1["title"] != "Ilica 10" || num(s1, "value") != 42000000 || s1["currency"] != "EUR" ||
		s1["url"] != fr.ts.URL+"/properties/1" || s1["subtitle"] != "Let out · Ilica 10, Zagreb · 2 of 3 let · rent 1,350 EUR a month" {
		t.Fatalf("the first place: %v", s1)
	}
	if s2 := items[1].(map[string]any); s2["subtitle"] != "Let out · Split · empty now" {
		t.Fatalf("the second place: %v", s2)
	}

	// added: a linked property at the value in Rentals
	added := a.ok(a.alice, "POST", "/api/v1/suggestions/add", map[string]any{"refs": []string{"rentals:property:1"}})["added"].([]any)
	if len(added) != 1 {
		t.Fatalf("added: %v", added)
	}
	aid := itoa(int64(added[0].(float64)))
	as := a.ok(a.alice, "GET", "/api/v1/assets/"+aid, nil)["asset"].(map[string]any)
	if as["kind"] != "property" || as["value_mode"] != "linked" || as["source"] != "rentals" || as["source_ref"] != "rentals:property:1" ||
		as["source_url"] != fr.ts.URL+"/properties/1" || num(as, "linked_value") != 42000000 || num(as, "bought_price") != 30000000 || as["bought_on"] != "2019-05-01" {
		t.Fatalf("the asset: %v", as)
	}
	if o := a.ok(a.alice, "GET", "/api/v1/overview", nil); num(o, "net") != 42000000 {
		t.Fatalf("net worth with the place: %v", o["net"])
	}

	// a new value in Rentals is read again
	props := rentalsFixture()
	props[0]["value"] = 45000000
	fr.set(props)
	if n, err := a.s.refreshLinked(context.Background(), a.ids["Alice"], ""); err != nil || n != 1 {
		t.Fatalf("refresh: %d %v", n, err)
	}
	if o := a.ok(a.alice, "GET", "/api/v1/overview", nil); num(o, "net") != 45000000 {
		t.Fatalf("net worth after the new value: %v", o["net"])
	}

	// the value taken out there: the last one stays, with a note
	props[0]["value"] = 0
	fr.set(props)
	_, _ = a.s.refreshLinked(context.Background(), a.ids["Alice"], "")
	as = a.ok(a.alice, "GET", "/api/v1/assets/"+aid, nil)["asset"].(map[string]any)
	if num(as, "linked_value") != 45000000 || !strings.Contains(as["linked_error"].(string), "no value") {
		t.Fatalf("after the value went: %v", as)
	}
	// gone from Rentals
	fr.set(props[1:])
	_, _ = a.s.refreshLinked(context.Background(), a.ids["Alice"], "")
	as = a.ok(a.alice, "GET", "/api/v1/assets/"+aid, nil)["asset"].(map[string]any)
	if !strings.Contains(as["linked_error"].(string), "no longer in Pocket Rentals") {
		t.Fatalf("after the place went: %v", as)
	}

	// Bob has no Rentals connection and sees nothing of Alice's
	for _, x := range a.list(a.bob, "/api/v1/suggestions") {
		if g := x.(map[string]any); g["app"] == "rentals" && (g["connected"] == true || len(g["items"].([]any)) != 0) {
			t.Fatalf("bob's rentals: %v", g)
		}
	}
}

func TestRentalsPlaceAlsoAHome(t *testing.T) {
	a := newApp(t, nil)
	fr := newFakeRentals(t)
	fr.set(rentalsFixture())
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"rentals_mode": "direct", "rentals_url": fr.ts.URL, "rentals_token": "tok-rentals-123456"})
	// the Sea house is already here, from Pocket Home
	if _, err := a.s.store.DB.Exec(`INSERT INTO assets (user_id, name, kind, currency, value_mode, include, share, source, source_ref) VALUES (?, 'Sea house', 'property', 'EUR', 'manual', 1, 1, 'home', 'home:home:7')`, a.ids["Alice"]); err != nil {
		t.Fatal(err)
	}
	items := rentalsGroup(t, a)["items"].([]any)
	if s := items[1].(map[string]any); !strings.HasSuffix(s["subtitle"].(string), "already here from Pocket Home: add only one") {
		t.Fatalf("the place that is a home: %v", s)
	}
	if s := items[0].(map[string]any); strings.Contains(s["subtitle"].(string), "Pocket Home") {
		t.Fatalf("the other place: %v", s)
	}
}

func TestRentalsLinkDeclared(t *testing.T) {
	app, ok := appBySlug("rentals")
	if !ok || len(app.scopes) != 1 || app.scopes[0] != scopeRentalsRead || app.reason == "" {
		t.Fatalf("rentals app: %+v", app)
	}
	for _, u := range linkUses() {
		if u.App == "rentals" {
			return
		}
	}
	t.Fatalf("rentals is not in the manifest's Uses")
}
