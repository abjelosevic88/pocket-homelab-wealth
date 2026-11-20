package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/config"
)

// moveLinks starts Wealth behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@ (both plain members
// as far as Core is concerned).
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "wealth:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"wealth","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) raw(tok, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/assets", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{exp, "GET", "/api/v1/users", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/assets", 403},
		{imp, "POST", "/api/v1/activity", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
		{read, "POST", "/api/v1/import", 403},
	} {
		if code, body := a.raw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.raw(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || len(p.Imports) != 1 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's wealth to Bob: the import adds once, and nothing of
// one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	acc := a.asset(a.alice, map[string]any{"name": "Broker", "kind": "investment"})
	a.ok(a.alice, "POST", "/api/v1/positions", map[string]any{"asset_id": mustNum(acc), "name": "World fund", "units": 10, "price": 100})
	cash := a.asset(a.alice, map[string]any{"name": "Rainy day savings", "kind": "savings", "value": 500000})
	a.ok(a.alice, "POST", "/api/v1/assets/"+cash+"/valuations", map[string]any{"value": 600000, "date": "2026-01-05"})

	code, export := a.raw("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(export), "Rainy day savings") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, csv := a.raw("pcx_alice_export", "GET", "/api/v1/export.csv", "", nil); !strings.Contains(string(csv), "Rainy day savings") {
		t.Fatalf("alice's csv: %s", csv)
	}
	if _, bob := a.raw("pcx_bob_export", "GET", "/api/v1/export.json", "", nil); strings.Contains(string(bob), "Rainy day savings") {
		t.Fatalf("bob's export holds alice's assets: %s", bob)
	}

	var first, second map[string]any
	code, out := a.raw("pcx_bob_import", "POST", "/api/v1/import", "application/json", export)
	if code != 200 || json.Unmarshal(out, &first) != nil || num(first, "assets") != 2 || num(first, "positions") != 1 || num(first, "valuations") < 1 {
		t.Fatalf("first import: %d %s", code, out)
	}
	code, out = a.raw("pcx_bob_import", "POST", "/api/v1/import", "application/json", export)
	_ = json.Unmarshal(out, &second)
	if code != 200 || num(second, "assets")+num(second, "valuations")+num(second, "positions")+num(second, "activity")+num(second, "attachments") != 0 {
		t.Fatalf("second import added: %d %s", code, out)
	}

	if l, _ := a.ok(a.bob, "GET", "/api/v1/assets", nil)["assets"].([]any); len(l) != 2 {
		t.Fatalf("bob's assets: %v", l)
	}
	if l, _ := a.ok(a.alice, "GET", "/api/v1/assets", nil)["assets"].([]any); len(l) != 2 {
		t.Fatalf("alice's assets changed: %v", l)
	}
}
