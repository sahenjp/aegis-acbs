package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sahenjp/aegis-acbs/internal/bench"
	"github.com/sahenjp/aegis-acbs/internal/graph"
	"github.com/sahenjp/aegis-acbs/internal/i18n"
	"github.com/sahenjp/aegis-acbs/internal/search"
	"github.com/sahenjp/aegis-acbs/internal/version"
)

//go:embed web/*
var webFS embed.FS

type App struct{ Graph *graph.Graph }

func (a App) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version.Version})
	})
	mux.HandleFunc("GET /api/meta", a.meta)
	mux.HandleFunc("GET /api/i18n/{lang}", a.catalog)
	mux.HandleFunc("POST /api/route", a.route)
	mux.HandleFunc("POST /api/benchmark", a.benchmark)
	return securityHeaders(mux)
}

func (a App) meta(w http.ResponseWriter, r *http.Request) {
	if a.Graph == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no graph"})
		return
	}
	minLat, minLon, maxLat, maxLon := a.Graph.BoundingBox()
	sourceID, targetID := suggestedPair(a.Graph)
	writeJSON(w, http.StatusOK, map[string]any{"version": version.Version, "name": a.Graph.Name, "source": a.Graph.Source, "nodes": len(a.Graph.Nodes), "edges": a.Graph.EdgeCount, "profile": a.Graph.Profile, "metric": a.Graph.Metric, "directed": a.Graph.Directed, "bbox": []float64{minLat, minLon, maxLat, maxLon}, "languages": i18n.Supported(), "suggestedSourceId": sourceID, "suggestedTargetId": targetID, "averageDegree": a.Graph.AverageDegree, "heuristicStrength": a.Graph.HeuristicStrength, "diameterMeters": a.Graph.DiameterMeters})
}

func suggestedPair(g *graph.Graph) (int64, int64) {
	start := -1
	for i := range g.Nodes {
		if g.OutDegree(i) > 0 {
			start = i
			break
		}
	}
	if start < 0 {
		return g.Nodes[0].ID, g.Nodes[0].ID
	}
	cur := start
	seen := map[int]bool{cur: true}
	for step := 0; step < 32; step++ {
		next := -1
		for _, e := range g.OutEdges(cur) {
			if !seen[e.To] {
				next = e.To
				break
			}
		}
		if next < 0 {
			break
		}
		cur = next
		seen[cur] = true
	}
	return g.Nodes[start].ID, g.Nodes[cur].ID
}

func (a App) catalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, i18n.Catalog(i18n.Normalize(r.PathValue("lang"))))
}

type routeRequest struct {
	Source    string           `json:"source"`
	Target    string           `json:"target"`
	Algorithm search.Algorithm `json:"algorithm"`
	TimeoutMS int              `json:"timeoutMs"`
}

func (a App) route(w http.ResponseWriter, r *http.Request) {
	if a.Graph == nil {
		writeError(w, http.StatusServiceUnavailable, "no graph loaded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req routeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	s, err := resolveNode(a.Graph, req.Source)
	if err != nil {
		writeError(w, http.StatusBadRequest, "source: "+err.Error())
		return
	}
	t, err := resolveNode(a.Graph, req.Target)
	if err != nil {
		writeError(w, http.StatusBadRequest, "target: "+err.Error())
		return
	}
	if req.Algorithm == "" {
		req.Algorithm = search.Aegis
	}
	timeout := 30 * time.Second
	if req.TimeoutMS > 0 && req.TimeoutMS <= 300000 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	result, err := search.Run(ctx, a.Graph, s, t, req.Algorithm)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	coords := make([][2]float64, 0, len(result.Path))
	ids := make([]int64, 0, len(result.Path))
	for _, idx := range result.Path {
		n := a.Graph.Nodes[idx]
		coords = append(coords, [2]float64{n.Lat, n.Lon})
		ids = append(ids, n.ID)
	}
	response := map[string]any{"stats": result.Stats, "pathNodeIds": ids, "coordinates": coords, "sourceIndex": s, "targetIndex": t}
	writeJSON(w, http.StatusOK, response)
}

type benchmarkRequest struct {
	Queries    int                `json:"queries"`
	Repeats    int                `json:"repeats"`
	Seed       uint64             `json:"seed"`
	Algorithms []search.Algorithm `json:"algorithms"`
}

func (a App) benchmark(w http.ResponseWriter, r *http.Request) {
	if a.Graph == nil {
		writeError(w, http.StatusServiceUnavailable, "no graph loaded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req benchmarkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Queries <= 0 {
		req.Queries = 20
	}
	if req.Queries > 1000 {
		writeError(w, http.StatusBadRequest, "queries must be <= 1000")
		return
	}
	if req.Repeats <= 0 {
		req.Repeats = 7
	}
	if req.Repeats > 31 {
		writeError(w, http.StatusBadRequest, "repeats must be <= 31")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	report, err := bench.Run(ctx, a.Graph, bench.Config{Queries: req.Queries, Repeats: req.Repeats, Warmup: 3, Seed: req.Seed, Algorithms: req.Algorithms, Timeout: 30 * time.Second, PairMode: "strongly-connected", Suite: "mixed"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func resolveNode(g *graph.Graph, s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return -1, errors.New("empty value")
	}
	if strings.Contains(s, ",") {
		p := strings.Split(s, ",")
		if len(p) != 2 {
			return -1, errors.New("expected latitude,longitude")
		}
		lat, e1 := strconv.ParseFloat(strings.TrimSpace(p[0]), 64)
		lon, e2 := strconv.ParseFloat(strings.TrimSpace(p[1]), 64)
		if e1 != nil || e2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return -1, errors.New("invalid coordinates")
		}
		idx, _ := g.Nearest(lat, lon)
		return idx, nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return -1, errors.New("expected node ID or latitude,longitude")
	}
	if idx, ok := g.IndexByID(id); ok {
		return idx, nil
	}
	return -1, fmt.Errorf("node ID %d not found", id)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
