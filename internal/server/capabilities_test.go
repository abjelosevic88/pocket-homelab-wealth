package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/config"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// The three routes Pocket Search, Pocket Day and Pocket Hub call
// (capabilities.go): the shape of each answer, that it carries the app's
// own numbers, and that nobody sees another person's assets.

// fakeCapCore is a Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|write> from Pocket Hub
// and remembers the manifest Wealth registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

func newCapApp(t *testing.T) (*app, *fakeCapCore) {
	t.Helper()
	fc := &fakeCapCore{}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "wealth:read", "write": "wealth:write"}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			fc.mu.Lock()
			fc.manifest = m
			fc.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "wealth", "scope": scopes[parts[2]],
				"user": map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(fc.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = fc.URL, "key" }), fc
}

// capItems are the rows of a capability answer.
func capItems(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

// capItem finds the row with that id.
func capItem(t *testing.T, m map[string]any, id string) map[string]any {
	t.Helper()
	for _, it := range capItems(t, m) {
		if it["id"] == id {
			return it
		}
	}
	t.Fatalf("no %q in %v", id, m["items"])
	return nil
}

// metric is the value of the metric with that label ("" when there is none).
func metric(m map[string]any, label string) string {
	l, _ := m["metrics"].([]any)
	for _, x := range l {
		if x := x.(map[string]any); x["label"] == label {
			s, _ := x["value"].(string)
			return s
		}
	}
	return ""
}

// wealthFixture gives Alice savings, a broker account with a holding, a
// mortgage paid down today, a car loan paid off today and a painting left
// out of the totals: their first values in the month before this one.
type wealthFixture struct {
	before, today                                    string
	savings, broker, holding, mortgage, loan, hidden string
}

func aliceWealth(a *app) wealthFixture {
	a.t.Helper()
	f := wealthFixture{today: store.Today()}
	f.before = store.AddDays(lastMonthEnd(f.today), -10)
	f.savings = a.asset(a.alice, map[string]any{"name": "Rainy day savings", "kind": "savings", "institution": "Erste Bank", "notes": "for emergencies", "value": 1000000, "date": f.before})
	a.ok(a.alice, "POST", "/api/v1/assets/"+f.savings+"/valuations", map[string]any{"value": 1200000, "date": f.today})
	f.broker = a.asset(a.alice, map[string]any{"name": "Broker", "kind": "investment"})
	f.holding = id(a.ok(a.alice, "POST", "/api/v1/positions", map[string]any{"asset_id": mustNum(f.broker), "name": "World fund", "symbol": "VWCE", "units": 10, "price": 100}))
	a.ok(a.alice, "POST", "/api/v1/activity", map[string]any{"asset_id": mustNum(f.broker), "position_id": mustNum(f.holding), "kind": "buy", "units": 1, "amount": 10000, "note": "monthly buy"})
	f.mortgage = a.asset(a.alice, map[string]any{"name": "Flat mortgage", "kind": "mortgage", "value": 5000000, "date": f.before})
	a.ok(a.alice, "POST", "/api/v1/assets/"+f.mortgage+"/valuations", map[string]any{"value": 4900000, "date": f.today})
	f.loan = a.asset(a.alice, map[string]any{"name": "Car loan", "kind": "loan", "value": 300000, "date": f.before})
	a.ok(a.alice, "PUT", "/api/v1/assets/"+f.loan, map[string]any{"closed_on": f.today})
	f.hidden = a.asset(a.alice, map[string]any{"name": "Painting", "kind": "valuables", "value": 9900000, "date": f.before, "include": false})
	return f
}

func TestPocketSearch(t *testing.T) {
	a := newApp(t, nil)
	f := aliceWealth(a)
	find := func(tok, q string) []map[string]any {
		t.Helper()
		return capItems(t, a.ok(tok, "GET", "/api/v1/pocket/search?q="+q, nil))
	}
	long := longDay(f.today)

	// an asset by its name, its bank or its notes, with what it is worth now
	for _, q := range []string{"rainy", "erste", "emergencies", "savings+account"} {
		got := find(a.alice, q)
		if len(got) != 1 || got[0]["id"] != "asset:"+f.savings || got[0]["title"] != "Rainy day savings" || got[0]["kind"] != "asset" ||
			got[0]["snippet"] != "12,000 EUR · Savings account · Erste Bank · last "+long || got[0]["url"] != "/a/"+f.savings || got[0]["date"] != f.today {
			t.Fatalf("%s: %v", q, got)
		}
	}
	// a debt says what is owed; one paid off says when
	if got := find(a.alice, "mortgage"); len(got) != 1 || got[0]["id"] != "asset:"+f.mortgage || got[0]["kind"] != "debt" ||
		got[0]["snippet"] != "49,000 EUR owed · Mortgage · last "+long || got[0]["url"] != "/a/"+f.mortgage {
		t.Fatalf("mortgage: %v", got)
	}
	if got := find(a.alice, "car+loan"); len(got) != 1 || got[0]["kind"] != "debt" || got[0]["snippet"] != "Loan · paid off "+long {
		t.Fatalf("car loan: %v", got)
	}
	// a holding by name or symbol, opened on its account
	for _, q := range []string{"world", "vwce"} {
		if got := find(a.alice, q); len(got) != 1 || got[0]["id"] != "holding:"+f.holding || got[0]["kind"] != "holding" || got[0]["title"] != "World fund" ||
			!strings.HasPrefix(got[0]["snippet"].(string), "1,100 EUR · in Broker") || got[0]["url"] != "/a/"+f.broker {
			t.Fatalf("%s: %v", q, got)
		}
	}
	// the asset before the holding inside it; the limit is kept
	if got := find(a.alice, "broker"); len(got) != 1 || got[0]["id"] != "asset:"+f.broker || got[0]["snippet"] != "1,100 EUR · Investments · last "+long {
		t.Fatalf("broker: %v", got)
	}
	if got := find(a.alice, "eur&limit=2"); len(got) != 2 {
		t.Fatalf("limit 2: %v", got)
	}
	// no words, or nothing that is a word
	for _, q := range []string{"", "%3F%3F", "*", "+"} {
		if got := find(a.alice, q); len(got) != 0 {
			t.Fatalf("q=%q: %v", q, got)
		}
	}
	// another person's assets and holdings never appear
	for _, q := range []string{"rainy", "mortgage", "world", "eur"} {
		if got := find(a.bob, q); len(got) != 0 {
			t.Fatalf("bob finds alice's %q: %v", q, got)
		}
	}
	a.expect(401, "", "GET", "/api/v1/pocket/search?q=rainy", nil)
	// the old route answers as before
	old := a.ok(a.alice, "GET", "/api/v1/search?q=world", nil)
	if l := old["results"].([]any); len(l) != 1 || l[0].(map[string]any)["type"] != "position" || l[0].(map[string]any)["url"] != "http://wealth.test/a/"+f.broker {
		t.Fatalf("/search changed: %v", old)
	}
}

func TestPocketDay(t *testing.T) {
	a := newApp(t, nil)
	f := aliceWealth(a)
	ov := a.ok(a.alice, "GET", "/api/v1/overview", nil)
	day := func(tok, date string) map[string]any {
		t.Helper()
		return a.ok(tok, "GET", "/api/v1/pocket/day/"+date, nil)
	}

	// today: the buy, the new value, the mortgage paid down, the car loan paid off
	d := day(a.alice, f.today)
	if d["date"] != f.today || len(capItems(t, d)) != 4 {
		t.Fatalf("today: %v", d)
	}
	if it := capItems(t, d)[0]; it["title"] != "Bought 1 World fund" || it["sub"] != "Broker" || it["kind"] != "buy" || num(it, "amount") != 10000 ||
		it["currency"] != "EUR" || it["url"] != "/a/"+f.broker || it["note"] != "monthly buy" || it["time"] != nil {
		t.Fatalf("the buy: %v", it)
	}
	var valueID int64
	_ = a.st.DB.QueryRow(`SELECT id FROM valuations WHERE asset_id = ? AND date = ?`, f.savings, f.today).Scan(&valueID)
	if it := capItem(t, d, "value:"+itoa(valueID)); it["title"] != "Rainy day savings" || it["kind"] != "value" || it["sub"] != "new value 12,000 EUR (+2,000 EUR)" ||
		num(it, "amount") != 1200000 || it["url"] != "/a/"+f.savings {
		t.Fatalf("the new value: %v", it)
	}
	var paid map[string]any
	for _, it := range capItems(t, d) {
		if it["title"] == "Flat mortgage" {
			paid = it
		}
	}
	if paid == nil || paid["kind"] != "paid" || paid["sub"] != "paid down 1,000 EUR, 49,000 EUR left" || num(paid, "amount") != 4900000 {
		t.Fatalf("the mortgage: %v", paid)
	}
	if it := capItem(t, d, "closed:"+f.loan); it["title"] != "Car loan" || it["kind"] != "paid off" || it["url"] != "/a/"+f.loan {
		t.Fatalf("the car loan: %v", it)
	}
	// net worth that day is the Overview's, with the month's change
	net := money(int64(num(ov, "net")), "EUR")
	month := ov["month"].(map[string]any)
	want := fmt.Sprintf("Net worth %s (%+.1f%% this month), 4 changes, paid off Car loan", net, month["pct"].(float64))
	if d["summary"] != want || metric(d, "Net worth") != net || metric(d, "This month") == "" {
		t.Fatalf("today's summary: %q %v, want %q", d["summary"], d["metrics"], want)
	}

	// the day the first values went in
	d = day(a.alice, f.before)
	if len(capItems(t, d)) != 4 || !strings.HasPrefix(d["summary"].(string), "Net worth ") || !strings.HasSuffix(d["summary"].(string), " this month), 4 changes") {
		t.Fatalf("the first day: %v", d)
	}
	// a day with only Wealth's own hourly value (prices moved): not news, nothing at all
	snap := store.AddDays(f.before, 2)
	if err := a.st.PutValuation(context.Background(), a.ids["Alice"], &store.Valuation{AssetID: int64(mustNum(f.broker)), Date: snap, Value: 50000, Source: "auto"}); err != nil {
		t.Fatal(err)
	}
	if d = day(a.alice, snap); len(capItems(t, d)) != 0 || d["summary"] != nil || d["metrics"] != nil {
		t.Fatalf("the snapshot day: %v", d)
	}
	// nothing written down: nothing; nor tomorrow
	for _, date := range []string{store.AddDays(f.before, -1), store.AddDays(f.before, 1), store.AddDays(f.today, 1)} {
		if d = day(a.alice, date); len(capItems(t, d)) != 0 || d["summary"] != nil || d["metrics"] != nil {
			t.Fatalf("%s: %v", date, d)
		}
	}
	a.expect(400, a.alice, "GET", "/api/v1/pocket/day/2026-13-01", nil)
	// another person's day is theirs
	if d = day(a.bob, f.today); len(capItems(t, d)) != 0 || d["summary"] != nil {
		t.Fatalf("bob's day: %v", d)
	}
	car := a.asset(a.bob, map[string]any{"name": "Bob's car", "kind": "vehicle", "value": 800000, "value_mode": "manual"})
	if d = day(a.bob, f.today); len(capItems(t, d)) != 1 || capItems(t, d)[0]["url"] != "/a/"+car || !strings.HasPrefix(d["summary"].(string), "Net worth 8,000 EUR") {
		t.Fatalf("bob's day with his car: %v", d)
	}
	// the old route answers as before
	if old := a.ok(a.alice, "GET", "/api/v1/day/"+f.today, nil); len(old["items"].([]any)) != 3 {
		t.Fatalf("/day changed: %v", old)
	}
}

func TestPocketWidget(t *testing.T) {
	a := newApp(t, nil)
	// nothing yet: nothing to show
	if w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil); len(capItems(t, w)) != 0 || w["line"] != nil || w["metrics"] != nil {
		t.Fatalf("empty widget: %v", w)
	}
	f := aliceWealth(a)
	ov := a.ok(a.alice, "GET", "/api/v1/overview", nil)
	w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil)
	net := money(int64(num(ov, "net")), "EUR")
	month := ov["month"].(map[string]any)
	if want := fmt.Sprintf("Net worth %s (%+.1f%% this month)", net, month["pct"].(float64)); w["line"] != want || w["url"] != "/" {
		t.Fatalf("line: %q, want %q", w["line"], want)
	}
	if metric(w, "You own") != money(int64(num(ov, "assets")), "EUR") || metric(w, "You owe") != "49,000 EUR" || metric(w, "Net worth") != net ||
		metric(w, "This month") != fmt.Sprintf("%s (%+.1f%%)", signed(int64(num(month, "amount")), "EUR"), month["pct"].(float64)) {
		t.Fatalf("metrics: %v", w["metrics"])
	}
	// the biggest assets, then the debts; not what is paid off or left out of the totals
	rows := capItems(t, w)
	if len(rows) != 3 || rows[0]["id"] != f.savings || rows[0]["title"] != "Rainy day savings" || rows[0]["detail"] != "12,000 EUR" || rows[0]["url"] != "/a/"+f.savings ||
		rows[1]["id"] != f.broker || rows[1]["detail"] != "1,100 EUR" || rows[2]["id"] != f.mortgage || rows[2]["detail"] != "49,000 EUR owed" {
		t.Fatalf("rows: %v", rows)
	}
	// at most five rows, two of them debts when there are more assets
	for i := 0; i < 5; i++ {
		a.asset(a.alice, map[string]any{"name": fmt.Sprintf("Card %d", i), "kind": "credit_card", "value": 1000 * (i + 1)})
		a.asset(a.alice, map[string]any{"name": fmt.Sprintf("Box %d", i), "kind": "other", "value": 2000 * (i + 1)})
	}
	rows = capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil))
	if len(rows) != 5 || rows[0]["id"] != f.savings || rows[3]["id"] != f.mortgage || rows[4]["title"] != "Card 4" {
		t.Fatalf("five rows: %v", rows)
	}
	// Bob sees only his own
	if w := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil); len(capItems(t, w)) != 0 || w["line"] != nil {
		t.Fatalf("bob's widget: %v", w)
	}
	a.asset(a.bob, map[string]any{"name": "Bob's car", "kind": "vehicle", "value": 800000, "value_mode": "manual"})
	if w := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil); len(capItems(t, w)) != 1 || w["line"] != "Net worth 8,000 EUR (+8,000 EUR this month)" || metric(w, "You owe") != "0 EUR" {
		t.Fatalf("bob's widget with his car: %v", w)
	}
}

