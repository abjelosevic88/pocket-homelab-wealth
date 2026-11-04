package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wealth/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-wealth/internal/store"
)

// ---- notifications ----
//
// At most one message a day, at each person's own hour: on the 1st how last
// month went (and, with it, what needs an update), any milestone passed
// since yesterday, and deposits or fixed rates ending in 30 days, 7 days
// or today. wealth.monthly and wealth.matures_soon are published either way.

// pushes runs every minute and sends each person's message at their hour.
func (s *Server) pushes(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || now.Hour() != st.NotifyHour || now.Minute() > 10 {
			continue
		}
		key := fmt.Sprintf("push:%d:%s", uid, store.Today())
		if _, _, done := s.store.CacheGet(ctx, key); done {
			continue
		}
		_ = s.store.CachePut(ctx, key, "1")
		s.dailyCheck(ctx, uid, st, now)
	}
}

// queueMessage keeps a line for the person's next daily message.
func (s *Server) queueMessage(ctx context.Context, uid int64, line string) {
	key := "notify:queue:" + itoa(uid)
	v, _, _ := s.store.CacheGet(ctx, key)
	if v != "" {
		v += "\n"
	}
	_ = s.store.CachePut(ctx, key, v+line)
}

// money formats minor units for a message: "12,345 EUR".
func money(v int64, cur string) string {
	f := majorOf(v, cur)
	neg := f < 0
	f = math.Abs(f)
	whole := fmt.Sprintf("%.0f", math.Floor(f))
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteRune(',')
		}
		b.WriteRune(r)
	}
	out := b.String() + " " + cur
	if neg {
		out = "−" + out
	}
	return out
}

func signed(v int64, cur string) string {
	if v >= 0 {
		return "+" + money(v, cur)
	}
	return money(v, cur)
}

// dailyCheck builds and sends one person's daily message.
func (s *Server) dailyCheck(ctx context.Context, uid int64, st store.Settings, now time.Time) (string, bool) {
	b, err := s.loadBook(ctx, uid, true)
	if err != nil || len(b.assets) == 0 {
		return "", false
	}
	var lines []string
	title := "Pocket Wealth"
	// what was queued (milestones)
	qkey := "notify:queue:" + itoa(uid)
	if v, _, ok := s.store.CacheGet(ctx, qkey); ok && v != "" {
		for _, l := range strings.Split(v, "\n") {
			lines = append(lines, "• "+l)
		}
		_ = s.store.CacheDelete(ctx, qkey)
	}
	// the 1st: last month
	if now.Day() == 1 {
		end := lastMonthEnd(b.today)
		prev := lastMonthEnd(end)
		a, p := b.totalsAt(end), b.totalsAt(prev)
		t, _ := store.ParseDate(end)
		month := t.Format("January")
		ch := changeOf(a.Net, p.Net, prev)
		line := fmt.Sprintf("Net worth at the end of %s: %s (%s", month, money(a.Net, b.base), signed(ch.Amount, b.base))
		if ch.Pct != nil {
			line += fmt.Sprintf(", %+.1f%%", *ch.Pct)
		}
		line += ")"
		s.publish(ctx, uid, "wealth.monthly", month, map[string]any{"month": end[:7], "net": a.Net, "change": ch.Amount, "currency": b.base,
			"assets": a.Assets, "debts": a.Debts})
		if st.NotifyMonthly {
			title = "Your " + month
			lines = append(lines, "• "+line)
			if mv := s.biggestMover(b, prev, end); mv != "" {
				lines = append(lines, "• "+mv)
			}
			if st.NotifyStale {
				var stale []string
				for i := range b.assets {
					if a := &b.assets[i]; a.Include && b.stale(a) {
						stale = append(stale, a.Name)
					}
				}
				if len(stale) > 0 {
					sort.Strings(stale)
					if len(stale) > 6 {
						stale = append(stale[:6], fmt.Sprintf("%d more", len(stale)-6))
					}
					lines = append(lines, "• Time to update: "+strings.Join(stale, ", "))
				}
			}
		}
	}
	// deposits and fixed rates ending
	for i := range b.assets {
		a := &b.assets[i]
		if a.MaturesOn == "" || a.Closed(b.today) {
			continue
		}
		d := store.DaysBetween(b.today, a.MaturesOn)
		if d != 30 && d != 7 && d != 0 {
			continue
		}
		what := "matures"
		if a.Debt {
			what = "ends (the fixed rate or the last payment)"
		}
		when := fmt.Sprintf("in %d days (%s)", d, a.MaturesOn)
		if d == 0 {
			when = "today"
		}
		s.publish(ctx, uid, "wealth.matures_soon", a.Name, map[string]any{"asset_id": a.ID, "name": a.Name, "matures_on": a.MaturesOn, "days": d})
		if st.NotifyMaturity {
			lines = append(lines, "• "+a.Name+" "+what+" "+when)
		}
	}
	if len(lines) == 0 || st.NotifyURL == "" {
		return strings.Join(lines, "\n"), false
	}
	body := strings.Join(lines, "\n")
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: title, Body: body, Priority: "info", Tags: []string{"moneybag"}, Link: s.cfg.BaseURL + "/"}); err != nil {
		s.log.Warn("notify", "user", uid, "err", err)
		return body, false
	}
	return body, true
}

// biggestMover is the asset whose value changed most between two days.
func (s *Server) biggestMover(b *book, from, to string) string {
	best, name := int64(0), ""
	for i := range b.assets {
		a := &b.assets[i]
		if !a.Include {
			continue
		}
		x, ok1 := b.baseAt(a, to)
		y, ok2 := b.baseAt(a, from)
		if !ok1 || !ok2 {
			continue
		}
		d := x - y
		if a.Debt {
			d = -d
		}
		if abs64(d) > abs64(best) {
			best, name = d, a.Name
		}
	}
	if name == "" || best == 0 {
		return ""
	}
	return "Biggest change: " + name + " " + signed(best, b.base)
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Wealth", Body: "Notifications work. Your monthly summary and milestones will arrive here.", Priority: "info", Tags: []string{"moneybag"}}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
