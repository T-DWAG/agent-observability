package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/evaluation"
	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func TestCreateAndListEvaluations(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "tr-e", TenantID: "default", UserInput: "hi", AgentOutput: "hello",
		StartTime: now, Status: model.SpanStatusSuccess,
	})

	srv := NewServer(store).WithJudge(evaluation.NewJudge(store, evaluation.FakeCompleter{}))
	keys := testAPIKeys()

	body, _ := json.Marshal(map[string]string{"trace_id": "tr-e"})
	req := withTestAuth(httptest.NewRequest(http.MethodPost, "/api/v1/evaluations", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	srv.Handler(keys).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("post status=%d body=%s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req2 := withTestAuth(httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/tr-e", nil))
		rec2 := httptest.NewRecorder()
		srv.Handler(keys).ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("get status=%d body=%s", rec2.Code, rec2.Body.String())
		}
		var response struct {
			Status   string              `json:"status"`
			ErrorMsg string              `json:"error_msg"`
			Items    []*model.Evaluation `json:"items"`
			Total    int                 `json:"total"`
		}
		if err := json.Unmarshal(rec2.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Status == model.EvalStatusDone {
			if response.ErrorMsg != "" || response.Total != 3 || len(response.Items) != 3 {
				t.Fatalf("response=%+v", response)
			}
			for _, item := range response.Items {
				if item.Dimension == "overall" {
					t.Fatal("overall task row must not appear in items")
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("evaluation did not reach done")
}

func TestCreateEvaluation_DuplicateReturnsConflict(t *testing.T) {
	store := storage.NewMemoryStorage()
	_ = store.SaveTrace(context.Background(), &model.Trace{
		TraceID: "tr-dup", TenantID: "default",
		StartTime: time.Now().UTC(), Status: model.SpanStatusSuccess,
	})
	srv := NewServer(store).WithJudge(evaluation.NewJudge(store, evaluation.FakeCompleter{}))
	keys := testAPIKeys()
	body, _ := json.Marshal(map[string]string{"trace_id": "tr-dup"})

	first := withTestAuth(httptest.NewRequest(http.MethodPost, "/api/v1/evaluations", bytes.NewReader(body)))
	firstRec := httptest.NewRecorder()
	srv.Handler(keys).ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusAccepted {
		t.Fatalf("first status=%d", firstRec.Code)
	}

	second := withTestAuth(httptest.NewRequest(http.MethodPost, "/api/v1/evaluations", bytes.NewReader(body)))
	secondRec := httptest.NewRecorder()
	srv.Handler(keys).ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusConflict {
		t.Fatalf("second status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
}