func TestPocketLinkTokens(t *testing.T) {
	a, _ := newCapApp(t)
	f := aliceWealth(a)
	for _, c := range []struct {
		tok, method, path string
		body              any
		want              int
	}{
		{"pcx_alice_search", "GET", "/api/v1/pocket/search?q=rainy", nil, 200},
		{"pcx_alice_day", "GET", "/api/v1/pocket/day/" + f.today, nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/auth/me", nil, 200},
		// another capability's route
		{"pcx_alice_search", "GET", "/api/v1/pocket/day/" + f.today, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/search?q=rainy", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/search?q=rainy", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/day/" + f.today, nil, 403},
		// there is no capture, whatever the scope
		{"pcx_alice_capture", "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000001", "kind": "asset", "text": "x"}, 404},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_capture", "POST", "/api/v1/assets", map[string]any{"name": "x", "kind": "cash"}, 403},
		// the rest of the app
		{"pcx_alice_widget", "GET", "/api/v1/overview", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/summary", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/assets", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/search?q=rainy", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/assets/" + f.savings, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/lookup?q=vwce", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/day/" + f.today, nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/activity", nil, 403},
		{"pcx_alice_widget", "POST", "/api/v1/prices/update", nil, 403},
		{"pcx_alice_widget", "POST", "/api/v1/linked/refresh", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/settings", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/export.json", nil, 403},
		// the app's own scopes do not open the new routes, and still open their own
		{"pcx_alice_read", "GET", "/api/v1/pocket/search?q=rainy", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/day/" + f.today, nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_write", "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000002", "kind": "asset", "text": "x"}, 404},
		{"pcx_alice_read", "GET", "/api/v1/search?q=rainy", nil, 200},
		{"pcx_alice_read", "GET", "/api/v1/day/" + f.today, nil, 200},
		{"pcx_nobody", "GET", "/api/v1/pocket/widget", nil, 401},
	} {
		if code, m, _ := a.call(c.tok, c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%v)", c.tok, c.method, c.path, code, c.want, m)
		}
	}
	// the token acts for its person only
	if got := capItems(t, a.ok("pcx_alice_search", "GET", "/api/v1/pocket/search?q=rainy", nil)); len(got) != 1 || got[0]["id"] != "asset:"+f.savings {
		t.Fatalf("alice through the link: %v", got)
	}
	if w := a.ok("pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil); len(capItems(t, w)) != 3 {
		t.Fatalf("alice's widget through the link: %v", w)
	}
	if got := capItems(t, a.ok("pcx_bob_search", "GET", "/api/v1/pocket/search?q=rainy", nil)); len(got) != 0 {
		t.Fatalf("bob through the link finds alice's savings: %v", got)
	}
	if w := a.ok("pcx_bob_widget", "GET", "/api/v1/pocket/widget", nil); len(capItems(t, w)) != 0 || w["line"] != nil {
		t.Fatalf("bob's widget through the link: %v", w)
	}
	if d := a.ok("pcx_bob_day", "GET", "/api/v1/pocket/day/"+f.today, nil); len(capItems(t, d)) != 0 || d["summary"] != nil {
		t.Fatalf("bob's day through the link: %v", d)
	}
	// reading changed nothing
	var n int
	_ = a.st.DB.QueryRow(`SELECT COUNT(1) FROM assets WHERE user_id = ?`, a.ids["Alice"]).Scan(&n)
	if n != 5 {
		t.Fatalf("alice has %d assets", n)
	}
}

