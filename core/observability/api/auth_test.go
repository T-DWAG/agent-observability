package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func TestAuth_HealthzPublic(t *testing.T) {
	store := storage.NewMemoryStorage()
	h := NewServer(store).Handler(ParseAPIKeys("alice:tenant-a"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestAuth_Unauthorized(t *testing.T) {
	store := storage.NewMemoryStorage()
	h := NewServer(store).Handler(ParseAPIKeys("alice:tenant-a"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/traces", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestAuth_TenantIsolation(t *testing.T) {
	store := storage.NewMemoryStorage()
	_ = store.SaveTrace(context.Background(), &model.Trace{
		TraceID:   "tr-a",
		TenantID:  "tenant-a",
		SessionID: "s1",
		StartTime: time.Now(),
		Status:    model.SpanStatusSuccess,
	})
	keys := ParseAPIKeys("alice:tenant-a,bob:tenant-b")
	h := NewServer(store).Handler(keys)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/tr-a", nil)
	req.Header.Set("Authorization", "Bearer alice")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("alice status=%d body=%s", rr.Code, rr.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/traces/tr-a", nil)
	req2.Header.Set("Authorization", "Bearer bob")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("bob status=%d", rr2.Code)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/traces", nil)
	req3.Header.Set("Authorization", "Bearer alice")
	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, req3)
	var list listTracesResponse
	if err := json.NewDecoder(rr3.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Items[0].TraceID != "tr-a" {
		t.Fatalf("list=%+v", list)
	}
}
