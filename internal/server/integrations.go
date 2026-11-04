package server

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// ---------- pickers: what the other apps have ----------

// pick answers a picker: the items, or why the app is not reachable.
func pick(w http.ResponseWriter, items any, err error, app pocketApp) {
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": !isNotConnected(err), "detail": app.name + ": " + strings.TrimPrefix(err.Error(), app.name+" "), "items": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "items": items})
}

func (s *Server) handlePickDocuments(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appDocuments, scopeDocumentsRead)
	if err != nil {
		pick(w, nil, err, appDocuments)
		return
	}
	docs, err := conn.client.SearchDocuments(ctx, strings.TrimSpace(r.URL.Query().Get("q")), 15)
	pick(w, docs, err, appDocuments)
}
