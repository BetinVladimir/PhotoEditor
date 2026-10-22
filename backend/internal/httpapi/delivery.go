package httpapi

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

type deliveryPackageReader interface {
	Get(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (json.RawMessage, error)
}

func (s *Server) deliveryRoutes(router chi.Router) {
	router.Get("/api/v1/tenants/{t}/delivery-packages", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		projectID, n, cursor, err := deliveryListQuery(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		service := delivery.Service{DB: s.DB, Cursors: pagination.Codec{Key: auth.MAC(s.Auth.Pepper, "cursor-key")}}
		page, err := service.List(r.Context(), who, tid, projectID, n, cursor)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if r.Context().Err() != nil {
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, page)
	})
	router.Get("/api/v1/tenants/{t}/delivery-packages/by-run/{run}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveDeliveryByRun(w, r, who, tid, delivery.Service{DB: s.DB})
	})
	router.Get("/api/v1/tenants/{t}/delivery-packages/by-workflow/{workflow}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveDeliveryByWorkflow(w, r, who, tid, delivery.Service{DB: s.DB})
	})
	router.Post("/api/v1/tenants/{t}/delivery-packages", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		creator := s.DeliveryCreator
		if creator == nil {
			creator = DeliveryCreateService{Service: delivery.Service{DB: s.DB}}
		}
		serveCreateDelivery(w, r, who, tid, creator)
	})
	router.Get("/api/v1/tenants/{t}/delivery-packages/{id}/inspection", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveDeliveryInspection(w, r, who, tid, delivery.Service{DB: s.DB})
	})

	router.Get("/api/v1/tenants/{t}/delivery-packages/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveDeliveryItems(w, r, who, tid, delivery.Service{DB: s.DB})
	})
	router.Post("/api/v1/tenants/{t}/delivery-packages/{id}/download-intent", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		bucket := ""
		if s.Media != nil {
			bucket = s.Media.Bucket
		}
		service := delivery.DownloadIntentService{DB: s.DB, Pepper: s.Auth.Pepper, Bucket: bucket}
		serveDeliveryDownloadIntent(w, r, who, tid, service)
	})

	router.Post("/api/v1/tenants/{t}/delivery-packages/{id}/publish", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		servePublishDelivery(w, r, who, tid, delivery.Service{DB: s.DB})
	})

	router.Get("/api/v1/tenants/{t}/delivery-packages/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveDeliveryPackage(w, r, who, tid, delivery.Service{DB: s.DB})
	})
}

func deliveryListQuery(r *http.Request) (uuid.UUID, int, string, error) {
	invalid := func() (uuid.UUID, int, string, error) {
		return uuid.Nil, 0, "", domain.NewError("VALIDATION_FAILED", 422)
	}
	if r == nil {
		return invalid()
	}
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		return invalid()
	}
	for key, values := range q {
		if key != "project_id" && key != "limit" && key != "cursor" || len(values) != 1 {
			return invalid()
		}
	}
	if len(q["project_id"]) != 1 {
		return invalid()
	}
	projectID, err := uuid.Parse(q.Get("project_id"))
	if err != nil || projectID == uuid.Nil {
		return invalid()
	}
	if values, ok := q["cursor"]; ok && (values[0] == "" || len(values[0]) > 4096) {
		return invalid()
	}
	n, err := limit(r)
	if err != nil {
		return uuid.Nil, 0, "", err
	}
	return projectID, n, q.Get("cursor"), nil
}

func serveDeliveryByRun(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, reader deliveryPackageReader) {
	runID, err := uuid.Parse(chi.URLParam(r, "run"))
	if err != nil || runID == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	workflowID, err := v2input.WorkflowID(runID)
	if err != nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	serveDerivedDelivery(w, r, who, tid, reader, workflowID)
}

// The create receipt intentionally contains only the run/workflow ticket.
// Resolve its deterministic package identity through the existing scoped Get
// authority so the client can reach publication after the workflow finishes.
func serveDeliveryByWorkflow(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, reader deliveryPackageReader) {
	workflowID, err := uuid.Parse(chi.URLParam(r, "workflow"))
	if err != nil || workflowID == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	serveDerivedDelivery(w, r, who, tid, reader, workflowID)
}

