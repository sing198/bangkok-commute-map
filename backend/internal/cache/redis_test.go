package cache

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUpstashRestClient(t *testing.T) {
	var (
		mu    sync.Mutex
		store = map[string]string{}
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var cmd []any
		if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil || len(cmd) == 0 {
			http.Error(w, `{"error":"bad command"}`, http.StatusBadRequest)
			return
		}
		op, _ := cmd[0].(string)
		mu.Lock()
		defer mu.Unlock()

		switch strings.ToUpper(op) {
		case "PING":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "PONG"})
		case "SET":
			k, _ := cmd[1].(string)
			v, _ := cmd[2].(string)
			store[k] = v
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "OK"})
		case "GET":
			k, _ := cmd[1].(string)
			v, ok := store[k]
			if !ok {
				_ = json.NewEncoder(w).Encode(map[string]any{"result": nil})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": v})
		case "SCAN":
			pattern, _ := cmd[3].(string)
			prefix := strings.TrimSuffix(pattern, "*")
			keys := make([]string, 0)
			for k := range store {
				if strings.HasPrefix(k, prefix) {
					keys = append(keys, k)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []any{"0", keys}})
		case "DEL":
			var deleted int64
			for _, raw := range cmd[1:] {
				k, _ := raw.(string)
				if _, ok := store[k]; ok {
					delete(store, k)
					deleted++
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": deleted})
		default:
			http.Error(w, `{"error":"unsupported"}`, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := NewFromOptions(Options{
		RestURL:   srv.URL,
		RestToken: "test-token",
	})
	if client.Mode() != "upstash-rest" {
		t.Fatalf("expected mode upstash-rest, got %s", client.Mode())
	}

	ctx := context.Background()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	if _, ok := client.Get(ctx, "commute:missing"); ok {
		t.Fatalf("expected cache miss")
	}

	client.Set(ctx, "commute:v1:asok", `{"ok":true}`, 15*time.Minute)
	got, ok := client.Get(ctx, "commute:v1:asok")
	if !ok || got != `{"ok":true}` {
		t.Fatalf("expected cache hit with payload, got ok=%v val=%q", ok, got)
	}

	deleted, err := client.DeleteByPrefix(ctx, "commute:")
	if err != nil || deleted != 1 {
		t.Fatalf("expected 1 deleted key, got deleted=%d err=%v", deleted, err)
	}
	if _, ok := client.Get(ctx, "commute:v1:asok"); ok {
		t.Fatalf("expected key to be deleted")
	}
}
