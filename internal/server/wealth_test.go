package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/config"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string
	bob   string
	ids   map[string]int64
}

func newApp(t *testing.T, tweak func(*config.Config)) *app {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.BaseURL = "http://wealth.test"
	cfg.MaxBodyBytes = 64 << 20
	cfg.NotifyHour = 8
	if tweak != nil {
		tweak(cfg)
	}
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, ids: map[string]int64{}}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, a.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return res.StatusCode, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func (a *app) list(auth, path string) []any {
	a.t.Helper()
	code, m, l := a.call(auth, "GET", path, nil)
	if code != 200 {
		a.t.Fatalf("GET %s → %d %v", path, code, m)
	}
	return l
}

func (a *app) expect(want int, auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s → %d, want %d (%v)", method, path, code, want, m)
	}
	return m
}

func num(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }
func id(m map[string]any) string             { return itoa(int64(num(m, "id"))) }

// asset adds an asset and returns its id.
func (a *app) asset(auth string, body map[string]any) string {
	a.t.Helper()
	return id(a.ok(auth, "POST", "/api/v1/assets", body)["asset"].(map[string]any))
}

func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t, nil)
	acc := a.asset(a.alice, map[string]any{"name": "Broker", "kind": "investment"})
	pos := a.ok(a.alice, "POST", "/api/v1/positions", map[string]any{"asset_id": mustNum(acc), "name": "World fund", "units": 10, "price": 100})
	cash := a.asset(a.alice, map[string]any{"name": "Savings", "kind": "savings", "value": 500000})
	val := a.ok(a.alice, "POST", "/api/v1/assets/"+cash+"/valuations", map[string]any{"value": 600000})
	act := a.ok(a.alice, "POST", "/api/v1/activity", map[string]any{"asset_id": mustNum(acc), "position_id": num(pos, "id"), "kind": "buy", "units": 1, "amount": 10000})

	for _, p := range []string{"/api/v1/assets/" + acc, "/api/v1/assets/" + cash} {
		a.expect(404, a.bob, "GET", p, nil)
		a.expect(404, a.bob, "PUT", p, map[string]any{"name": "mine"})
		a.expect(404, a.bob, "DELETE", p, nil)
		a.expect(404, a.bob, "POST", p+"/valuations", map[string]any{"value": 1})
		a.expect(404, a.bob, "POST", p+"/attachments", map[string]any{"url": "https://x.test"})
	}
	a.expect(404, a.bob, "POST", "/api/v1/positions", map[string]any{"asset_id": mustNum(acc), "name": "x", "units": 1})
	a.expect(404, a.bob, "PUT", "/api/v1/positions/"+id(pos), map[string]any{"units": 99})
	a.expect(404, a.bob, "DELETE", "/api/v1/positions/"+id(pos), nil)
	a.expect(404, a.bob, "DELETE", "/api/v1/valuations/"+id(val), nil)
	a.expect(404, a.bob, "DELETE", "/api/v1/activity/"+id(act), nil)
	a.expect(404, a.bob, "POST", "/api/v1/activity", map[string]any{"asset_id": mustNum(acc), "kind": "deposit", "amount": 1})

	if n := len(a.ok(a.bob, "GET", "/api/v1/assets", nil)["assets"].([]any)); n != 0 {
		t.Fatalf("bob sees %d assets", n)
	}
	if n := len(a.ok(a.bob, "GET", "/api/v1/positions", nil)["positions"].([]any)); n != 0 {
		t.Fatalf("bob sees %d positions", n)
	}
	if n := len(a.list(a.bob, "/api/v1/activity")); n != 0 {
		t.Fatalf("bob sees %d activity rows", n)
	}
	if o := a.ok(a.bob, "GET", "/api/v1/overview", nil); num(o, "net") != 0 {
		t.Fatalf("bob's net worth: %v", o["net"])
	}
	// the admin does not see a member's content either
	b := a.asset(a.bob, map[string]any{"name": "Bob's car", "kind": "vehicle", "value": 100})
	a.expect(404, a.alice, "GET", "/api/v1/assets/"+b, nil)
	// the buy was applied to alice's holding only
	p := a.ok(a.alice, "GET", "/api/v1/assets/"+acc, nil)["positions"].([]any)[0].(map[string]any)
	if num(p, "units") != 11 {
		t.Fatalf("units after a buy: %v", p["units"])
	}
}

func mustNum(s string) float64 {
	var f float64
	_ = json.Unmarshal([]byte(s), &f)
	return f
}

