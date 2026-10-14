package httpapi

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/semanticreview"
	"testing"
)

func TestSemanticRunIntentRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("semantic-run-route")}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/runs/"+uuid.NewString()+"/semantic-intent", nil)
	s.Routes().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated parent resolution accepted", w.Code)
	}
}

func TestSemanticApplyRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("semantic-review-route")}}
	handler := s.Routes()
	found := false
	if err := chi.Walk(handler.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "POST" && path == "/api/v1/tenants/{t}/semantic-intents/{id}/apply" {
			found = true
		}
		return nil
	}); err != nil || !found {
		t.Fatal("missing semantic apply route", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/semantic-intents/"+uuid.NewString()+"/apply", nil)
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated apply accepted", w.Code)
	}
}

func TestSemanticApplyResponseRequiresExactReceipt(t *testing.T) {
	batch := uuid.New()
	body, _ := canonical.Marshal(semanticreview.BatchResult{WorkflowID: uuid.New(), Revision: "2", Remaining: 1, BatchID: batch, Child: &semanticreview.ChildTicket{RunID: uuid.New(), State: "queued"}})
	response := idempotency.Response{Status: 200, Body: body, ResourceID: &batch}
	if result, err := semanticReviewResponse(response); err != nil || result.BatchID != batch {
		t.Fatal(err)
	}
	for _, kind := range []string{"status", "resource", "missing", "body"} {
		bad := response
		switch kind {
		case "status":
			bad.Status = 202
		case "resource":
			id := uuid.New()
			bad.ResourceID = &id
		case "missing":
			bad.ResourceID = nil
		case "body":
			bad.Body = []byte(`{"child":null}`)
		}
		if _, err := semanticReviewResponse(bad); err == nil {
			t.Fatal("invalid receipt accepted", kind)
		}
	}
}

func TestSemanticCloseRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("semantic-review-route")}}
	handler := s.Routes()
	found := false
	if err := chi.Walk(handler.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "POST" && path == "/api/v1/tenants/{t}/semantic-intents/{id}/close-review" {
			found = true
		}
		return nil
	}); err != nil || !found {
		t.Fatal("missing semantic close route", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/semantic-intents/"+uuid.NewString()+"/close-review", nil)
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated close accepted", w.Code)
	}
}

func TestSemanticCloseResponseRejectsIncompleteJob(t *testing.T) {
	batch := uuid.New()
	body, _ := canonical.Marshal(semanticreview.CloseResult{RunID: uuid.New(), WorkflowID: uuid.New(), Revision: "2", State: "completed", Outputs: []json.RawMessage{}, ErrorCode: nil})
	response := idempotency.Response{Status: 200, Body: body, ResourceID: &batch}
	if _, err := semanticCloseResponse(response); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"status", "missing", "nil-id", "body"} {
		bad := response
		switch kind {
		case "status":
			bad.Status = 202
		case "missing":
			bad.ResourceID = nil
		case "nil-id":
			id := uuid.Nil
			bad.ResourceID = &id
		case "body":
			bad.Body = []byte(`{"state":"waiting_review"}`)
		}
		if _, err := semanticCloseResponse(bad); err == nil {
			t.Fatal("invalid closure accepted", kind)
		}
	}
}
