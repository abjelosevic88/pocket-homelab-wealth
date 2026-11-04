package server

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// ---- background work ----
//
// Every minute: the daily message at each person's hour. Every hour: the
// linked values (Money, Inventory, Garage, Home), today's snapshot of what
// positions and linked apps are worth (so the history has them), and new
// milestones. Every six hours: exchange rates and prices.

func (s *Server) janitor(ctx context.Context) {
	first := time.NewTimer(15 * time.Second)
	minute := time.NewTicker(time.Minute)
	hour := time.NewTicker(time.Hour)
	six := time.NewTicker(6 * time.Hour)
	defer minute.Stop()
	defer hour.Stop()
	defer six.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			s.online(ctx)
			s.hourly(ctx)
		case <-minute.C:
			s.pushes(ctx)
		case <-hour.C:
			s.hourly(ctx)
			_ = s.store.PurgeSessions(ctx)
		case <-six.C:
			s.online(ctx)
		}
	}
}

func (s *Server) hourly(ctx context.Context) {
	users, _ := s.store.SettingsUsers(ctx)
	for _, uid := range users {
		bctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		if _, err := s.refreshLinked(bctx, uid, ""); err != nil {
			s.log.Debug("linked values: refresh failed", "user", uid, "err", err)
		}
		s.snapshot(bctx, uid)
		s.checkMilestones(bctx, uid)
		cancel()
	}
}

func (s *Server) online(ctx context.Context) {
	bctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := s.updateRates(bctx, false); err != nil {
		s.log.Warn("exchange rates: update failed", "err", err)
	}
	users, _ := s.store.SettingsUsers(bctx)
	for _, uid := range users {
		st, _ := s.store.GetSettings(bctx, uid)
		if !s.onlineFor(st) {
			continue
		}
		if _, failed, err := s.updatePrices(bctx, uid); err != nil || failed > 0 {
			s.log.Debug("prices: update", "user", uid, "failed", failed, "err", err)
		}
	}
}

// snapshot writes today's value of every asset whose value is computed
// (positions) or read from another app (linked) when it changed, so the
// history can show it later.
func (s *Server) snapshot(ctx context.Context, uid int64) {
	b, err := s.loadBook(ctx, uid, false)
	if err != nil {
		return
	}
	for i := range b.assets {
		a := &b.assets[i]
		if a.ValueMode != "positions" && a.ValueMode != "linked" {
			continue
		}
		if a.ValueMode == "positions" && len(b.pos[a.ID]) == 0 {
			continue
		}
		if a.ValueMode == "linked" && a.LinkedValue == nil {
			continue
		}
		v, ok := b.valueAt(a, b.today)
		if !ok {
			continue
		}
		last, has := b.latest(a.ID, b.today)
		if has && last.Date == b.today && last.Source != "auto" {
			continue // typed in today: leave it
		}
		if has && last.Value == v {
			continue
		}
		_ = s.store.PutValuation(ctx, uid, &store.Valuation{AssetID: a.ID, Date: b.today, Value: v, Source: "auto"})
	}
}

// soon runs work for one person a moment from now, once for a burst of changes.
func (s *Server) soon(key string, wait time.Duration, fn func(ctx context.Context)) {
	s.scanMu.Lock()
	if s.pending[key] {
		s.scanMu.Unlock()
		return
	}
	s.pending[key] = true
	s.scanMu.Unlock()
	time.AfterFunc(wait, func() {
		s.scanMu.Lock()
		delete(s.pending, key)
		s.scanMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		fn(ctx)
	})
}

func (s *Server) snapshotSoon(uid int64) {
	s.soon("snapshot:"+itoa(uid), 3*time.Second, func(ctx context.Context) { s.snapshot(ctx, uid); s.checkMilestones(ctx, uid) })
}

func (s *Server) pricesSoon(uid int64) {
	s.soon("prices:"+itoa(uid), 2*time.Second, func(ctx context.Context) {
		st, _ := s.store.GetSettings(ctx, uid)
		if s.onlineFor(st) {
			_ = s.updateRates(ctx, false)
			_, _, _ = s.updatePrices(ctx, uid)
		}
	})
}

func (s *Server) checkMilestonesSoon(uid int64) {
	s.soon("milestones:"+itoa(uid), 3*time.Second, func(ctx context.Context) { s.checkMilestones(ctx, uid) })
}

// checkMilestones records the round numbers net worth has passed. The very
// first time, the ones already behind are recorded quietly.
func (s *Server) checkMilestones(ctx context.Context, uid int64) {
	b, err := s.loadBook(ctx, uid, true)
	if err != nil || len(b.assets) == 0 {
		return
	}
	first := false
	key := "milestones:init:" + itoa(uid) + ":" + b.base
	if _, _, ok := s.store.CacheGet(ctx, key); !ok {
		first = true
		_ = s.store.CachePut(ctx, key, "1")
	}
	for _, m := range b.milestones() {
		if m.Next || m.ReachedOn == "" {
			continue
		}
		isNew, err := s.store.ReachMilestone(ctx, uid, store.Milestone{Amount: m.Amount, Currency: b.base, ReachedOn: m.ReachedOn})
		if err != nil || !isNew || first {
			continue
		}
		s.publish(ctx, uid, "wealth.milestone_reached", "Net worth "+money(m.Amount, b.base), map[string]any{"amount": m.Amount, "currency": b.base, "reached_on": m.ReachedOn})
		if b.st.NotifyMilestones {
			s.queueMessage(ctx, uid, "Net worth passed "+money(m.Amount, b.base)+" 🎉")
		}
	}
}

// ---- small helpers ----

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func remarshalString(s string, out any) error { return json.Unmarshal([]byte(s), out) }

// remarshal turns a decoded map into a struct.
func remarshal(in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