func TestNetWorthSharesDebtsAndRates(t *testing.T) {
	a := newApp(t, nil)
	a.asset(a.alice, map[string]any{"name": "Current", "kind": "cash", "value": 100000})                                     // 1,000 EUR
	a.asset(a.alice, map[string]any{"name": "Flat", "kind": "property", "value": 20000000, "share": 0.5})                    // half of 200,000
	a.asset(a.alice, map[string]any{"name": "Mortgage", "kind": "mortgage", "value": 5000000, "rate": 4, "payment": 100000}) // owes 50,000
	a.asset(a.alice, map[string]any{"name": "Dollars", "kind": "savings", "currency": "USD", "value": 100000})               // 1,000 USD, no rate yet
	a.asset(a.alice, map[string]any{"name": "Marks", "kind": "cash", "currency": "BAM", "value": 195583})                    // pegged: 1,000 EUR
	a.asset(a.alice, map[string]any{"name": "Not counted", "kind": "cash", "value": 999900, "include": false})

	o := a.ok(a.alice, "GET", "/api/v1/overview", nil)
	if miss := o["missing_rates"].([]any); len(miss) != 1 || miss[0] != "USD" {
		t.Fatalf("missing rates: %v", miss)
	}
	if num(o, "net") != 100000+10000000-5000000+100000 {
		t.Fatalf("net without USD: %v", o["net"])
	}
	// an own rate: 1 USD = 0.9 EUR
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"rates": map[string]float64{"USD": 0.9}})
	o = a.ok(a.alice, "GET", "/api/v1/overview", nil)
	if num(o, "net") != 100000+10000000-5000000+100000+90000 || len(o["missing_rates"].([]any)) != 0 {
		t.Fatalf("net with the own rate: %v %v", o["net"], o["missing_rates"])
	}
	if num(o, "liquid") != 100000+90000+100000 {
		t.Fatalf("liquid: %v", o["liquid"])
	}
	d := a.ok(a.alice, "GET", "/api/v1/debts", nil)
	po := d["debts"].([]any)[0].(map[string]any)["payoff"].(map[string]any)
	if m := num(po, "months"); m < 55 || m > 56 {
		t.Fatalf("50,000 at 4%% paying 1,000 a month: %v months", m)
	}
}

func TestEstimateAndHistory(t *testing.T) {
	a := newApp(t, nil)
	year := store.AddDays(store.Today(), -365)
	car := a.asset(a.alice, map[string]any{"name": "Car", "kind": "vehicle", "bought_price": 2000000, "bought_on": year})
	v := a.ok(a.alice, "GET", "/api/v1/assets/"+car, nil)["asset"].(map[string]any)
	if got := num(v, "value"); math.Abs(got-1700000) > 2000 {
		t.Fatalf("a car a year on at -15%%: %v", got)
	}
	// a backfilled value changes the history of that month
	sav := a.asset(a.alice, map[string]any{"name": "Savings", "kind": "savings", "value": 300000})
	a.ok(a.alice, "POST", "/api/v1/assets/"+sav+"/valuations", map[string]any{"value": 100000, "date": store.AddDays(store.Today(), -100)})
	h := a.ok(a.alice, "GET", "/api/v1/history?months=12", nil)["points"].([]any)
	if len(h) < 4 {
		t.Fatalf("history points: %d", len(h))
	}
	last := h[len(h)-1].(map[string]any)
	if last["label"] != "Today" || math.Abs(num(last, "net")-2000000) > 2000 {
		t.Fatalf("today: %v", last)
	}
	a.expect(400, a.alice, "POST", "/api/v1/assets/"+sav+"/valuations", map[string]any{"value": 1, "date": store.AddDays(store.Today(), 30)})
}

func TestPositionsAndActivity(t *testing.T) {
	a := newApp(t, nil)
	acc := a.asset(a.alice, map[string]any{"name": "Broker", "kind": "investment"})
	pos := a.ok(a.alice, "POST", "/api/v1/positions", map[string]any{"asset_id": mustNum(acc), "name": "Fund", "units": 10, "price": 100, "cost": 80000})
	a.ok(a.alice, "POST", "/api/v1/activity", map[string]any{"asset_id": mustNum(acc), "position_id": num(pos, "id"), "kind": "sell", "units": 5, "amount": 60000})
	a.expect(400, a.alice, "POST", "/api/v1/activity", map[string]any{"asset_id": mustNum(acc), "position_id": num(pos, "id"), "kind": "sell", "units": 50, "amount": 1})
	d := a.ok(a.alice, "GET", "/api/v1/assets/"+acc, nil)
	p := d["positions"].([]any)[0].(map[string]any)
	if num(p, "units") != 5 || num(p, "cost") != 40000 || num(p, "value") != 50000 {
		t.Fatalf("after selling half: %v", p)
	}
	if num(d["asset"].(map[string]any), "value") != 50000 || num(d["asset"].(map[string]any), "gain") != 10000 {
		t.Fatalf("asset value and gain: %v", d["asset"])
	}
	// money put into a value typed in by hand raises it
	pen := a.asset(a.alice, map[string]any{"name": "Pension", "kind": "pension", "value": 100000})
	a.ok(a.alice, "POST", "/api/v1/activity", map[string]any{"asset_id": mustNum(pen), "kind": "deposit", "amount": 15000})
	pv := a.ok(a.alice, "GET", "/api/v1/assets/"+pen, nil)["asset"].(map[string]any)
	if num(pv, "value") != 115000 || num(pv, "invested") != 15000 {
		t.Fatalf("after a deposit: value %v invested %v", pv["value"], pv["invested"])
	}
	a.ok(a.alice, "POST", "/api/v1/activity", map[string]any{"asset_id": mustNum(acc), "position_id": num(pos, "id"), "kind": "dividend", "amount": 2500})
	if inc := num(a.ok(a.alice, "GET", "/api/v1/positions", nil), "income_year"); inc != 2500 {
		t.Fatalf("income: %v", inc)
	}
}

