package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// ---- changes in the other apps, as they happen ----
//
// Wealth follows Pocket Core's event bus over SSE with its app token. Core
// sends only what Wealth may read: the events of people who have the
// Wealth → <app> events:read link switched on. A transaction saved or
// deleted in Pocket Money, or an item saved in Pocket Inventory, reads that
// person's linked balances again a few seconds later (once for a burst of
// saves). The last event id is kept, so after a restart the stream resumes
// where it was; events older than a day are skipped (the hourly refresh
// catches those).

const cursorKey = "events:cursor"

// eventSources maps the topics followed to the app whose values they change.
var eventSources = map[string]string{
	"money.transaction_created": "money", "money.transaction_deleted": "money", "money.transaction_updated": "money",
	"inventory.item_created": "inventory", "inventory.item_updated": "inventory", "inventory.item_archived": "inventory", "inventory.adjusted": "inventory",
}

func streamTopics() string {
	topics := make([]string, 0, len(eventSources))
	for t := range eventSources {
		topics = append(topics, t)
	}
	sort.Strings(topics)
	return strings.Join(topics, ",")
}

func (s *Server) setStream(ok bool, err string) {
	s.streamMu.Lock()
	s.streamOK, s.streamErr = ok, err
	s.streamMu.Unlock()
}

func (s *Server) streamStatus() map[string]any {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	out := map[string]any{"configured": s.core.Configured(), "connected": s.streamOK, "error": s.streamErr}
	if !s.lastEvent.IsZero() {
		out["last_event_at"] = s.lastEvent.UTC().Format(time.RFC3339)
	}
	return out
}

// listen follows the bus until ctx ends, reconnecting with backoff.
func (s *Server) listen(ctx context.Context) {
	wait := 2 * time.Second
	for ctx.Err() == nil {
		if s.core.Token() == "" { // not registered yet
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		err := s.streamOnce(ctx, s.cursor(ctx))
		if ctx.Err() != nil {
			return
		}
		msg := "the stream ended"
		if err != nil {
			msg = err.Error()
		}
		s.setStream(false, msg)
		s.log.Debug("pocket core: event stream closed, reconnecting", "err", msg, "in", wait.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err == nil {
			wait = 2 * time.Second
		} else if wait < time.Minute {
			wait *= 2
		}
	}
}

// cursor is the last event id seen; on the very first start it is the
// newest event on the bus, so old history is left to the scans.
func (s *Server) cursor(ctx context.Context) int64 {
	if v, _, ok := s.store.CacheGet(ctx, cursorKey); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	var last int64
	if evs, err := s.core.Events(ctx, "", 0, 1); err == nil && len(evs) > 0 {
		last = evs[0].ID
	}
	_ = s.store.CachePut(ctx, cursorKey, strconv.FormatInt(last, 10))
	return last
}

func (s *Server) streamOnce(ctx context.Context, after int64) error {
	q := url.Values{"topics": {streamTopics()}}
	if after > 0 {
		q.Set("after", strconv.FormatInt(after, 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.CoreURL+"/api/v1/events/stream?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.core.Token())
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	if after > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(after, 10))
	}
	res, err := (&http.Client{}).Do(req) // long-lived: no timeout
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("Pocket Core answered %d %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	s.setStream(true, "")
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if data.Len() > 0 {
				var e core.Event
				if json.Unmarshal([]byte(data.String()), &e) == nil && e.ID > 0 {
					s.handleEvent(ctx, e)
					_ = s.store.CachePut(ctx, cursorKey, strconv.FormatInt(e.ID, 10))
				}
				data.Reset()
			}
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return sc.Err()
}

// eventOwner is the person an event belongs to.
func eventOwner(p map[string]any) string {
	for _, k := range []string{"user_email", "owner_email", "user"} {
		if v, ok := p[k].(string); ok && strings.Contains(v, "@") {
			return strings.ToLower(strings.TrimSpace(v))
		}
	}
	return ""
}

// handleEvent refreshes the linked values an event is about, for its owner.
func (s *Server) handleEvent(ctx context.Context, e core.Event) {
	app, ok := eventSources[e.Topic]
	if !ok {
		return
	}
	if at, err := time.Parse(time.RFC3339Nano, e.CreatedAt); err == nil && time.Since(at) > 24*time.Hour {
		return
	}
	var p map[string]any
	if json.Unmarshal(e.Payload, &p) != nil {
		return
	}
	email := eventOwner(p)
	if email == "" {
		return
	}
	u, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return // not a person of this box (yet)
	}
	st, err := s.store.GetSettings(ctx, u.ID)
	if err != nil || !st.Live {
		return
	}
	s.streamMu.Lock()
	s.lastEvent = time.Now()
	s.streamMu.Unlock()
	uid := u.ID
	s.soon("live:"+app+":"+itoa(uid), liveDelay, func(ctx context.Context) {
		if _, err := s.refreshLinked(ctx, uid, app); err != nil {
			s.log.Debug("event: refreshing linked values failed", "app", app, "err", err)
		}
		s.checkMilestones(ctx, uid)
	})
}

// liveDelay gathers a burst of saves into one refresh.
var liveDelay = 5 * time.Second