func serveDerivedDelivery(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, reader deliveryPackageReader, workflowID uuid.UUID) {
	if reader == nil {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	packageID := uuid.NewSHA1(workflowID, []byte("delivery:package:1"))
	resource, err := reader.Get(r.Context(), who, tid, packageID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if !delivery.ValidPackageResource(resource, tid, packageID) {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	var binding struct {
		Data struct {
			WorkflowID uuid.UUID `json:"workflow_id"`
		} `json:"data"`
	}
	if json.Unmarshal(resource, &binding) != nil || binding.Data.WorkflowID != workflowID {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, resource)
}

// Called only after session authentication; the service repeats tenant/project ACL.
func serveDeliveryPackage(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, reader deliveryPackageReader) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || id == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	if reader == nil {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	resource, err := reader.Get(r.Context(), who, tid, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if !delivery.ValidPackageResource(resource, tid, id) {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, resource)
}

type deliveryPackagePublisher interface {
	Publish(context.Context, auth.Identity, uuid.UUID, uuid.UUID, delivery.PublishRequest, string) (json.RawMessage, error)
}

func servePublishDelivery(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, publisher deliveryPackagePublisher) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || id == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	var request delivery.PublishRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, r, err)
		return
	}
	if _, err := delivery.ParsePublishRevision(request.ExpectedRevision); err != nil {
		writeError(w, r, err)
		return
	}
	if !idempotency.ValidKey(r.Header.Get("Idempotency-Key")) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	if publisher == nil {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	var resource json.RawMessage
	var replayed bool
	var response idempotency.Response
	if receipts, ok := publisher.(interface {
		PublishWithReceipt(context.Context, auth.Identity, uuid.UUID, uuid.UUID, delivery.PublishRequest, string) (idempotency.Response, error)
	}); ok {
		var receipt idempotency.Response
		receipt, err = receipts.PublishWithReceipt(r.Context(), who, tid, id, request, r.Header.Get("Idempotency-Key"))
		resource, replayed = receipt.Body, receipt.Replayed
		response = receipt
	} else {
		resource, err = publisher.Publish(r.Context(), who, tid, id, request, r.Header.Get("Idempotency-Key"))
		response = idempotency.Response{Status: 200, Body: resource, ResourceID: &id}
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if delivery.ValidatePublicationResponse(response, tid, id, request) != nil {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, http.StatusOK, resource)
}

type deliveryItemsReader interface {
	Items(context.Context, auth.Identity, uuid.UUID, uuid.UUID, delivery.ItemsQuery) (json.RawMessage, error)
}

func serveDeliveryItems(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, reader deliveryItemsReader) {
	invalid := func() { writeError(w, r, domain.NewError("VALIDATION_FAILED", 422)) }
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || id == uuid.Nil {
		invalid()
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		invalid()
		return
	}
	for name, value := range values {
		if len(value) != 1 || (name != "revision" && name != "after_ordinal" && name != "after_preset" && name != "limit") {
			invalid()
			return
		}
	}
	q := delivery.ItemsQuery{Revision: values.Get("revision"), AfterOrdinal: -1, Limit: 100}
	for name, dest := range map[string]*int{"after_ordinal": &q.AfterOrdinal, "limit": &q.Limit} {
		if raw, ok := values[name]; ok {
			n, err := strconv.Atoi(raw[0])
			if err != nil || strconv.Itoa(n) != raw[0] {
				invalid()
				return
			}
			*dest = n
		}
	}
	_, ordinalPresent := values["after_ordinal"]
	preset, presetPresent := values["after_preset"]
	if ordinalPresent != presetPresent {
		invalid()
		return
	}
	if presetPresent {
		q.AfterPreset, err = uuid.Parse(preset[0])
		if err != nil || q.AfterPreset == uuid.Nil {
			invalid()
			return
		}
	}
	if err := q.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	if reader == nil {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	result, err := reader.Items(r.Context(), who, tid, id, q)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if !delivery.ValidItemsResource(result, id, q) {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, result)
}

type deliveryInspector interface {
	Inspect(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string) (delivery.Inspection, error)
}

func serveDeliveryInspection(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, inspector deliveryInspector) {
	invalid := func() { writeError(w, r, domain.NewError("VALIDATION_FAILED", 422)) }
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || id == uuid.Nil {
		invalid()
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query) != 1 || len(query["revision"]) != 1 {
		invalid()
		return
	}
	revision := query.Get("revision")
	q := delivery.ItemsQuery{Revision: revision, AfterOrdinal: -1, Limit: 1}
	if err := q.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	if inspector == nil {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	result, err := inspector.Inspect(r.Context(), who, tid, id, revision)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if !delivery.ValidInspectionResource(result, id, revision) {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, result)
}