func TestPayoff(t *testing.T) {
	p := payoffOf(1000000, 0, 100000, "2026-01-15", 0)
	if p.Months != 10 || p.InterestLeft != 0 || p.PaidOffOn != "2026-11-15" {
		t.Fatalf("no interest: %+v", p)
	}
	if p := payoffOf(1000000, 24, 10000, "2026-01-15", 0); p.Months != -1 {
		t.Fatalf("a payment under the interest never ends: %+v", p)
	}
	a, b := payoffOf(5000000, 5, 100000, "2026-01-15", 0), payoffOf(5000000, 5, 100000, "2026-01-15", 50000)
	if b.Months >= a.Months || b.InterestLeft >= a.InterestLeft {
		t.Fatalf("paying extra: %+v vs %+v", a, b)
	}
}

func TestLinkScopes(t *testing.T) {
	req := func(method, path string) *http.Request { return httptest.NewRequest(method, path, nil) }
	read := []string{"wealth:read"}
	write := []string{"wealth:write"}
	for _, c := range []struct {
		scopes []string
		r      *http.Request
		want   bool
	}{
		{nil, req("GET", "/api/v1/auth/me"), true},
		{read, req("GET", "/api/v1/summary"), true},
		{read, req("GET", "/api/v1/overview"), true},
		{read, req("GET", "/api/v1/assets/4"), true},
		{read, req("GET", "/api/v1/maturing"), true},
		{read, req("GET", "/api/v1/day/2026-11-04"), true},
		{read, req("POST", "/api/v1/assets"), false},
		{write, req("POST", "/api/v1/assets"), true},
		{write, req("POST", "/api/v1/assets/3/valuations"), true},
		{write, req("POST", "/api/v1/activity"), true},
		{write, req("GET", "/api/v1/overview"), false},
		{append(read, write...), req("POST", "/api/v1/suggestions/add"), false},
		{append(read, write...), req("POST", "/api/v1/assets/3/read"), false},
		{append(read, write...), req("PUT", "/api/v1/settings"), false},
		{append(read, write...), req("GET", "/api/v1/export.json"), false},
		{append(read, write...), req("GET", "/api/v1/backup"), false},
		{append(read, write...), req("POST", "/api/v1/tokens"), false},
		{append(read, write...), req("GET", "/api/v1/links/documents"), false},
	} {
		if got := linkAllows(c.scopes, c.r); got != c.want {
			t.Errorf("%v %s %s = %v, want %v", c.scopes, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

func TestExportImportIdempotent(t *testing.T) {
	a := newApp(t, nil)
	acc := a.asset(a.alice, map[string]any{"name": "Broker", "kind": "investment"})
	pos := a.ok(a.alice, "POST", "/api/v1/positions", map[string]any{"asset_id": mustNum(acc), "name": "Fund", "units": 3, "price": 10})
	a.ok(a.alice, "POST", "/api/v1/activity", map[string]any{"asset_id": mustNum(acc), "position_id": num(pos, "id"), "kind": "dividend", "amount": 100})
	sav := a.asset(a.alice, map[string]any{"name": "Savings", "kind": "savings", "value": 1000})
	a.ok(a.alice, "POST", "/api/v1/assets/"+sav+"/valuations", map[string]any{"value": 2000, "date": store.AddDays(store.Today(), -40)})
	code, doc, _ := a.call(a.alice, "GET", "/api/v1/export.json", nil)
	if code != 200 || doc["app"] != "pocket-wealth" {
		t.Fatalf("export: %d %v", code, doc["app"])
	}
	first := a.ok(a.bob, "POST", "/api/v1/import", doc)
	if num(first, "assets") != 2 || num(first, "valuations") != 2 || num(first, "positions") != 1 || num(first, "activity") != 1 {
		t.Fatalf("first import: %v", first)
	}
	second := a.ok(a.bob, "POST", "/api/v1/import", doc)
	for _, k := range []string{"assets", "valuations", "positions", "activity"} {
		if num(second, k) != 0 {
			t.Fatalf("second import added %s: %v", k, second)
		}
	}
	if o := a.ok(a.bob, "GET", "/api/v1/overview", nil); num(o, "net") != 1000+3000 {
		t.Fatalf("bob's net after import: %v", o["net"])
	}
	a.expect(400, a.bob, "POST", "/api/v1/import", map[string]any{"app": "pocket-home"})
}

func TestSettingsPerPerson(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"currency": "BAM", "notify_url": "http://apprise/notify/alice", "money_mode": "direct", "money_token": "secret-token-1234",
		"title": "Our money", "plan_saving": 50000, "plan_birth_year": 1990})
	b := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if b["currency"] != "EUR" || b["notify_url"] != "" || b["money_token"] != "" || num(b, "plan_saving") != 0 {
		t.Fatalf("bob got alice's settings: %v", b)
	}
	if b["title"] != "Our money" {
		t.Fatalf("the admin's title is box-wide, bob sees %v", b["title"])
	}
	a.ok(a.bob, "PUT", "/api/v1/settings", map[string]any{"title": "Bob's"})
	al := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	if al["title"] != "Our money" || al["money_token"] != "secr••••1234" || al["currency"] != "BAM" || num(al, "plan_birth_year") != 1990 {
		t.Fatalf("alice after bob's change: %v", al)
	}
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"currency": "euro"})
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"plan_withdraw": 40})
}

