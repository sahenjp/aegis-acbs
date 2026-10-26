package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sahenjp/aegis-acbs/internal/graph"
)

func testGraph() *graph.Graph {
	g := graph.New("t", "", "car", graph.MetricDistance)
	g.Nodes = []graph.Node{{ID: 1, Lat: 35, Lon: 139}, {ID: 2, Lat: 35.001, Lon: 139}}
	g.Adj = [][]graph.Edge{{{To: 1, Cost: 111000}}, {{To: 0, Cost: 111000}}}
	_ = g.Finalize()
	return g
}
func TestHTTPAPI(t *testing.T) {
	h := App{Graph: testGraph()}.Handler()
	for _, path := range []string{"/", "/healthz", "/api/meta", "/api/i18n/fr"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	body := bytes.NewBufferString(`{"source":"1","target":"2","algorithm":"aegis"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/route", body)
	r.Header.Set("content-type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("route: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Stats struct {
			Algorithm string `json:"algorithm"`
			Reachable bool   `json:"reachable"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Stats.Algorithm != "aegis" || !response.Stats.Reachable {
		t.Fatalf("unexpected ACBS response: %+v", response.Stats)
	}
}
