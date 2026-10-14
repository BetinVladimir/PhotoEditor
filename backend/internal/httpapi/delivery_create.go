package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

type DeliveryCreator interface {
	CreateDelivery(context.Context, auth.Identity, uuid.UUID, delivery.CreateRequest, string) (idempotency.Response, error)
}

type DeliveryCreateService struct {
	Service      delivery.Service
	Dependencies delivery.CreateDeliveryDependencies
}

func (s DeliveryCreateService) CreateDelivery(ctx context.Context, who auth.Identity, tid uuid.UUID, request delivery.CreateRequest, key string) (idempotency.Response, error) {
	return s.Service.CreateDelivery(ctx, who, tid, request, key, s.Dependencies)
}

func serveCreateDelivery(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, creator DeliveryCreator) {
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var request delivery.CreateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, r, err)
		return
	}
	intent, err := delivery.FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if creator == nil {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	result, err := creator.CreateDelivery(r.Context(), who, tid, intent.Request(), key)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var ticket v2input.JobTicket
	decoder := json.NewDecoder(bytes.NewReader(result.Body))
	decoder.DisallowUnknownFields()
	identity := intent.Identity()
	if result.Status != 202 || result.ResourceID == nil || *result.ResourceID != identity.Package || len(result.Body) > 4096 || decoder.Decode(&ticket) != nil || decoder.Decode(new(any)) != io.EOF || ticket.RunID != identity.Run || ticket.WorkflowID != identity.Workflow || ticket.State != "queued" {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, http.StatusAccepted, ticket)
}
