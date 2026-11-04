package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// ---- reading a statement with AI ----

// statementRead is what the AI found in a statement; nothing is saved until
// the person confirms it in the app.
type statementRead struct {
	Date      string   `json:"date"`
	Value     *float64 `json:"value"` // major units
	Currency  string   `json:"currency"`
	Positions []struct {
		Name     string   `json:"name"`
		Symbol   string   `json:"symbol"`
		Units    float64  `json:"units"`
		Price    *float64 `json:"price"`
		Value    *float64 `json:"value"`
		Currency string   `json:"currency"`
	} `json:"positions"`
	Note string `json:"note"`
}

const statementPrompt = `You read a bank, broker or pension statement and say what the account is worth.
Answer with JSON only: {"date":"YYYY-MM-DD","value":12345.67,"currency":"EUR","positions":[{"name":"...","symbol":"","units":1.5,"price":100.0,"value":150.0,"currency":"EUR"}],"note":"one short sentence"}.
"date" is the statement's date (the balance date), "value" the total balance or portfolio value as a plain number (no thousands separators) and for a loan what is still owed.
List "positions" only for holdings (funds, shares, ETFs, bonds, coins) with how many units; leave it empty otherwise. Use "" or null for anything the text does not say. Never guess a number.`

// handleReadStatement reads a statement (pasted text or a document in
// Pocket Documents) for one asset and returns what it found.
func (s *Server) handleReadStatement(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	uid := actorID(r.Context())
	a, err := s.store.GetAsset(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "asset")
		return
	}
	var in struct {
		Text       string `json:"text"`
		DocumentID int64  `json:"document_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	text := strings.TrimSpace(in.Text)
	title := ""
	if in.DocumentID > 0 {
		conn, err := s.clients(ctx, uid).conn(ctx, appDocuments, scopeDocumentsRead)
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		if d, err := conn.client.GetDocument(ctx, in.DocumentID); err == nil {
			title = d.Title
		}
		if text, err = conn.client.DocumentText(ctx, in.DocumentID); err != nil {
			writeErr(w, http.StatusBadGateway, "Pocket Documents: "+err.Error())
			return
		}
	}
	if len(text) < 20 {
		writeErr(w, http.StatusBadRequest, "there is no text to read (paste the statement, or pick a document that has text)")
		return
	}
	if len(text) > 40000 {
		text = text[:40000]
	}
	st, _ := s.store.GetSettings(ctx, uid)
	c, _, why := s.aiFor(ctx, uid, st)
	if c == nil {
		writeErr(w, http.StatusConflict, "AI is not set up: "+why)
		return
	}
	var out statementRead
	prompt := fmt.Sprintf("The account: %q (%s, %s). Today is %s.\n\nThe statement%s:\n%s", a.Name, store.KindOf(a.Kind).Label, a.Currency, store.Today(),
		map[bool]string{true: " (" + title + ")", false: ""}[title != ""], text)
	if err := c.JSON(ctx, statementPrompt, prompt, &out); err != nil {
		writeErr(w, http.StatusBadGateway, "the AI could not read it: "+err.Error())
		return
	}
	if !validDate(out.Date) || out.Date > store.Today() {
		out.Date = ""
	}
	out.Currency = firstNonEmpty(cleanCurrency(out.Currency), a.Currency)
	writeJSON(w, http.StatusOK, out)
}

// ---- search, day, summary (also for the other apps and Pocket AI) ----

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	b, err := s.loadBook(ctx, actorID(ctx), true)
	if err != nil {
		s.fail(w, err, "search")
		return
	}
	out := []map[string]any{}
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": out})
		return
	}
	for i := range b.assets {
		a := &b.assets[i]
		if !matches(a, q) {
			continue
		}
		v := b.view(a)
		out = append(out, map[string]any{"type": "asset", "id": a.ID, "title": a.Name, "kind": a.Kind, "group": a.Group, "value": v.Mine, "currency": a.Currency,
			"value_base": v.ValueBase, "closed": a.Closed(b.today), "url": s.cfg.BaseURL + "/a/" + itoa(a.ID)})
	}
	ql := strings.ToLower(q)
	for _, list := range b.pos {
		for _, p := range list {
			if strings.Contains(strings.ToLower(p.Name+" "+p.Symbol), ql) {
				pv := b.positionView(p)
				out = append(out, map[string]any{"type": "position", "id": p.ID, "asset_id": p.AssetID, "title": p.Name, "symbol": p.Symbol, "units": p.Units,
					"value": pv.Value, "currency": p.Currency, "value_base": pv.ValueBase, "url": s.cfg.BaseURL + "/a/" + itoa(p.AssetID)})
			}
		}
	}
	if len(out) > 50 {
		out = out[:50]
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out, "currency": b.base})
}

// handleDay is what happened on one day: activity and values typed in (for Pocket Day).
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	day := chi.URLParam(r, "date")
	if !validDate(day) || day == "" {
		writeErr(w, http.StatusBadRequest, "the date is YYYY-MM-DD")
		return
	}
	acts, err := s.store.ListActivity(ctx, uid, store.ActivityFilter{From: day, To: day})
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	items := []map[string]any{}
	for _, x := range acts {
		items = append(items, map[string]any{"type": "activity", "id": x.ID, "kind": x.Kind, "title": activityTitle(x), "amount": x.Amount, "currency": x.Currency,
			"asset_id": x.AssetID, "asset": x.AssetName, "url": s.cfg.BaseURL + "/a/" + itoa(x.AssetID)})
	}
	vals, _ := s.store.Valuations(ctx, uid, 0)
	assets, _ := s.store.ListAssets(ctx, uid, true)
	names := map[int64]string{}
	curs := map[int64]string{}
	for _, a := range assets {
		names[a.ID], curs[a.ID] = a.Name, a.Currency
	}
	for _, v := range vals {
		if v.Date != day || v.Source == "auto" {
			continue
		}
		items = append(items, map[string]any{"type": "valuation", "id": v.ID, "title": names[v.AssetID] + " updated", "amount": v.Value, "currency": curs[v.AssetID],
			"asset_id": v.AssetID, "asset": names[v.AssetID], "url": s.cfg.BaseURL + "/a/" + itoa(v.AssetID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": day, "items": items})
}

// activityTitle says an activity in a few words: "Bought 3 VWCE", "Dividend from …".
func activityTitle(x store.Activity) string {
	what := firstNonEmpty(x.PositionName, x.AssetName)
	switch x.Kind {
	case "buy":
		return fmt.Sprintf("Bought %s %s", trimUnits(x.Units), what)
	case "sell":
		return fmt.Sprintf("Sold %s %s", trimUnits(x.Units), what)
	case "dividend":
		return "Dividend from " + what
	case "interest":
		return "Interest on " + what
	case "deposit":
		return "Put money into " + what
	case "withdrawal":
		return "Took money out of " + what
	case "fee":
		return "Fee on " + what
	}
	return what
}

func trimUnits(u float64) string {
	s := fmt.Sprintf("%.6f", u)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

// handleSummary is a short picture for other apps and Pocket AI: net worth,
// how it moved, the groups and what needs attention.
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out, err := s.overview(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "summary")
		return
	}
	stale := []string{}
	for _, v := range out["stale"].([]assetView) {
		stale = append(stale, v.Name)
	}
	maturing := []map[string]any{}
	for _, v := range out["maturing"].([]assetView) {
		maturing = append(maturing, map[string]any{"name": v.Name, "matures_on": v.MaturesOn, "days": *v.DaysToMature})
	}
	writeJSON(w, http.StatusOK, map[string]any{"currency": out["currency"], "net": out["net"], "assets": out["assets"], "debts": out["debts"], "liquid": out["liquid"],
		"month": out["month"], "year": out["year"], "groups": out["groups"], "needs_update": stale, "maturing": maturing, "url": s.cfg.BaseURL + "/"})
}

// handleMaturing lists what matures or ends within days (default 120), for Pocket Renewals.
func (s *Server) handleMaturing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	days := queryInt(r, "days")
	if days <= 0 {
		days = 3650
	}
	b, err := s.loadBook(ctx, actorID(ctx), false)
	if err != nil {
		s.fail(w, err, "maturing")
		return
	}
	out := []map[string]any{}
	for i := range b.assets {
		a := &b.assets[i]
		if a.MaturesOn == "" {
			continue
		}
		d := store.DaysBetween(b.today, a.MaturesOn)
		if d > days {
			continue
		}
		out = append(out, map[string]any{"id": a.ID, "name": a.Name, "kind": a.Kind, "kind_label": store.KindOf(a.Kind).Label, "debt": a.Debt,
			"institution": a.Institution, "number": a.Number, "matures_on": a.MaturesOn, "days": d, "url": s.cfg.BaseURL + "/a/" + itoa(a.ID),
			"updated_at": a.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
