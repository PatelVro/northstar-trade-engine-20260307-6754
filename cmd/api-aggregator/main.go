// Command api-aggregator combines multiple northstar API backends into a
// single HTTP surface. The Cirelay dashboard talks to one port; this
// aggregator fans out to the per-trader backends and merges responses.
//
// Why: `default_coins` in config is a global field, so crypto traders and
// equity traders can't coexist in the same process. We run two (or more)
// northstar instances on different ports, each with a specialized config.
// The aggregator hides that topology from the frontend.
//
// Routing rules:
//   /api/traders            → union of `traders` from every backend
//   /api/competition        → union; merged trader summaries
//   /api/*?trader_id=X      → routed to the backend owning X
//   /api/* (no trader_id)   → primary backend (first configured)
//   /health, /readiness     → primary backend
//
// The trader→backend mapping is cached at startup and refreshed every 30s.
// If a backend is down at startup, the aggregator still serves known
// backends; the down backend just disappears from responses.
//
// Usage:
//   go run ./cmd/api-aggregator -port 8082 \
//       -backends http://localhost:8080,http://localhost:8081
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

type aggregator struct {
	backends        []*backend
	mu              sync.RWMutex
	traderToBackend map[string]*backend // cached trader_id → backend
}

type backend struct {
	name string
	url  *url.URL
	// Reverse proxy is created once and reused; it handles connection pooling.
	rp *httputil.ReverseProxy
}

func main() {
	port := flag.Int("port", 8082, "port to listen on")
	backendList := flag.String("backends", "http://localhost:8080,http://localhost:8081",
		"comma-separated backend URLs, first is primary")
	refreshSecs := flag.Int("refresh-secs", 30, "seconds between trader map refreshes")
	flag.Parse()

	urls := strings.Split(*backendList, ",")
	if len(urls) == 0 {
		log.Fatal("at least one backend URL required")
	}

	agg := &aggregator{
		traderToBackend: map[string]*backend{},
	}
	for i, raw := range urls {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			log.Fatalf("backend %q: %v", raw, err)
		}
		b := &backend{
			name: fmt.Sprintf("backend%d@%s", i, u.Host),
			url:  u,
			rp:   httputil.NewSingleHostReverseProxy(u),
		}
		// Don't error out the whole proxy when a backend is unhealthy —
		// log and fall back to "upstream unavailable".
		b.rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy error to %s: %v", b.name, err)
			http.Error(w, fmt.Sprintf("backend %s unreachable: %v", b.name, err), http.StatusBadGateway)
		}
		agg.backends = append(agg.backends, b)
	}

	// Initial refresh + background refresher
	agg.refreshTraderMap()
	go func() {
		t := time.NewTicker(time.Duration(*refreshSecs) * time.Second)
		defer t.Stop()
		for range t.C {
			agg.refreshTraderMap()
		}
	}()

	// HTTP handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/api/traders", agg.handleTraders)
	mux.HandleFunc("/api/competition", agg.handleCompetition)
	mux.HandleFunc("/api/", agg.handleRouted)
	mux.HandleFunc("/health", agg.proxyToPrimary)
	mux.HandleFunc("/readiness", agg.proxyToPrimary)
	mux.HandleFunc("/", agg.proxyToPrimary) // any other path

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("api-aggregator listening on %s, fan-out over %d backend(s):", addr, len(agg.backends))
	for _, b := range agg.backends {
		log.Printf("  %s -> %s", b.name, b.url)
	}
	log.Fatal(http.ListenAndServe(addr, mux))
}

// refreshTraderMap queries each backend's /api/traders and caches which
// backend owns each trader_id. Failed backends are skipped silently.
func (a *aggregator) refreshTraderMap() {
	newMap := map[string]*backend{}
	client := &http.Client{Timeout: 3 * time.Second}
	for _, b := range a.backends {
		u := b.url.String() + "/api/traders"
		resp, err := client.Get(u)
		if err != nil {
			log.Printf("refresh: %s unreachable: %v", b.name, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("refresh: %s /api/traders returned %d", b.name, resp.StatusCode)
			continue
		}
		var traders []map[string]interface{}
		if err := json.Unmarshal(body, &traders); err != nil {
			log.Printf("refresh: %s decode failed: %v", b.name, err)
			continue
		}
		for _, t := range traders {
			if id, ok := t["trader_id"].(string); ok && id != "" {
				newMap[id] = b
			}
		}
	}
	a.mu.Lock()
	a.traderToBackend = newMap
	a.mu.Unlock()
	if len(newMap) > 0 {
		log.Printf("refresh: mapped %d trader(s) across %d backend(s)", len(newMap), len(a.backends))
	}
}

// handleTraders returns the UNION of traders across all backends.
func (a *aggregator) handleTraders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client := &http.Client{Timeout: 3 * time.Second}
	var merged []map[string]interface{}
	for _, b := range a.backends {
		resp, err := client.Get(b.url.String() + "/api/traders")
		if err != nil {
			continue // drop dead backends silently
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		var arr []map[string]interface{}
		if err := json.Unmarshal(body, &arr); err != nil {
			continue
		}
		merged = append(merged, arr...)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(merged)
}

// handleCompetition fans out competition summaries, merging trader entries.
// The shape returned by /api/competition is a single object; we shallow-merge
// the "traders" list and sum equity/pnl figures.
func (a *aggregator) handleCompetition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client := &http.Client{Timeout: 3 * time.Second}
	// competition payload shape is backend-specific — we return the first
	// non-empty response as the "base" and splice additional `traders` arrays.
	var base map[string]interface{}
	for _, b := range a.backends {
		resp, err := client.Get(b.url.String() + "/api/competition")
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			continue
		}
		if base == nil {
			base = payload
			continue
		}
		if others, ok := payload["traders"].([]interface{}); ok {
			if existing, ok := base["traders"].([]interface{}); ok {
				base["traders"] = append(existing, others...)
			} else {
				base["traders"] = others
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if base == nil {
		base = map[string]interface{}{"traders": []interface{}{}}
	}
	_ = json.NewEncoder(w).Encode(base)
}

// handleRouted routes /api/* requests to the backend that owns the
// trader_id query parameter. If no trader_id present or unknown, falls
// back to the primary backend.
func (a *aggregator) handleRouted(w http.ResponseWriter, r *http.Request) {
	traderID := r.URL.Query().Get("trader_id")
	b := a.pickBackend(traderID)
	b.rp.ServeHTTP(w, r)
}

// proxyToPrimary routes everything else (/health, /, …) to the first backend.
func (a *aggregator) proxyToPrimary(w http.ResponseWriter, r *http.Request) {
	a.backends[0].rp.ServeHTTP(w, r)
}

// pickBackend returns the backend owning trader_id, or the primary if unknown.
func (a *aggregator) pickBackend(traderID string) *backend {
	if traderID == "" {
		return a.backends[0]
	}
	a.mu.RLock()
	b, ok := a.traderToBackend[traderID]
	a.mu.RUnlock()
	if !ok {
		return a.backends[0]
	}
	return b
}
