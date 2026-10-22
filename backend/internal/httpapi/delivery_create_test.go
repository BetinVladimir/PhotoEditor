package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

type deliveryCreateStub struct {
	calls int
	mode  string
}

func (s *deliveryCreateStub) CreateDelivery(_ context.Context, who auth.Identity, tid uuid.UUID, request delivery.CreateRequest, key string) (idempotency.Response, error) {
	s.calls++
	i, err := delivery.FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return idempotency.Response{}, err
	}
	id := i.Identity()
	ticket := v2input.JobTicket{RunID: id.Run, WorkflowID: id.Workflow, State: "queued"}
	if s.mode == "wrong-ticket" {
		ticket.RunID = uuid.New()
	}
	body, _ := json.Marshal(ticket)
	return idempotency.Response{Status: 202, Body: body, ResourceID: &id.Package, Replayed: s.mode == "replay"}, nil
}

func TestCreateDeliveryHTTPValidationAndTicket(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tid := uuid.New()
	request := delivery.CreateRequest{ProjectID: uuid.New(), SnapshotID: uuid.New(), Name: "Delivery", Policy: delivery.Policy{RequiredPresets: []uuid.UUID{uuid.New()}, OptionalPresets: []uuid.UUID{}, MetadataPolicy: "strip", Provenance: "off", RequireReview: true}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "replay", "wrong-ticket", "missing-key", "unknown", "duplicate", "missing-policy"} {
		t.Run(mode, func(t *testing.T) {
			payload := string(body)
			key := "delivery-key"
			expected := 202
			switch mode {
			case "wrong-ticket":
				expected = 503
			case "missing-key":
				key = ""
				expected = 422
			case "unknown":
				payload = payload[:len(payload)-1] + `,"extra":true}`
				expected = 422
			case "duplicate":
				payload = payload[:len(payload)-1] + `,"name":"other"}`
				expected = 400
			case "missing-policy":
				payload = `{"project_id":"` + request.ProjectID.String() + `","snapshot_id":"` + request.SnapshotID.String() + `","name":"Delivery"}`
				expected = 422
			}
			stub := &deliveryCreateStub{mode: mode}
			r := newJSONTestRequest("POST", "/", strings.NewReader(payload))
			r.Header.Set("Idempotency-Key", key)
			w := httptest.NewRecorder()
			serveCreateDelivery(w, r, who, tid, stub)
			if w.Code != expected {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if expected == 422 && stub.calls != 0 {
				t.Fatal("invalid request reached service")
			}
			if mode == "replay" && w.Header().Get("Idempotency-Replayed") != "true" {
				t.Fatal("missing replay marker")
			}
		})
	}
}

func TestCreateDeliveryRouteRequiresSession(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("delivery-create")}}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, newJSONTestRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/delivery-packages", strings.NewReader(`{}`)))
	if w.Code != 401 {
		t.Fatalf("status=%d", w.Code)
	}
}
