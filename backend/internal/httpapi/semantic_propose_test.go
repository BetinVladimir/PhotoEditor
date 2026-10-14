package httpapi

import (
	"github.com/google/uuid"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"testing"
)

func TestSemanticProposeRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("semantic-propose-test")}}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/semantic-intents", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestSemanticProposeResponseRequiresDurableRunReceipt(t *testing.T) {
	run := uuid.New()
	body, _ := canonical.Marshal(semanticProposeTicket{run, uuid.New(), "queued"})
	response := idempotency.Response{Status: 202, Body: body, ResourceID: &run}
	if _, err := semanticProposeResponse(response); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"status", "resource", "state", "extra"} {
		bad := response
		switch kind {
		case "status":
			bad.Status = 200
		case "resource":
			other := uuid.New()
			bad.ResourceID = &other
		case "state":
			bad.Body = []byte(`{"run_id":"` + run.String() + `","workflow_id":"` + uuid.NewString() + `","state":"completed"}`)
		case "extra":
			bad.Body = append(append([]byte(nil), body[:len(body)-1]...), []byte(`,"extra":true}`)...)
		}
		if _, err := semanticProposeResponse(bad); err == nil {
			t.Fatal(kind)
		}
	}
}