func TestPocketLinkScopes(t *testing.T) {
	req := func(m, p string) *http.Request { return httptest.NewRequest(m, p, nil) }
	for _, c := range []struct {
		scope string
		r     *http.Request
		want  bool
	}{
		{core.ScopeSearch, req("GET", "/api/v1/pocket/search?q=x"), true},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/2026-01-02"), true},
		{core.ScopeWidget, req("GET", "/api/v1/pocket/widget"), true},
		{core.ScopeWidget, req("GET", "/api/v1/auth/me"), true},
		{core.ScopeCapture, req("POST", "/api/v1/pocket/capture"), false},
		{core.ScopeSearch, req("POST", "/api/v1/pocket/search"), false},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/today"), false},
		{core.ScopeDay, req("GET", "/api/v1/day/2026-01-02"), false},
		{core.ScopeWidget, req("GET", "/api/v1/overview"), false},
		{core.ScopeWidget, req("POST", "/api/v1/prices/update"), false},
		{"wealth:read", req("GET", "/api/v1/pocket/widget"), false},
		{"wealth:read", req("GET", "/api/v1/pocket/search"), false},
		{"wealth:read", req("GET", "/api/v1/pocket/day/2026-01-02"), false},
		{"wealth:write", req("POST", "/api/v1/pocket/capture"), false},
	} {
		if got := linkAllows([]string{c.scope}, c.r); got != c.want {
			t.Errorf("%s %s %s = %v, want %v", c.scope, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

// Wealth tells Core what it can do, so Search, Day and Hub find it on their own.
func TestManifestCapabilities(t *testing.T) {
	a, fc := newCapApp(t)
	if err := a.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc.mu.Lock()
	m := fc.manifest
	fc.mu.Unlock()
	if m["slug"] != "wealth" || m["category"] != "Money" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	if search == nil || day == nil || day["label"] != "Wealth" || day["one"] != "change" || day["many"] != "changes" || day["unit"] != "amount" ||
		widget == nil || widget["title"] != "Net worth" || m["capture"] != nil {
		t.Fatalf("capabilities: %v %v %v %v", search, day, widget, m["capture"])
	}
	if bangs := search["bangs"].([]any); len(bangs) != 2 || bangs[0] != "wealth" || bangs[1] != "networth" || len(search["kinds"].([]any)) != 3 {
		t.Fatalf("search: %v", search)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"wealth:read", "wealth:write", core.ScopeMoveExport, core.ScopeMoveImport, core.ScopeSearch, core.ScopeDay, core.ScopeWidget} {
		if !offered[sc] {
			t.Errorf("%s is not offered", sc)
		}
	}
	if offered[core.ScopeCapture] {
		t.Errorf("capture is offered")
	}
}
