package store

// Kind describes one kind of asset or debt: the group it is counted in on
// the Overview, whether it is a debt, whether the money is at hand (for the
// emergency fund), how its value usually comes in and how it is expected to
// change in a year (for the plan and the estimate).
type Kind struct {
	Value  string  `json:"value"`
	Label  string  `json:"label"`
	Group  string  `json:"group"`
	Debt   bool    `json:"debt"`
	Liquid bool    `json:"liquid"`
	Mode   string  `json:"mode"`   // the usual value mode for a new one
	Growth float64 `json:"growth"` // % a year
	Icon   string  `json:"icon"`
	Hint   string  `json:"hint"`
}

// Kinds is every kind, in the order the add form offers them.
var Kinds = []Kind{
	{"cash", "Current account or cash", "cash", false, true, "manual", 0, "wallet", "Everyday money: a current account, cash at home"},
	{"savings", "Savings account", "cash", false, true, "manual", 1.5, "piggy", "Money put aside you can reach at any time"},
	{"deposit", "Term deposit", "cash", false, false, "estimate", 3, "lock", "Fixed for a term at an interest rate; set the date it matures"},
	{"investment", "Investments", "investments", false, false, "positions", 6, "trending-up", "A broker account, funds, shares, bonds"},
	{"pension", "Pension", "pension", false, false, "manual", 5, "sunset", "A pension fund or retirement savings"},
	{"crypto", "Crypto", "investments", false, false, "positions", 0, "bitcoin", "Coins in a wallet or on an exchange"},
	{"property", "Home or property", "property", false, false, "manual", 2, "home", "A flat, a house, land"},
	{"vehicle", "Vehicle", "vehicles", false, false, "estimate", -15, "car", "A car or a motorbike; its value drops every year"},
	{"valuables", "Valuables", "valuables", false, false, "manual", 0, "gem", "Jewellery, art, collections, the things you own"},
	{"business", "Business", "other", false, false, "manual", 0, "briefcase", "A share in a company"},
	{"receivable", "Money owed to me", "other", false, false, "manual", 0, "hand-coins", "A loan you gave someone"},
	{"other", "Something else", "other", false, false, "manual", 0, "box", "Anything else of value"},
	{"mortgage", "Mortgage", "debts", true, false, "manual", 0, "home", "A loan on a home"},
	{"loan", "Loan", "debts", true, false, "manual", 0, "landmark", "A car loan, a personal loan, a student loan"},
	{"credit_card", "Credit card", "debts", true, false, "manual", 0, "card", "What you owe on a card"},
	{"debt", "Other debt", "debts", true, false, "manual", 0, "receipt", "Money you owe someone"},
}

// Groups are the Overview's breakdown, in order.
var Groups = []struct {
	Value, Label string
}{
	{"cash", "Cash and savings"}, {"investments", "Investments"}, {"pension", "Pension"}, {"property", "Property"},
	{"vehicles", "Vehicles"}, {"valuables", "Valuables"}, {"other", "Other"}, {"debts", "Debts"},
}

// KindOf returns a kind's description (Other for an unknown one).
func KindOf(v string) Kind {
	for _, k := range Kinds {
		if k.Value == v {
			return k
		}
	}
	return Kinds[11]
}

// ValidKind reports whether v is a kind.
func ValidKind(v string) bool {
	for _, k := range Kinds {
		if k.Value == v {
			return true
		}
	}
	return false
}

// ValueModes are how an asset's value comes in.
var ValueModes = []string{"manual", "positions", "linked", "estimate"}

// PositionKinds are the kinds of holding.
var PositionKinds = []string{"stock", "etf", "fund", "bond", "crypto", "cash", "other"}

// ActivityKinds are what can happen to an asset.
var ActivityKinds = []string{"buy", "sell", "dividend", "interest", "deposit", "withdrawal", "fee"}

// In reports whether v is one of list.
func In(v string, list []string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Slow reports whether a kind's value changes slowly (a home, a car,
// valuables): it needs an update once a year, not every few months.
func Slow(kind string) bool {
	return In(kind, []string{"property", "vehicle", "valuables", "business", "receivable", "other"})
}
