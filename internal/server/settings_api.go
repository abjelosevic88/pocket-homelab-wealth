package server

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart,
		"currency": st.Currency, "stale_days": st.StaleDays, "online": st.Online, "online_allowed": !s.cfg.DemoData, "rates": st.Rates,
		"notify_monthly": st.NotifyMonthly, "notify_stale": st.NotifyStale, "notify_milestones": st.NotifyMilestones, "notify_maturity": st.NotifyMaturity,
		"notify_hour": st.NotifyHour, "notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"plan_saving_auto": st.PlanSavingAuto, "plan_saving": st.PlanSaving, "plan_spending": st.PlanSpending, "plan_return": st.PlanReturn,
		"plan_inflation": st.PlanInflation, "plan_withdraw": st.PlanWithdraw, "plan_target": st.PlanTarget, "plan_birth_year": st.PlanBirthYear,
		"live": st.Live, "inventory_min": st.InventoryMin, "stream": s.streamStatus(),
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		_, url, token, public := connSettings(st, a)
		out[a.slug+"_url"], out[a.slug+"_token"], out[a.slug+"_public_url"] = url, mask(token), public
		if public == "" && via != "" {
			public = s.resolvePublic(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	raw := map[string]any{}
	var in store.Settings
	keys, err := decodeKeys(r, &raw)
	if err != nil || remarshal(raw, &in) != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	if keys["currency"] {
		if c := cleanCurrency(in.Currency); c != "" {
			cur.Currency = c
		} else {
			bad("currency is a three-letter code like EUR")
			return
		}
	}
	if keys["stale_days"] {
		if in.StaleDays < 7 || in.StaleDays > 730 {
			bad("a value is old after 7 to 730 days")
			return
		}
		cur.StaleDays = in.StaleDays
	}
	if keys["rates"] {
		rates := map[string]float64{}
		for k, v := range in.Rates {
			if c := cleanCurrency(k); c != "" && v > 0 && c != cur.Currency {
				rates[c] = v
			}
		}
		cur.Rates = rates
	}
	if keys["plan_inflation"] {
		if in.PlanInflation < -5 || in.PlanInflation > 50 {
			bad("inflation is -5 to 50% a year")
			return
		}
		cur.PlanInflation = in.PlanInflation
	}
	if keys["plan_return"] {
		if in.PlanReturn < -20 || in.PlanReturn > 50 {
			bad("the return is -20 to 50% a year (0 = the mix of what you own)")
			return
		}
		cur.PlanReturn = in.PlanReturn
	}
	if keys["plan_withdraw"] {
		if in.PlanWithdraw < 1 || in.PlanWithdraw > 10 {
			bad("the withdrawal rate is 1 to 10% a year")
			return
		}
		cur.PlanWithdraw = in.PlanWithdraw
	}
	if keys["plan_birth_year"] {
		if in.PlanBirthYear != 0 && (in.PlanBirthYear < 1900 || in.PlanBirthYear > time.Now().Year()) {
			bad("the year you were born, like 1985")
			return
		}
		cur.PlanBirthYear = in.PlanBirthYear
	}
	if keys["inventory_min"] {
		if in.InventoryMin < 0 {
			bad("the amount cannot be negative")
			return
		}
		cur.InventoryMin = in.InventoryMin
	}
	minor := func(k string, dst *int64, v int64) {
		if keys[k] && v >= 0 {
			*dst = v
		}
	}
	minor("plan_saving", &cur.PlanSaving, in.PlanSaving)
	minor("plan_spending", &cur.PlanSpending, in.PlanSpending)
	minor("plan_target", &cur.PlanTarget, in.PlanTarget)
	if keys["notify_hour"] {
		if in.NotifyHour < 0 || in.NotifyHour > 23 {
			bad("the hour is 0 to 23")
			return
		}
		cur.NotifyHour = in.NotifyHour
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	boolIn("notify_monthly", &cur.NotifyMonthly, in.NotifyMonthly)
	boolIn("notify_stale", &cur.NotifyStale, in.NotifyStale)
	boolIn("notify_milestones", &cur.NotifyMilestones, in.NotifyMilestones)
	boolIn("notify_maturity", &cur.NotifyMaturity, in.NotifyMaturity)
	onlineWas := cur.Online
	boolIn("online", &cur.Online, in.Online)
	boolIn("live", &cur.Live, in.Live)
	boolIn("plan_saving_auto", &cur.PlanSavingAuto, in.PlanSavingAuto)
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	// each Pocket app: mode, URL, token, public URL
	inMap := map[string]string{}
	for k, v := range raw {
		if sv, ok := v.(string); ok {
			inMap[k] = sv
		}
	}
	for _, a := range pocketApps {
		c := cur.Conns[a.slug]
		if v := inMap[a.slug+"_mode"]; keys[a.slug+"_mode"] && (v == modeCore || v == modeDirect || v == modeOff) {
			c.Mode = v
		}
		url(a.slug+"_url", &c.URL, inMap[a.slug+"_url"])
		secret(a.slug+"_token", &c.Token, inMap[a.slug+"_token"])
		url(a.slug+"_public_url", &c.PublicURL, inMap[a.slug+"_public_url"])
		if c != (store.Conn{}) {
			cur.Conns[a.slug] = c
		}
	}
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			bad("AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			bad("unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for k := range keys {
		if modeKey(k) {
			s.reportModesSoon()
			break
		}
	}
	if cur.Online && !onlineWas {
		s.pricesSoon(me.ID)
	}
	if keys["currency"] || keys["rates"] {
		s.snapshotSoon(me.ID)
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, uid).conn(ctx, a, a.scopes...)
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				msg = fmt.Sprintf("%s %s, %s", a.name, v, how)
				switch a.slug {
				case "money":
					var accts []pocket.Account
					if accts, err = conn.client.Accounts(ctx); err == nil {
						msg += fmt.Sprintf(", %d accounts", len(accts))
					}
				case "documents":
					var docs []pocket.Document
					if docs, err = conn.client.SearchDocuments(ctx, "", 1); err == nil && len(docs) == 0 {
						msg += ", no documents yet"
					}
				case "home":
					var hs []pocket.Home
					if hs, err = conn.client.Homes(ctx); err == nil {
						msg += fmt.Sprintf(", %d homes", len(hs))
					}
				case "garage":
					var vs []pocket.Vehicle
					if vs, err = conn.client.Vehicles(ctx); err == nil {
						msg += fmt.Sprintf(", %d vehicles", len(vs))
					}
				case "rentals":
					var props []pocket.Property
					if props, err = conn.client.Properties(ctx); err == nil {
						valued := 0
						for _, p := range props {
							if p.Value > 0 {
								valued++
							}
						}
						msg += fmt.Sprintf(", %d places, %d with a value", len(props), valued)
					}
				case "inventory":
					var items []pocket.Item
					if items, err = conn.client.Items(ctx); err == nil {
						msg += fmt.Sprintf(", %d items", len(items))
					}
				}
			}
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Wealth", Body: "Notifications work. Your monthly summary, milestones and maturing deposits will arrive here.", Priority: "info", Tags: []string{"moneybag"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// handleCheckNow runs the daily check for the caller now and says what it sent.
func (s *Server) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	body, sent := s.dailyCheck(ctx, uid, st, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "body": body})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-wealth-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one person's data.
type exportDoc struct {
	App         string             `json:"app"`
	Version     string             `json:"version"`
	Exported    string             `json:"exported_at"`
	Currency    string             `json:"currency"`
	Assets      []store.Asset      `json:"assets"`
	Valuations  []store.Valuation  `json:"valuations"`
	Positions   []store.Position   `json:"positions"`
	Activity    []store.Activity   `json:"activity"`
	Attachments []store.Attachment `json:"attachments"`
}

func (s *Server) export(ctx context.Context, uid int64) exportDoc {
	st, _ := s.store.GetSettings(ctx, uid)
	doc := exportDoc{App: "pocket-wealth", Version: Version, Exported: store.Now(), Currency: st.Currency}
	doc.Assets, _ = s.store.ListAssets(ctx, uid, true)
	doc.Valuations, _ = s.store.Valuations(ctx, uid, 0)
	doc.Positions, _ = s.store.Positions(ctx, uid, 0)
	doc.Activity, _ = s.store.ListActivity(ctx, uid, store.ActivityFilter{})
	doc.Attachments, _ = s.store.Attachments(ctx, uid, 0)
	return doc
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-wealth-export.json"`)
	writeJSON(w, http.StatusOK, s.export(r.Context(), actorID(r.Context())))
}

// handleExportCSV writes every value ever recorded: a spreadsheet of what
// each asset was worth when.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	b, err := s.loadBook(ctx, uid, true)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-wealth.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"asset", "kind", "group", "debt", "currency", "date", "value", "share", "your_part", "your_part_" + strings.ToLower(b.base), "source", "note"})
	for i := range b.assets {
		a := &b.assets[i]
		for _, v := range b.vals[a.ID] {
			part := mine(a, v.Value)
			_ = cw.Write([]string{a.Name, a.Kind, a.Group, strconv.FormatBool(a.Debt), a.Currency, v.Date, decimalString(v.Value, a.Currency),
				strconv.FormatFloat(shareOf(a), 'f', -1, 64), decimalString(part, a.Currency), decimalString(b.fx.toBase(part, a.Currency, v.Date), b.base), v.Source, v.Note})
		}
	}
	cw.Flush()
}

func decimalString(v int64, cur string) string {
	return strconv.FormatFloat(majorOf(v, cur), 'f', decimals(cur), 64)
}

// handleImportJSON merges an export into the caller's own data. Rows keep
// their identity (the export's ids, remembered per person), so importing
// the same file twice adds nothing. Links to other apps' items are kept
// only when the caller does not have that item linked already.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if doc.App != "" && doc.App != "pocket-wealth" {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Wealth export")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	counts := map[string]int{"assets": 0, "valuations": 0, "positions": 0, "activity": 0, "attachments": 0}
	idOf := func(kind string, old int64) (int64, bool) {
		v, _, ok := s.store.CacheGet(ctx, fmt.Sprintf("import:%d:%s:%d", uid, kind, old))
		if !ok {
			return 0, false
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		if n <= 0 {
			return 0, false
		}
		switch kind {
		case "asset":
			if _, err := s.store.GetAsset(ctx, uid, n); err != nil {
				return 0, false
			}
		case "position":
			if _, err := s.store.GetPosition(ctx, uid, n); err != nil {
				return 0, false
			}
		}
		return n, true
	}
	remember := func(kind string, old, id int64) {
		_ = s.store.CachePut(ctx, fmt.Sprintf("import:%d:%s:%d", uid, kind, old), strconv.FormatInt(id, 10))
	}
	for _, a := range doc.Assets {
		if _, ok := idOf("asset", a.ID); ok {
			continue
		}
		if a.SourceRef != "" {
			if have, err := s.store.AssetBySource(ctx, uid, a.SourceRef); err == nil {
				remember("asset", a.ID, have.ID)
				continue
			}
		}
		old := a.ID
		if !store.ValidKind(a.Kind) {
			a.Kind = "other"
		}
		a.Source = firstNonEmpty(a.Source, "import")
		if err := s.store.CreateAsset(ctx, uid, &a); err == nil {
			remember("asset", old, a.ID)
			counts["assets"]++
		}
	}
	for _, v := range doc.Valuations {
		if _, ok := idOf("valuation", v.ID); ok {
			continue
		}
		aid, ok := idOf("asset", v.AssetID)
		if !ok {
			continue
		}
		old := v.ID
		v.AssetID = aid
		if err := s.store.PutValuation(ctx, uid, &v); err == nil {
			remember("valuation", old, v.ID)
			counts["valuations"]++
		}
	}
	for _, p := range doc.Positions {
		if _, ok := idOf("position", p.ID); ok {
			continue
		}
		aid, ok := idOf("asset", p.AssetID)
		if !ok {
			continue
		}
		old := p.ID
		p.AssetID = aid
		if err := s.store.CreatePosition(ctx, uid, &p); err == nil {
			remember("position", old, p.ID)
			counts["positions"]++
		}
	}
	for _, x := range doc.Activity {
		if _, ok := idOf("activity", x.ID); ok {
			continue
		}
		aid, ok := idOf("asset", x.AssetID)
		if !ok {
			continue
		}
		old := x.ID
		x.AssetID = aid
		if x.PositionID != nil {
			if pid, ok := idOf("position", *x.PositionID); ok {
				x.PositionID = &pid
			} else {
				x.PositionID = nil
			}
		}
		if err := s.store.InsertActivity(ctx, uid, &x); err == nil {
			remember("activity", old, x.ID)
			counts["activity"]++
		}
	}
	for _, at := range doc.Attachments {
		if _, ok := idOf("attachment", at.ID); ok {
			continue
		}
		aid, ok := idOf("asset", at.AssetID)
		if !ok {
			continue
		}
		old := at.ID
		at.AssetID = aid
		if err := s.store.AddAttachment(ctx, uid, &at); err == nil {
			remember("attachment", old, at.ID)
			counts["attachments"]++
		}
	}
	s.snapshotSoon(uid)
	writeJSON(w, http.StatusOK, counts)
}
