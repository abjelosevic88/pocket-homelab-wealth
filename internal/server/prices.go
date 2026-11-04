package server

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// ---- prices and exchange rates from the internet ----
//
// Exchange rates come from the European Central Bank's daily reference
// rates (no key; the last 90 days on the first start, then each day).
// Currencies the ECB does not publish, and the prices of holdings with a
// symbol, come from Yahoo Finance's public chart endpoint. Both only when
// the person has "Look up prices and exchange rates online" on, and never
// with demo data.

var httpClient = &http.Client{Timeout: 20 * time.Second}

// Endpoints (variables so tests can point them elsewhere).
var (
	ecbDailyURL = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"
	ecb90URL    = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-hist-90d.xml"
	yahooURL    = "https://query1.finance.yahoo.com/v8/finance/chart/"
)

func (s *Server) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	req.Header.Set("Accept", "application/json, application/xml;q=0.9, */*;q=0.5")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, errors.New("not found")
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return b, nil
}

// ecbRates parses the ECB's eurofxref XML.
func ecbRates(b []byte) ([]store.Rate, error) {
	var doc struct {
		Cube struct {
			Days []struct {
				Time  string `xml:"time,attr"`
				Rates []struct {
					Currency string  `xml:"currency,attr"`
					Rate     float64 `xml:"rate,attr"`
				} `xml:"Cube"`
			} `xml:"Cube"`
		} `xml:"Cube"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	out := []store.Rate{}
	for _, d := range doc.Cube.Days {
		for _, r := range d.Rates {
			out = append(out, store.Rate{Currency: r.Currency, Date: d.Time, PerEUR: r.Rate, Source: "ecb"})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no rates in the answer")
	}
	return out, nil
}

// quote is a price from Yahoo Finance.
type quote struct {
	Symbol   string  `json:"symbol"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
	Date     string  `json:"date"`
	Exchange string  `json:"exchange"`
}

// lookup asks Yahoo Finance for a symbol's latest price.
func (s *Server) lookup(ctx context.Context, symbol string) (quote, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return quote{}, errors.New("no symbol")
	}
	b, err := s.get(ctx, yahooURL+url.PathEscape(symbol)+"?range=5d&interval=1d")
	if err != nil {
		if err.Error() == "not found" {
			return quote{}, fmt.Errorf("%s was not found on Yahoo Finance", symbol)
		}
		return quote{}, err
	}
	var doc struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Symbol    string  `json:"symbol"`
					Currency  string  `json:"currency"`
					Price     float64 `json:"regularMarketPrice"`
					Time      int64   `json:"regularMarketTime"`
					LongName  string  `json:"longName"`
					ShortName string  `json:"shortName"`
					Exchange  string  `json:"fullExchangeName"`
				} `json:"meta"`
			} `json:"result"`
			Error *struct {
				Description string `json:"description"`
			} `json:"error"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return quote{}, err
	}
	if doc.Chart.Error != nil {
		return quote{}, errors.New(doc.Chart.Error.Description)
	}
	if len(doc.Chart.Result) == 0 || doc.Chart.Result[0].Meta.Price <= 0 {
		return quote{}, fmt.Errorf("no price for %s", symbol)
	}
	m := doc.Chart.Result[0].Meta
	q := quote{Symbol: m.Symbol, Name: firstNonEmpty(m.LongName, m.ShortName), Price: m.Price, Currency: m.Currency, Exchange: m.Exchange,
		Date: time.Unix(m.Time, 0).Format("2006-01-02")}
	// London quotes in pence
	switch q.Currency {
	case "GBp", "GBX":
		q.Currency, q.Price = "GBP", q.Price/100
	case "ZAc":
		q.Currency, q.Price = "ZAR", q.Price/100
	case "ILA":
		q.Currency, q.Price = "ILS", q.Price/100
	}
	q.Currency = strings.ToUpper(q.Currency)
	return q, nil
}

func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	if !st.Online || s.cfg.DemoData {
		writeErr(w, http.StatusConflict, "looking prices up online is off (Settings → General)")
		return
	}
	q, err := s.lookup(ctx, r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// onlineFor reports whether Wealth may go online for a person.
func (s *Server) onlineFor(st store.Settings) bool { return st.Online && !s.cfg.DemoData }

// updatePrices looks up every holding with a symbol for one person.
func (s *Server) updatePrices(ctx context.Context, uid int64) (int, int, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	if !s.onlineFor(st) {
		return 0, 0, errors.New("looking prices up online is off")
	}
	pos, err := s.store.Positions(ctx, uid, 0)
	if err != nil {
		return 0, 0, err
	}
	b, err := s.loadBook(ctx, uid, false)
	if err != nil {
		return 0, 0, err
	}
	cache := map[string]quote{}
	cacheErr := map[string]error{}
	ok, failed := 0, 0
	for _, p := range pos {
		if !p.PriceAuto || p.Symbol == "" {
			continue
		}
		q, seen := cache[p.Symbol]
		e := cacheErr[p.Symbol]
		if !seen && e == nil {
			q, e = s.lookup(ctx, p.Symbol)
			if e != nil {
				cacheErr[p.Symbol] = e
			} else {
				cache[p.Symbol] = q
			}
		}
		if e != nil {
			failed++
			_ = s.store.SetPrice(ctx, uid, p.ID, 0, "", plainErr(e))
			continue
		}
		price := q.Price
		if q.Currency != "" && q.Currency != p.Currency {
			// a quote in another currency: convert it to the holding's
			a, okA := b.fx.unitBase(q.Currency, b.today)
			c, okC := b.fx.unitBase(p.Currency, b.today)
			if !okA || !okC || c == 0 {
				failed++
				_ = s.store.SetPrice(ctx, uid, p.ID, 0, "", fmt.Sprintf("the price is in %s and there is no %s rate", q.Currency, q.Currency))
				continue
			}
			price = price * a / c
		}
		ok++
		_ = s.store.SetPrice(ctx, uid, p.ID, price, firstNonEmpty(q.Date, b.today), "")
	}
	_ = s.store.CachePut(ctx, "prices:at:"+itoa(uid), store.Now())
	if ok > 0 {
		s.snapshot(ctx, uid)
	}
	return ok, failed, nil
}

func (s *Server) handleUpdatePrices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	ok, failed, err := s.updatePrices(ctx, actorID(r.Context()))
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": ok, "failed": failed})
}

// updateRates fetches the ECB's rates (90 days the first time) and, for the
// currencies people use that the ECB does not publish, Yahoo's.
func (s *Server) updateRates(ctx context.Context, force bool) error {
	users, _ := s.store.SettingsUsers(ctx)
	wanted := map[string]bool{}
	anyone := false
	for _, uid := range users {
		st, _ := s.store.GetSettings(ctx, uid)
		if !s.onlineFor(st) {
			continue
		}
		anyone = true
		wanted[st.Currency] = true
		curs, _ := s.store.Currencies(ctx, uid)
		for _, c := range curs {
			wanted[c] = true
		}
	}
	if !anyone {
		return nil
	}
	last := s.store.LatestRateDate(ctx, "ecb")
	if force || last < store.AddDays(store.Today(), -1) || last == "" {
		src := ecbDailyURL
		if last == "" || last < store.AddDays(store.Today(), -60) {
			src = ecb90URL
		}
		b, err := s.get(ctx, src)
		if err != nil {
			return fmt.Errorf("the European Central Bank: %w", err)
		}
		rates, err := ecbRates(b)
		if err != nil {
			return fmt.Errorf("the European Central Bank: %w", err)
		}
		if err := s.store.PutRates(ctx, rates); err != nil {
			return err
		}
	}
	// the rest from Yahoo, once a day
	list := []string{}
	for c := range wanted {
		list = append(list, c)
	}
	have, _ := s.store.AllRates(ctx, list)
	today := store.Today()
	for c := range wanted {
		if _, ok := pegged[c]; ok {
			continue
		}
		rs := have[c]
		if len(rs) > 0 && (rs[len(rs)-1].Source == "ecb" || rs[len(rs)-1].Date >= today) {
			continue
		}
		q, err := s.lookup(ctx, "EUR"+c+"=X")
		if err != nil {
			s.log.Debug("rates: no rate", "currency", c, "err", err)
			continue
		}
		_ = s.store.PutRates(ctx, []store.Rate{{Currency: c, Date: today, PerEUR: q.Price, Source: "yahoo"}})
	}
	_ = s.store.CachePut(ctx, "rates:at", store.Now())
	return nil
}

func (s *Server) handleRates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	b, err := s.loadBook(ctx, uid, true)
	if err != nil {
		s.fail(w, err, "rates")
		return
	}
	curs, _ := s.store.Currencies(ctx, uid)
	type row struct {
		Currency string   `json:"currency"`
		Rate     *float64 `json:"rate"` // 1 unit = this many of the base currency
		Date     string   `json:"date"`
		Source   string   `json:"source"` // ecb | yahoo | fixed | own | demo
		Own      *float64 `json:"own"`
	}
	out := []row{}
	seen := map[string]bool{b.base: true}
	for _, c := range curs {
		if seen[c] {
			continue
		}
		seen[c] = true
		rw := row{Currency: c}
		if v, ok := b.st.Rates[c]; ok && v > 0 {
			o := v
			rw.Own = &o
		}
		own := b.fx.own
		b.fx.own = nil
		if v, ok := b.fx.unitBase(c, b.today); ok {
			rw.Rate = &v
			if _, p := pegged[c]; p {
				rw.Source = "fixed"
			} else if list := b.fx.rates[c]; len(list) > 0 {
				rw.Date, rw.Source = list[len(list)-1].Date, list[len(list)-1].Source
			}
		}
		b.fx.own = own
		out = append(out, rw)
	}
	at, _, _ := s.store.CacheGet(ctx, "rates:at")
	writeJSON(w, http.StatusOK, map[string]any{"currency": b.base, "rates": out, "updated_at": at, "online": s.onlineFor(b.st)})
}

func (s *Server) handleUpdateRates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	st, _ := s.store.GetSettings(ctx, actorID(r.Context()))
	if !s.onlineFor(st) {
		writeErr(w, http.StatusConflict, "looking rates up online is off (Settings → General)")
		return
	}
	if err := s.updateRates(ctx, true); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.handleRates(w, r)
}
