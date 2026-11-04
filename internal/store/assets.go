package store

import (
	"context"
	"database/sql"
	"strings"
)

// Every query here takes the person's id and filters on it: another
// person's row is simply not found.

const assetCols = `a.id, a.user_id, a.name, a.kind, a.currency, a.institution, a.number, a.notes, a.value_mode, a.include, a.share, a.rate,
	a.payment, a.growth, a.matures_on, a.bought_on, a.bought_price, a.invested, a.source, a.source_ref, a.source_url, a.linked_value,
	a.linked_at, a.linked_error, a.closed_on, a.sort, a.created_at, a.updated_at,
	(SELECT COUNT(1) FROM positions p WHERE p.asset_id = a.id)`

func scanAsset(row interface{ Scan(...any) error }) (*Asset, error) {
	var a Asset
	var inc int
	var growth sql.NullFloat64
	var bought, invested, linked sql.NullInt64
	if err := row.Scan(&a.ID, &a.UserID, &a.Name, &a.Kind, &a.Currency, &a.Institution, &a.Number, &a.Notes, &a.ValueMode, &inc, &a.Share, &a.Rate,
		&a.Payment, &growth, &a.MaturesOn, &a.BoughtOn, &bought, &invested, &a.Source, &a.SourceRef, &a.SourceURL, &linked,
		&a.LinkedAt, &a.LinkedError, &a.ClosedOn, &a.Sort, &a.CreatedAt, &a.UpdatedAt, &a.Positions); err != nil {
		return nil, notFound(err)
	}
	a.Include = inc == 1
	if growth.Valid {
		g := growth.Float64
		a.Growth = &g
	}
	a.BoughtPrice, a.Invested, a.LinkedValue = ptr(bought), ptr(invested), ptr(linked)
	k := KindOf(a.Kind)
	a.Group, a.Debt, a.Liquid = k.Group, k.Debt, k.Liquid
	return &a, nil
}

func ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func nullF(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func nullZ(i *int64) any {
	if i == nil {
		return nil
	}
	return *i
}

// ListAssets lists a person's assets and debts (closed ones too when asked).
func (s *Store) ListAssets(ctx context.Context, uid int64, closed bool) ([]Asset, error) {
	q := `SELECT ` + assetCols + ` FROM assets a WHERE a.user_id = ?`
	if !closed {
		q += ` AND (a.closed_on = '' OR a.closed_on > date('now','localtime'))`
	}
	q += ` ORDER BY a.sort, a.id`
	rows, err := s.DB.QueryContext(ctx, q, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// GetAsset reads one of a person's assets.
func (s *Store) GetAsset(ctx context.Context, uid, id int64) (*Asset, error) {
	return scanAsset(s.DB.QueryRowContext(ctx, `SELECT `+assetCols+` FROM assets a WHERE a.user_id = ? AND a.id = ?`, uid, id))
}

// AssetBySource finds the asset linked to an item in another app.
func (s *Store) AssetBySource(ctx context.Context, uid int64, ref string) (*Asset, error) {
	return scanAsset(s.DB.QueryRowContext(ctx, `SELECT `+assetCols+` FROM assets a WHERE a.user_id = ? AND a.source_ref = ?`, uid, ref))
}

// CreateAsset adds an asset for a person.
func (s *Store) CreateAsset(ctx context.Context, uid int64, a *Asset) error {
	if a.Share <= 0 || a.Share > 1 {
		a.Share = 1
	}
	if a.ValueMode == "" {
		a.ValueMode = KindOf(a.Kind).Mode
	}
	if a.Source == "" {
		a.Source = "web"
	}
	if a.Sort == 0 {
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort), 0) + 1 FROM assets WHERE user_id = ?`, uid).Scan(&a.Sort)
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO assets (user_id, name, kind, currency, institution, number, notes, value_mode, include, share, rate,
		payment, growth, matures_on, bought_on, bought_price, invested, source, source_ref, source_url, linked_value, linked_at, linked_error, closed_on, sort)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, a.Name, a.Kind, a.Currency, a.Institution, a.Number, a.Notes, a.ValueMode, boolInt(a.Include), a.Share, a.Rate,
		a.Payment, nullF(a.Growth), a.MaturesOn, a.BoughtOn, nullZ(a.BoughtPrice), nullZ(a.Invested), a.Source, a.SourceRef, a.SourceURL,
		nullZ(a.LinkedValue), a.LinkedAt, a.LinkedError, a.ClosedOn, a.Sort)
	if err != nil {
		return conflict(err)
	}
	a.ID, _ = res.LastInsertId()
	a.UserID = uid
	got, err := s.GetAsset(ctx, uid, a.ID)
	if err == nil {
		*a = *got
	}
	return err
}

// UpdateAsset saves an asset (all of its own fields).
func (s *Store) UpdateAsset(ctx context.Context, uid int64, a *Asset) error {
	if a.Share <= 0 || a.Share > 1 {
		a.Share = 1
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE assets SET name = ?, kind = ?, currency = ?, institution = ?, number = ?, notes = ?, value_mode = ?,
		include = ?, share = ?, rate = ?, payment = ?, growth = ?, matures_on = ?, bought_on = ?, bought_price = ?, invested = ?, source_ref = ?,
		source_url = ?, closed_on = ?, sort = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND id = ?`,
		a.Name, a.Kind, a.Currency, a.Institution, a.Number, a.Notes, a.ValueMode, boolInt(a.Include), a.Share, a.Rate, a.Payment,
		nullF(a.Growth), a.MaturesOn, a.BoughtOn, nullZ(a.BoughtPrice), nullZ(a.Invested), a.SourceRef, a.SourceURL, a.ClosedOn, a.Sort, uid, a.ID)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	got, err := s.GetAsset(ctx, uid, a.ID)
	if err == nil {
		*a = *got
	}
	return err
}

// SetLinked stores what the linked app said the asset is worth (or why it could not say).
func (s *Store) SetLinked(ctx context.Context, uid, id int64, value *int64, errText, url string) error {
	if value == nil {
		_, err := s.DB.ExecContext(ctx, `UPDATE assets SET linked_error = ? WHERE user_id = ? AND id = ?`, errText, uid, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE assets SET linked_value = ?, linked_at = ?, linked_error = '',
		source_url = CASE WHEN ? <> '' THEN ? ELSE source_url END WHERE user_id = ? AND id = ?`, *value, Now(), url, url, uid, id)
	return err
}

// SetClosed closes (or reopens with "") an asset.
func (s *Store) SetClosed(ctx context.Context, uid, id int64, day string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE assets SET closed_on = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND id = ?`, day, uid, id)
	return err
}

// DeleteAsset removes an asset with its valuations, positions and activity.
func (s *Store) DeleteAsset(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM assets WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderAssets sets the order of a person's assets.
func (s *Store) ReorderAssets(ctx context.Context, uid int64, ids []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE assets SET sort = ? WHERE user_id = ? AND id = ?`, i+1, uid, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ---- valuations ----

const valCols = `id, user_id, asset_id, date, value, note, source, created_at`

func scanVal(row interface{ Scan(...any) error }) (*Valuation, error) {
	var v Valuation
	if err := row.Scan(&v.ID, &v.UserID, &v.AssetID, &v.Date, &v.Value, &v.Note, &v.Source, &v.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	return &v, nil
}

// Valuations lists a person's valuations, oldest first (one asset, or all with assetID 0).
func (s *Store) Valuations(ctx context.Context, uid, assetID int64) ([]Valuation, error) {
	q, args := `SELECT `+valCols+` FROM valuations WHERE user_id = ?`, []any{uid}
	if assetID > 0 {
		q += ` AND asset_id = ?`
		args = append(args, assetID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY asset_id, date`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Valuation{}
	for rows.Next() {
		v, err := scanVal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// GetValuation reads one valuation.
func (s *Store) GetValuation(ctx context.Context, uid, id int64) (*Valuation, error) {
	return scanVal(s.DB.QueryRowContext(ctx, `SELECT `+valCols+` FROM valuations WHERE user_id = ? AND id = ?`, uid, id))
}

// LatestValuations is each asset's newest valuation.
func (s *Store) LatestValuations(ctx context.Context, uid int64) (map[int64]Valuation, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+valCols+` FROM valuations v WHERE user_id = ?
		AND date = (SELECT MAX(date) FROM valuations w WHERE w.asset_id = v.asset_id)`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Valuation{}
	for rows.Next() {
		v, err := scanVal(rows)
		if err != nil {
			return nil, err
		}
		out[v.AssetID] = *v
	}
	return out, rows.Err()
}

// PutValuation sets an asset's value on a day (one per day: a second one replaces it).
func (s *Store) PutValuation(ctx context.Context, uid int64, v *Valuation) error {
	if v.Source == "" {
		v.Source = "web"
	}
	err := s.DB.QueryRowContext(ctx, `INSERT INTO valuations (user_id, asset_id, date, value, note, source) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(asset_id, date) DO UPDATE SET value = excluded.value, note = excluded.note, source = excluded.source
		WHERE valuations.user_id = excluded.user_id
		RETURNING id, created_at`, uid, v.AssetID, v.Date, v.Value, v.Note, v.Source).Scan(&v.ID, &v.CreatedAt)
	if err != nil {
		return notFound(err)
	}
	v.UserID = uid
	return nil
}

// DeleteValuation removes a valuation.
func (s *Store) DeleteValuation(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM valuations WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- positions ----

const posCols = `p.id, p.user_id, p.asset_id, p.name, p.symbol, p.kind, p.units, p.cost, p.currency, p.price, p.price_on, p.price_auto,
	p.price_error, p.notes, p.sort, p.created_at, p.updated_at, a.name`

func scanPos(row interface{ Scan(...any) error }) (*Position, error) {
	var p Position
	var auto int
	if err := row.Scan(&p.ID, &p.UserID, &p.AssetID, &p.Name, &p.Symbol, &p.Kind, &p.Units, &p.Cost, &p.Currency, &p.Price, &p.PriceOn, &auto,
		&p.PriceError, &p.Notes, &p.Sort, &p.CreatedAt, &p.UpdatedAt, &p.AssetName); err != nil {
		return nil, notFound(err)
	}
	p.PriceAuto = auto == 1
	return &p, nil
}

// Positions lists a person's holdings (one asset's, or all with assetID 0).
func (s *Store) Positions(ctx context.Context, uid, assetID int64) ([]Position, error) {
	q, args := `SELECT `+posCols+` FROM positions p JOIN assets a ON a.id = p.asset_id WHERE p.user_id = ?`, []any{uid}
	if assetID > 0 {
		q += ` AND p.asset_id = ?`
		args = append(args, assetID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY a.sort, p.sort, p.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Position{}
	for rows.Next() {
		p, err := scanPos(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetPosition reads one holding.
func (s *Store) GetPosition(ctx context.Context, uid, id int64) (*Position, error) {
	return scanPos(s.DB.QueryRowContext(ctx, `SELECT `+posCols+` FROM positions p JOIN assets a ON a.id = p.asset_id WHERE p.user_id = ? AND p.id = ?`, uid, id))
}

// CreatePosition adds a holding to one of the person's assets.
func (s *Store) CreatePosition(ctx context.Context, uid int64, p *Position) error {
	if _, err := s.GetAsset(ctx, uid, p.AssetID); err != nil {
		return err
	}
	if p.Sort == 0 {
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort), 0) + 1 FROM positions WHERE asset_id = ?`, p.AssetID).Scan(&p.Sort)
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO positions (user_id, asset_id, name, symbol, kind, units, cost, currency, price, price_on, price_auto, price_error, notes, sort)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, p.AssetID, p.Name, p.Symbol, p.Kind, p.Units, p.Cost, p.Currency, p.Price, p.PriceOn,
		boolInt(p.PriceAuto), p.PriceError, p.Notes, p.Sort)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	got, err := s.GetPosition(ctx, uid, p.ID)
	if err == nil {
		*p = *got
	}
	return err
}

// UpdatePosition saves a holding.
func (s *Store) UpdatePosition(ctx context.Context, uid int64, p *Position) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE positions SET name = ?, symbol = ?, kind = ?, units = ?, cost = ?, currency = ?, price = ?, price_on = ?,
		price_auto = ?, price_error = ?, notes = ?, sort = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND id = ?`,
		p.Name, p.Symbol, p.Kind, p.Units, p.Cost, p.Currency, p.Price, p.PriceOn, boolInt(p.PriceAuto), p.PriceError, p.Notes, p.Sort, uid, p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	got, err := s.GetPosition(ctx, uid, p.ID)
	if err == nil {
		*p = *got
	}
	return err
}

// SetPrice stores a price looked up online (or why it could not be).
func (s *Store) SetPrice(ctx context.Context, uid, id int64, price float64, day, errText string) error {
	if errText != "" {
		_, err := s.DB.ExecContext(ctx, `UPDATE positions SET price_error = ? WHERE user_id = ? AND id = ?`, errText, uid, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE positions SET price = ?, price_on = ?, price_error = '' WHERE user_id = ? AND id = ?`, price, day, uid, id)
	return err
}

// DeletePosition removes a holding (its activity stays, without the link).
func (s *Store) DeletePosition(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM positions WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- activity ----

const actCols = `x.id, x.user_id, x.asset_id, x.position_id, x.date, x.kind, x.units, x.amount, x.currency, x.note, x.source, x.created_at,
	a.name, COALESCE(p.name, '')`

func scanAct(row interface{ Scan(...any) error }) (*Activity, error) {
	var x Activity
	var pos sql.NullInt64
	if err := row.Scan(&x.ID, &x.UserID, &x.AssetID, &pos, &x.Date, &x.Kind, &x.Units, &x.Amount, &x.Currency, &x.Note, &x.Source, &x.CreatedAt,
		&x.AssetName, &x.PositionName); err != nil {
		return nil, notFound(err)
	}
	x.PositionID = ptr(pos)
	return &x, nil
}

// ActivityFilter narrows the activity list.
type ActivityFilter struct {
	AssetID int64
	Kind    string
	From    string
	To      string
	Limit   int
}

// ListActivity lists a person's activity, newest first.
func (s *Store) ListActivity(ctx context.Context, uid int64, f ActivityFilter) ([]Activity, error) {
	q := `SELECT ` + actCols + ` FROM activity x JOIN assets a ON a.id = x.asset_id LEFT JOIN positions p ON p.id = x.position_id WHERE x.user_id = ?`
	args := []any{uid}
	if f.AssetID > 0 {
		q += ` AND x.asset_id = ?`
		args = append(args, f.AssetID)
	}
	if f.Kind != "" {
		q += ` AND x.kind IN (` + strings.TrimSuffix(strings.Repeat("?,", len(strings.Split(f.Kind, ","))), ",") + `)`
		for _, k := range strings.Split(f.Kind, ",") {
			args = append(args, k)
		}
	}
	if f.From != "" {
		q += ` AND x.date >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND x.date <= ?`
		args = append(args, f.To)
	}
	q += ` ORDER BY x.date DESC, x.id DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		x, err := scanAct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// GetActivity reads one activity row.
func (s *Store) GetActivity(ctx context.Context, uid, id int64) (*Activity, error) {
	return scanAct(s.DB.QueryRowContext(ctx, `SELECT `+actCols+` FROM activity x JOIN assets a ON a.id = x.asset_id LEFT JOIN positions p ON p.id = x.position_id
		WHERE x.user_id = ? AND x.id = ?`, uid, id))
}

// InsertActivity records activity on one of the person's assets.
func (s *Store) InsertActivity(ctx context.Context, uid int64, x *Activity) error {
	if _, err := s.GetAsset(ctx, uid, x.AssetID); err != nil {
		return err
	}
	if x.PositionID != nil {
		if p, err := s.GetPosition(ctx, uid, *x.PositionID); err != nil || p.AssetID != x.AssetID {
			return ErrNotFound
		}
	}
	if x.Source == "" {
		x.Source = "web"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO activity (user_id, asset_id, position_id, date, kind, units, amount, currency, note, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, x.AssetID, nullI(x.PositionID), x.Date, x.Kind, x.Units, x.Amount, x.Currency, x.Note, x.Source)
	if err != nil {
		return err
	}
	x.ID, _ = res.LastInsertId()
	got, err := s.GetActivity(ctx, uid, x.ID)
	if err == nil {
		*x = *got
	}
	return err
}

// DeleteActivity removes an activity row.
func (s *Store) DeleteActivity(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM activity WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- attachments ----

// Attachments lists what is kept with an asset.
func (s *Store) Attachments(ctx context.Context, uid, assetID int64) ([]Attachment, error) {
	q, args := `SELECT id, user_id, asset_id, document_id, title, url, created_at FROM attachments WHERE user_id = ?`, []any{uid}
	if assetID > 0 {
		q += ` AND asset_id = ?`
		args = append(args, assetID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var a Attachment
		var doc sql.NullInt64
		if err := rows.Scan(&a.ID, &a.UserID, &a.AssetID, &doc, &a.Title, &a.URL, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.DocumentID = ptr(doc)
		out = append(out, a)
	}
	return out, rows.Err()
}

// AddAttachment keeps a document or a link with one of the person's assets.
func (s *Store) AddAttachment(ctx context.Context, uid int64, a *Attachment) error {
	if _, err := s.GetAsset(ctx, uid, a.AssetID); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO attachments (user_id, asset_id, document_id, title, url) VALUES (?, ?, ?, ?, ?)`,
		uid, a.AssetID, nullI(a.DocumentID), a.Title, a.URL)
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	a.UserID, a.CreatedAt = uid, Now()
	return nil
}

// DeleteAttachment removes an attachment.
func (s *Store) DeleteAttachment(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM attachments WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- milestones ----

// Milestones lists the net worth milestones a person reached.
func (s *Store) Milestones(ctx context.Context, uid int64) ([]Milestone, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT amount, currency, reached_on FROM milestones WHERE user_id = ? ORDER BY amount`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Milestone{}
	for rows.Next() {
		var m Milestone
		if err := rows.Scan(&m.Amount, &m.Currency, &m.ReachedOn); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReachMilestone records a milestone; false when it was already reached.
func (s *Store) ReachMilestone(ctx context.Context, uid int64, m Milestone) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO milestones (user_id, amount, currency, reached_on) VALUES (?, ?, ?, ?)`, uid, m.Amount, m.Currency, m.ReachedOn)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---- dismissed suggestions ----

// Dismiss remembers that a suggestion from another app was turned down.
func (s *Store) Dismiss(ctx context.Context, uid int64, ref, title string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO dismissed (user_id, source_ref, title) VALUES (?, ?, ?)`, uid, ref, title)
	return err
}

// Undismiss offers a suggestion again.
func (s *Store) Undismiss(ctx context.Context, uid int64, ref string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM dismissed WHERE user_id = ? AND source_ref = ?`, uid, ref)
	return err
}

// Dismissed lists the refs a person turned down.
func (s *Store) Dismissed(ctx context.Context, uid int64) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT source_ref, title FROM dismissed WHERE user_id = ?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, t string
		if err := rows.Scan(&k, &t); err != nil {
			return nil, err
		}
		out[k] = t
	}
	return out, rows.Err()
}

// ---- exchange rates ----

// Rate is units of a currency per euro on a day.
type Rate struct {
	Currency string  `json:"currency"`
	Date     string  `json:"date"`
	PerEUR   float64 `json:"per_eur"`
	Source   string  `json:"source"`
}

// PutRates stores exchange rates (a newer source replaces the same day's).
func (s *Store) PutRates(ctx context.Context, rates []Rate) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, r := range rates {
		if r.PerEUR <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO fx (currency, date, per_eur, source) VALUES (?, ?, ?, ?)
			ON CONFLICT(currency, date) DO UPDATE SET per_eur = excluded.per_eur, source = excluded.source`, r.Currency, r.Date, r.PerEUR, r.Source); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// AllRates loads the rates of some currencies, oldest first per currency.
func (s *Store) AllRates(ctx context.Context, currencies []string) (map[string][]Rate, error) {
	out := map[string][]Rate{}
	if len(currencies) == 0 {
		return out, nil
	}
	args := make([]any, len(currencies))
	for i, c := range currencies {
		args[i] = c
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT currency, date, per_eur, source FROM fx WHERE currency IN (`+strings.TrimSuffix(strings.Repeat("?,", len(currencies)), ",")+`)
		ORDER BY currency, date`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Rate
		if err := rows.Scan(&r.Currency, &r.Date, &r.PerEUR, &r.Source); err != nil {
			return nil, err
		}
		out[r.Currency] = append(out[r.Currency], r)
	}
	return out, rows.Err()
}

// LatestRateDate is the newest day with rates from a source ("" when none).
func (s *Store) LatestRateDate(ctx context.Context, source string) string {
	var d sql.NullString
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(date) FROM fx WHERE source = ?`, source).Scan(&d)
	return d.String
}

// Currencies lists the currencies a person's assets, positions and activity use.
func (s *Store) Currencies(ctx context.Context, uid int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT currency FROM assets WHERE user_id = ? UNION SELECT currency FROM positions WHERE user_id = ?
		UNION SELECT currency FROM activity WHERE user_id = ?`, uid, uid, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
