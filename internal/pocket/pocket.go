// Package pocket talks to sibling Pocket apps: the ones Wealth reads values
// from (Money, Garage, Home, Inventory) and the one it keeps statements in
// (Documents). Same bearer-token JSON API everywhere.
package pocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and token are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

// ErrNotFound is a 404 from the sibling app.
var ErrNotFound = errors.New("not found")

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, v any) error {
	if !c.Configured() {
		return errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errors.New("token rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		_ = json.Unmarshal(b, &e)
		if e.Error != "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// Health is the connection test for any Pocket app: /healthz plus a token check.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if !c.Configured() {
		return "", errors.New("not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var me map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, nil, &me); err != nil {
		return out.Version, err
	}
	return out.Version, nil
}

// Do is a raw JSON call (for a connection test).
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body, v any) error {
	return c.do(ctx, method, path, q, body, v)
}

// list decodes a list that may be bare or wrapped in {key:[…]} / {results:[…]}.
func list[T any](raw json.RawMessage, keys ...string) ([]T, error) {
	var out []T
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, nil
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	for _, k := range append(keys, "results", "items") {
		if b, ok := wrap[k]; ok {
			if err := json.Unmarshal(b, &out); err != nil {
				return nil, err
			}
			return out, nil
		}
	}
	return []T{}, nil
}

// ---------------- Pocket Money ----------------

// Account is a Money account the person can see, with its balance.
type Account struct {
	ID              int64   `json:"id"`
	Name            string  `json:"name"`
	Kind            string  `json:"kind"` // checking | savings | cash | credit | investment | loan | mortgage | debt | other
	Currency        string  `json:"currency"`
	Institution     string  `json:"institution"`
	Number          string  `json:"number"`
	Balance         int64   `json:"balance"` // minor units; liabilities are negative when owed
	Direction       string  `json:"direction"`
	InterestRate    float64 `json:"interest_rate"`
	Payment         int64   `json:"payment"`
	IncludeNetWorth bool    `json:"include_net_worth"`
	Archived        bool    `json:"archived"`
	Role            string  `json:"role"` // owner | editor | viewer
}

// Accounts lists the accounts the person can see (archived ones left out).
func (c *Client) Accounts(ctx context.Context) ([]Account, error) {
	var all []Account
	if err := c.do(ctx, http.MethodGet, "/api/v1/accounts", nil, nil, &all); err != nil {
		return nil, err
	}
	out := []Account{}
	for _, a := range all {
		if !a.Archived {
			out = append(out, a)
		}
	}
	return out, nil
}

// Totals is what came in and went out over a period, in Money's home currency.
type Totals struct {
	Count    int    `json:"count"`
	Income   int64  `json:"income"`
	Expense  int64  `json:"expense"`
	Currency string `json:"currency"`
}

// Totals adds up the person's transactions from one day to another (transfers left out).
func (c *Client) Totals(ctx context.Context, from, to string) (Totals, error) {
	var out struct {
		Totals   Totals `json:"totals"`
		Currency string `json:"currency"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/transactions", url.Values{"from": {from}, "to": {to}, "limit": {"1"}}, nil, &out)
	out.Totals.Currency = firstSet(out.Totals.Currency, out.Currency)
	return out.Totals, err
}

func firstSet(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---------------- Pocket Garage ----------------

// Vehicle is a vehicle in Pocket Garage.
type Vehicle struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Year          int    `json:"year"`
	Make          string `json:"make"`
	Model         string `json:"model"`
	Plate         string `json:"plate"`
	Currency      string `json:"currency"`
	PurchaseDate  string `json:"purchase_date"`
	PurchasePrice int64  `json:"purchase_price"`
	SoldDate      string `json:"sold_date"`
	SoldPrice     int64  `json:"sold_price"`
	Status        string `json:"status"` // active | sold | archived
}

// Title is how the vehicle is called.
func (v Vehicle) Title() string {
	if v.DisplayName != "" {
		return v.DisplayName
	}
	if v.Name != "" {
		return v.Name
	}
	return strings.TrimSpace(fmt.Sprintf("%d %s %s", v.Year, v.Make, v.Model))
}

// Vehicles lists the person's vehicles.
func (c *Client) Vehicles(ctx context.Context) ([]Vehicle, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/vehicles", url.Values{"bare": {"1"}}, nil, &raw); err != nil {
		return nil, err
	}
	return list[Vehicle](raw, "vehicles")
}

// ---------------- Pocket Home ----------------

// Home is a home in Pocket Home.
type Home struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Address   string   `json:"address"`
	YearBuilt *int     `json:"year_built"`
	AreaM2    *float64 `json:"area_m2"`
	Currency  string   `json:"currency"`
	Archived  bool     `json:"archived"`
}

// Homes lists the person's homes.
func (c *Client) Homes(ctx context.Context) ([]Home, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/homes", nil, nil, &raw); err != nil {
		return nil, err
	}
	return list[Home](raw, "homes")
}

// ---------------- Pocket Inventory ----------------

// Item is a Pocket Inventory item.
type Item struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Quantity float64  `json:"quantity"`
	Category string   `json:"category_name"`
	Price    *float64 `json:"price"` // each, major units
	Archived bool     `json:"archived"`
}

// Items lists the person's items (up to 5000).
func (c *Client) Items(ctx context.Context) ([]Item, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/items", url.Values{"limit": {"5000"}}, nil, &raw); err != nil {
		return nil, err
	}
	return list[Item](raw, "items")
}

// text reads a text/plain answer (at most max bytes).
func (c *Client) text(ctx context.Context, path string, max int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, max))
	return string(b), err
}

// ---------------- Pocket Documents ----------------

// FieldValue is a custom field's value on a document (always a string).
type FieldValue struct {
	Field int64  `json:"field"`
	Value string `json:"value"`
}

// Document is a document in Pocket Documents.
type Document struct {
	ID            int64        `json:"id"`
	Title         string       `json:"title"`
	OriginalName  string       `json:"original_name"`
	Created       string       `json:"created"`
	AddedAt       string       `json:"added_at"`
	ModifiedAt    string       `json:"modified_at"`
	Correspondent string       `json:"correspondent_name"`
	DocumentType  string       `json:"document_type_name"`
	Status        string       `json:"status"`
	CustomFields  []FieldValue `json:"custom_fields"`
}

// CustomField is a field defined in Pocket Documents.
type CustomField struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"data_type"`
}

// SearchDocuments finds the person's documents (newest first without a query).
func (c *Client) SearchDocuments(ctx context.Context, q string, limit int) ([]Document, error) {
	v := url.Values{"limit": {fmt.Sprint(limit)}, "no_facets": {"1"}}
	if q != "" {
		v.Set("q", q)
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/documents", v, nil, &raw); err != nil {
		return nil, err
	}
	return list[Document](raw, "documents")
}

// ListDocuments pages through the person's documents, last changed first.
func (c *Client) ListDocuments(ctx context.Context, offset, limit int) ([]Document, error) {
	v := url.Values{"limit": {fmt.Sprint(limit)}, "offset": {fmt.Sprint(offset)}, "no_facets": {"1"}, "sort": {"-modified"}}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/documents", v, nil, &raw); err != nil {
		return nil, err
	}
	return list[Document](raw, "documents")
}

// GetDocument reads one document with its custom fields.
func (c *Client) GetDocument(ctx context.Context, id int64) (*Document, error) {
	var out struct {
		Document Document `json:"document"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/documents/%d", id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Document, nil
}

// DocumentText is a document's OCR text (the first 64 KiB).
func (c *Client) DocumentText(ctx context.Context, id int64) (string, error) {
	return c.text(ctx, fmt.Sprintf("/api/v1/documents/%d/text", id), 64<<10)
}
