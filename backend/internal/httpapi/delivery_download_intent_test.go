package httpapi

import (
	"context"
	"encoding/base64"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/delivery"
	"strings"
	"testing"
	"time"
)

type downloadIntentCreatorStub struct {
	cancel  context.CancelFunc
	calls   int
	invalid string
}

func TestDeliveryDownloadIntentNullDisguiseNeverReachesCreator(t *testing.T) {
	for _, body := range []string{`{"kind":"manifest","ordinal":null}`, `{"kind":"manifest","preset_id":null}`, `{"kind":"slot","ordinal":null,"preset_id":"12345678-1234-4234-8234-123456789abc"}`, `{"kind":"manifest","kind":"manifest"}`} {
		stub := &downloadIntentCreatorStub{}
		router := chi.NewRouter()
		router.Post("/{id}", func(w http.ResponseWriter, r *http.Request) {
			serveDeliveryDownloadIntent(w, r, auth.Identity{}, uuid.New(), stub)
		})
		request := newJSONTestRequest(http.MethodPost, "/"+uuid.NewString(), strings.NewReader(body))
		request.Header.Set("Idempotency-Key", "closed-wire-request")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		expectedStatus := 422
		if body == `{"kind":"manifest","kind":"manifest"}` {
			expectedStatus = 400
		}
		if recorder.Code != expectedStatus || stub.calls != 0 {
			t.Fatal(body, recorder.Code, stub.calls)
		}
	}
}

func (s *downloadIntentCreatorStub) Create(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, delivery.DownloadIntentRequest) (delivery.DownloadIntent, error) {
	s.calls++
	if s.cancel != nil {
		s.cancel()
	}
	intent := delivery.DownloadIntent{Token: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), ExpiresAt: time.Now().Add(45 * time.Second), Replayed: true}
	switch s.invalid {
	case "token":
		intent.Token = "secret-bearer"
	case "expired":
		intent.ExpiresAt = time.Now().Add(-time.Second)
	case "future":
		intent.ExpiresAt = time.Now().Add(2 * time.Minute)
	case "padded":
		intent.Token += "="
	}
	return intent, nil
}
func TestDeliveryDownloadIntentCancellationAndKeyAdmission(t *testing.T) {
	for _, mode := range []string{"success", "cancelled", "invalid-key", "zero-id", "invalid-selection"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stub := &downloadIntentCreatorStub{}
			if mode == "cancelled" {
				stub.cancel = cancel
			}
			id := uuid.New()
			if mode == "zero-id" {
				id = uuid.Nil
			}
			body := `{"kind":"manifest"}`
			if mode == "invalid-selection" {
				body = `{"kind":"slot","ordinal":0}`
			}
			request := newJSONTestRequest("POST", "/"+id.String(), strings.NewReader(body)).WithContext(ctx)
			key := "download-1"
			if mode == "invalid-key" {
				key = "bad key"
			}
			request.Header.Set("Idempotency-Key", key)
			router := chi.NewRouter()
			router.Post("/{id}", func(w http.ResponseWriter, r *http.Request) {
				serveDeliveryDownloadIntent(w, r, auth.Identity{}, uuid.New(), stub)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if mode == "cancelled" {
				if recorder.Body.Len() != 0 || recorder.Header().Get("Idempotency-Replayed") != "" {
					t.Fatal("cancelled bearer response emitted")
				}
				return
			}
			if mode == "success" {
				if recorder.Code != 201 || recorder.Header().Get("Cache-Control") != "private, no-store" || recorder.Header().Get("Idempotency-Replayed") != "true" {
					t.Fatal("successful response contract")
				}
				return
			}
			if recorder.Code != 422 || stub.calls != 0 {
				t.Fatal("invalid input reached creator", recorder.Code, stub.calls)
			}
		})
	}
}

func TestDeliveryDownloadIntentRejectsMalformedServiceReceipt(t *testing.T) {
	for _, mode := range []string{"token", "expired", "padded", "future"} {
		t.Run(mode, func(t *testing.T) {
			stub := &downloadIntentCreatorStub{invalid: mode}
			request := newJSONTestRequest("POST", "/"+uuid.NewString(), strings.NewReader(`{"kind":"manifest"}`))
			request.Header.Set("Idempotency-Key", "download-receipt")
			router := chi.NewRouter()
			router.Post("/{id}", func(w http.ResponseWriter, r *http.Request) {
				serveDeliveryDownloadIntent(w, r, auth.Identity{}, uuid.New(), stub)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != 503 || recorder.Header().Get("Idempotency-Replayed") != "" || strings.Contains(recorder.Body.String(), "intent_token") {
				t.Fatal("malformed receipt disclosed", recorder.Code, recorder.Body.String())
			}
		})
	}
}
