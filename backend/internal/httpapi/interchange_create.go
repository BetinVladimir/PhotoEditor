package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/interchange"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

type InterchangeCreator interface {
	CreateInterchange(context.Context, auth.Identity, uuid.UUID, interchange.CreateRequest, string) (idempotency.Response, error)
}

type InterchangeCreateService struct {
	Service      interchange.Service
	Dependencies interchange.ParseDependencies
	Export interchange.ExportCreatePreparer
}

func (s InterchangeCreateService) CreateInterchange(ctx context.Context, who auth.Identity, tid uuid.UUID, request interchange.CreateRequest, key string) (idempotency.Response, error) {
	if request.Direction == "export" {
		return s.Service.CreateExport(ctx, who, tid, request, key, s.Export)
	}
	return s.Service.CreateParse(ctx, who, tid, request, key, s.Dependencies)
}

func serveCreateInterchange(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, creator InterchangeCreator) {
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var request interchange.CreateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, r, err)
		return
	}
	intent, err := interchange.FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if creator == nil {
		writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
		return
	}
	result, err := creator.CreateInterchange(r.Context(), who, tid, intent.Request, key)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.Context().Err()!=nil{return}
	var ticket v2input.JobTicket
	decoder := json.NewDecoder(bytes.NewReader(result.Body))
	decoder.DisallowUnknownFields()
	identity := intent.Identity
	if result.Status != 202 || result.ResourceID == nil || *result.ResourceID != identity.Workflow || len(result.Body) > 4096 || decoder.Decode(&ticket) != nil || decoder.Decode(new(any)) != io.EOF || ticket.RunID != identity.Run || ticket.WorkflowID != identity.Workflow || ticket.State != "queued" {
		writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, http.StatusAccepted, ticket)
}
