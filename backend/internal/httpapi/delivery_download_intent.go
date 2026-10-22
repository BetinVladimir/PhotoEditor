package httpapi

import (
	"context"
	"encoding/base64"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"time"
)

type deliveryDownloadIntentCreator interface {
	Create(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, delivery.DownloadIntentRequest) (delivery.DownloadIntent, error)
}

func serveDeliveryDownloadIntent(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, creator deliveryDownloadIntentCreator) {
	if r.Context().Err() != nil {
		return
	}
	packageID, err := routeID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if packageID == uuid.Nil || !idempotency.ValidKey(r.Header.Get("Idempotency-Key")) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	var request delivery.DownloadIntentRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, r, err)
		return
	}
	if err := request.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	if creator == nil {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	intent, err := creator.Create(r.Context(), who, tid, packageID, r.Header.Get("Idempotency-Key"), request)
	// A late response must not disclose a bearer token to a cancelled request.
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	now := time.Now()
	token, tokenErr := base64.RawURLEncoding.Strict().DecodeString(intent.Token)
	if tokenErr != nil || len(token) != 32 || len(intent.Token) != 43 || !intent.ExpiresAt.After(now) || intent.ExpiresAt.After(now.Add(time.Minute)) {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if intent.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, http.StatusCreated, intent)
}