func TestDemoSeeds(t *testing.T) {
	a := newApp(t, nil)
	if err := a.s.seedDemo(context.Background(), a.ids["Alice"]); err != nil {
		t.Fatal(err)
	}
	o := a.ok(a.alice, "GET", "/api/v1/overview", nil)
	if num(o, "net") < 100000*100 || len(o["groups"].([]any)) < 7 || len(o["stale"].([]any)) == 0 || len(o["maturing"].([]any)) == 0 {
		t.Fatalf("demo overview: net %v groups %d", o["net"], len(o["groups"].([]any)))
	}
	if h := a.ok(a.alice, "GET", "/api/v1/history?months=40", nil)["points"].([]any); len(h) < 30 {
		t.Fatalf("demo history: %d points", len(h))
	}
	if p := a.ok(a.alice, "GET", "/api/v1/plan", nil); p["reach_on"] == "" || num(p, "fi_number") == 0 {
		t.Fatalf("demo plan: %v", p)
	}
	n := len(a.ok(a.alice, "GET", "/api/v1/assets?closed=1", nil)["assets"].([]any))
	if err := a.s.seedDemo(context.Background(), a.ids["Alice"]); err != nil {
		t.Fatal(err)
	}
	if m := len(a.ok(a.alice, "GET", "/api/v1/assets?closed=1", nil)["assets"].([]any)); m != n {
		t.Fatalf("demo seeded twice: %d then %d", n, m)
	}
}

func TestMilestonesRecordedOnce(t *testing.T) {
	a := newApp(t, nil)
	a.asset(a.alice, map[string]any{"name": "Savings", "kind": "savings", "value": 3000000})
	ctx := context.Background()
	a.s.checkMilestones(ctx, a.ids["Alice"]) // the first time: quietly
	ms, _ := a.st.Milestones(ctx, a.ids["Alice"])
	if len(ms) != 2 { // 10k and 25k
		t.Fatalf("milestones: %v", ms)
	}
	if v, _, _ := a.st.CacheGet(ctx, "notify:queue:"+itoa(a.ids["Alice"])); v != "" {
		t.Fatalf("the first look announced: %q", v)
	}
	a.ok(a.alice, "POST", "/api/v1/assets", map[string]any{"name": "Fund", "kind": "pension", "value": 3000000})
	a.s.checkMilestones(ctx, a.ids["Alice"])
	a.s.checkMilestones(ctx, a.ids["Alice"])
	if ms, _ := a.st.Milestones(ctx, a.ids["Alice"]); len(ms) != 3 {
		t.Fatalf("after passing 50k: %v", ms)
	}
	if v, _, _ := a.st.CacheGet(ctx, "notify:queue:"+itoa(a.ids["Alice"])); v == "" {
		t.Fatal("passing 50k was not queued for the daily message")
	}
}

func TestStatementNeedsAI(t *testing.T) {
	a := newApp(t, nil)
	sav := a.asset(a.alice, map[string]any{"name": "Savings", "kind": "savings"})
	a.expect(409, a.alice, "POST", "/api/v1/assets/"+sav+"/read", map[string]any{"text": "Balance on 1 Nov 2026: 12,345.67 EUR, account 123"})
	a.expect(400, a.alice, "POST", "/api/v1/assets/"+sav+"/read", map[string]any{"text": "short"})
}
