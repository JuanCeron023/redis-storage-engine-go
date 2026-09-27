// Package dashboard serves CacheNova's live web console: real-time stats, a key
// browser, and a command runner — the visual proof that the cache works.
package dashboard

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/portfolio/cachenova/internal/store"
)

// Handler builds the HTTP mux for the dashboard and its JSON API.
func Handler(s *store.Store, staticDir string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.Stats())
	})

	mux.HandleFunc("/api/keys", func(w http.ResponseWriter, r *http.Request) {
		snap := s.Snapshot()
		type kv struct {
			Key   string `json:"key"`
			Value string `json:"value"`
			TTL   int64  `json:"ttl"`
		}
		out := make([]kv, 0, len(snap))
		for k, v := range snap {
			out = append(out, kv{Key: k, Value: string(v), TTL: s.TTL(k)})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
		writeJSON(w, out)
	})

	mux.Handle("/", http.FileServer(http.Dir(staticDir)))
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
