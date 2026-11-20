package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Wealth as
// the target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "wealth:read", Name: "See your net worth", Description: "Read what you own and owe, net worth over time, investments, debts, the plan, and what matures when (deposits, fixed rates), for your briefing, your journal, Pocket Renewals and Pocket AI."},
	{Scope: "wealth:write", Name: "Update values", Description: "Add an asset, type in a new value or record a buy, sell, dividend or deposit. It may change or delete only what that app added itself."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "wealth.json", Kind: core.KindPocket, Title: "Everything you own and owe: assets and debts, every value over time, investments, activity and attached links"},
		{Path: "/api/v1/export.csv", File: "values.csv", Kind: core.KindCSV, Title: "Every value ever recorded, asset by asset, as a table"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json", MaxBytes: 32 << 20, Title: "Adds the assets, debts, values and investments from another box; what you already have is left alone"},
	},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(summary|overview|history|assets|positions|activity|debts|plan|maturing|kinds|search|day/\d{4}-\d{2}-\d{2})$`), "wealth:read"},
	{"GET", regexp.MustCompile(`^/api/v1/assets/\d+$`), "wealth:read"},
	{"POST", regexp.MustCompile(`^/api/v1/assets$`), "wealth:write"},
	{"PUT DELETE", regexp.MustCompile(`^/api/v1/assets/\d+$`), "wealth:write"},
	{"POST", regexp.MustCompile(`^/api/v1/assets/\d+/valuations$`), "wealth:write"},
	{"DELETE", regexp.MustCompile(`^/api/v1/valuations/\d+$`), "wealth:write"},
	{"POST", regexp.MustCompile(`^/api/v1/activity$`), "wealth:write"},
	{"DELETE", regexp.MustCompile(`^/api/v1/activity/\d+$`), "wealth:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" || in.Audience != "wealth" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}
