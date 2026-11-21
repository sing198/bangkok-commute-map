package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestPresetsEndpointAndTimingHeader(t *testing.T) {
	router := NewRouter(nil, nil, nil, "http://localhost:5173,https://demo.vercel.app")

	req := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	req.Header.Set("Origin", "https://demo.vercel.app")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if rec.Header().Get("X-Query-Time-Ms") == "" {
		t.Errorf("expected X-Query-Time-Ms header to be populated")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://demo.vercel.app" {
		t.Errorf("expected matched CORS origin, got %q", got)
	}

	var body struct {
		Presets []OfficePreset `json:"presets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal presets: %v", err)
	}
	if len(body.Presets) < 6 {
		t.Errorf("expected at least 6 Bangkok office presets, got %d", len(body.Presets))
	}
}

func TestHealthEndpointDegradesGracefullyWhenPoolNil(t *testing.T) {
	router := NewRouter(nil, nil, nil, "*")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when PostGIS pool is nil, got %d", rec.Code)
	}
}

func TestSearchAndCompareValidation(t *testing.T) {
	router := NewRouter(nil, nil, nil, "*")

	// Without commuteSvc initialized, returns 503 cleanly.
	req := httptest.NewRequest(http.MethodGet, "/api/commute/search?lat=13.7367&lng=100.5603", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when commute service is nil, got %d", rec.Code)
	}

	// Compare returns 503 when service is nil.
	payload := bytes.NewBufferString(`{"office":{"lat":13.7367,"lng":100.5603},"listing_ids":["LST_001","LST_002"]}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/commute/compare", payload)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when commute service is nil, got %d", rec2.Code)
	}
}
