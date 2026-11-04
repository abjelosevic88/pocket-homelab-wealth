package store

import "strings"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Asset is something a person owns or owes. Value, ValueBase and the
// fields after them are derived on read (the server fills them in).
type Asset struct {
	ID          int64    `json:"id"`
	UserID      int64    `json:"-"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Currency    string   `json:"currency"`
	Institution string   `json:"institution"`
	Number      string   `json:"number"`
	Notes       string   `json:"notes"`
	ValueMode   string   `json:"value_mode"`
	Include     bool     `json:"include"`
	Share       float64  `json:"share"`
	Rate        float64  `json:"rate"`
	Payment     int64    `json:"payment"`
	Growth      *float64 `json:"growth"`
	MaturesOn   string   `json:"matures_on"`
	BoughtOn    string   `json:"bought_on"`
	BoughtPrice *int64   `json:"bought_price"`
	Invested    *int64   `json:"invested"`
	Source      string   `json:"source"`
	SourceRef   string   `json:"source_ref"`
	SourceURL   string   `json:"source_url"`
	LinkedValue *int64   `json:"linked_value"`
	LinkedAt    string   `json:"linked_at"`
	LinkedError string   `json:"linked_error"`
	ClosedOn    string   `json:"closed_on"`
	Sort        int      `json:"sort"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`

	Group     string     `json:"group"`
	Debt      bool       `json:"debt"`
	Liquid    bool       `json:"liquid"`
	LastValue *Valuation `json:"last_valuation,omitempty"` // the newest valuation, any source
	Positions int        `json:"positions"`                // how many holdings
}

// Closed reports whether the asset no longer counts on a day.
func (a *Asset) Closed(day string) bool { return a.ClosedOn != "" && a.ClosedOn <= day }

// GrowthOr is the expected change % a year: its own, else the kind's.
func (a *Asset) GrowthOr() float64 {
	if a.Growth != nil {
		return *a.Growth
	}
	if (a.Kind == "deposit" || a.Kind == "savings") && a.Rate > 0 {
		return a.Rate
	}
	return KindOf(a.Kind).Growth
}

// Valuation is what an asset was worth on a day (its own currency, all of it).
type Valuation struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	AssetID   int64  `json:"asset_id"`
	Date      string `json:"date"`
	Value     int64  `json:"value"`
	Note      string `json:"note"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
}

// Position is a holding inside an asset.
type Position struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"-"`
	AssetID    int64   `json:"asset_id"`
	Name       string  `json:"name"`
	Symbol     string  `json:"symbol"`
	Kind       string  `json:"kind"`
	Units      float64 `json:"units"`
	Cost       int64   `json:"cost"`
	Currency   string  `json:"currency"`
	Price      float64 `json:"price"`
	PriceOn    string  `json:"price_on"`
	PriceAuto  bool    `json:"price_auto"`
	PriceError string  `json:"price_error"`
	Notes      string  `json:"notes"`
	Sort       int     `json:"sort"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`

	AssetName string `json:"asset_name,omitempty"`
}

// Activity is a buy, a sell, a dividend, money put in or taken out.
type Activity struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"-"`
	AssetID    int64   `json:"asset_id"`
	PositionID *int64  `json:"position_id"`
	Date       string  `json:"date"`
	Kind       string  `json:"kind"`
	Units      float64 `json:"units"`
	Amount     int64   `json:"amount"`
	Currency   string  `json:"currency"`
	Note       string  `json:"note"`
	Source     string  `json:"source"`
	CreatedAt  string  `json:"created_at"`

	AssetName    string `json:"asset_name,omitempty"`
	PositionName string `json:"position_name,omitempty"`
}

// Attachment is a statement or contract kept with an asset.
type Attachment struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	AssetID    int64  `json:"asset_id"`
	DocumentID *int64 `json:"document_id"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	CreatedAt  string `json:"created_at"`
}

// Milestone is a net worth reached for the first time.
type Milestone struct {
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	ReachedOn string `json:"reached_on"`
}

// Conn is how a person reaches one sibling Pocket app: "core" (an app link
// through Pocket Core), "direct" (URL + token) or "off"; "" = not chosen yet.
type Conn struct {
	Mode      string `json:"mode"`
	URL       string `json:"url"`
	Token     string `json:"token"`
	PublicURL string `json:"public_url"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday
	Currency   string `json:"currency"`   // everything is added up in this one
	StaleDays  int    `json:"stale_days"` // a value typed in longer ago than this "needs an update"

	// Exchange rates and prices.
	Online bool               `json:"online"` // look up prices and exchange rates online
	Rates  map[string]float64 `json:"rates"`  // own rates: 1 unit of the currency = this many of Currency

	// Notifications: at most one message a day, at NotifyHour.
	NotifyMonthly    bool   `json:"notify_monthly"`    // on the 1st: how last month went
	NotifyStale      bool   `json:"notify_stale"`      // with it: the values that need an update
	NotifyMilestones bool   `json:"notify_milestones"` // a new net worth milestone
	NotifyMaturity   bool   `json:"notify_maturity"`   // a deposit matures or a fixed rate ends in 30 days
	NotifyHour       int    `json:"notify_hour"`
	NotifyURL        string `json:"notify_url"`
	NotifyFormat     string `json:"notify_format"` // apprise | ntfy | json

	// The plan: where net worth is heading.
	PlanSavingAuto bool    `json:"plan_saving_auto"` // monthly saving and spending from Pocket Money
	PlanSaving     int64   `json:"plan_saving"`      // minor units a month, when not from Money
	PlanSpending   int64   `json:"plan_spending"`    // minor units a month, when not from Money
	PlanReturn     float64 `json:"plan_return"`      // % a year; 0 = the mix of what you own
	PlanInflation  float64 `json:"plan_inflation"`   // % a year
	PlanWithdraw   float64 `json:"plan_withdraw"`    // safe withdrawal % a year (the 4% rule)
	PlanTarget     int64   `json:"plan_target"`      // minor units; 0 = financial independence (spending / withdrawal)
	PlanBirthYear  int     `json:"plan_birth_year"`  // to say how old you will be
	Live           bool    `json:"live"`             // refresh linked values as soon as they change in the other app

	// Pocket Inventory: what counts as a valuable.
	InventoryMin float64 `json:"inventory_min"` // items worth at least this much each (major units)

	// Connections to the other Pocket apps, by slug.
	Conns map[string]Conn `json:"conns"`

	// AI for reading statements: Pocket AI through Core, or an
	// OpenAI-compatible API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }
