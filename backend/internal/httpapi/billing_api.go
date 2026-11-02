package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/billing"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/legal"
	"photoeditor/backend/internal/privacy"
)

func serveBillingGetCatalog(w http.ResponseWriter, r *http.Request, svc billing.CatalogService) {
	if r.Context().Err() != nil {
		return
	}
	q := r.URL.Query()
	locale := q.Get("locale")
	currency := q.Get("currency")
	if locale == "" || currency == "" {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	switch locale {
	case "en", "ru", "bg":
	default:
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	if currency != "EUR" {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	cat, err := svc.GetCatalog(r.Context(), locale, currency)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, cat)
}

func serveBillingGetSummary(w http.ResponseWriter, r *http.Request, svc billing.BillingSummaryService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, tid, err := s.tenantRequest(r, false)
	if err != nil {
		writeError(w, r, err)
		return
	}
	summary, err := svc.GetSummary(r.Context(), who, tid)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, summary)
}

func serveBillingGetUsage(w http.ResponseWriter, r *http.Request, svc billing.BillingSummaryService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, tid, err := s.tenantRequest(r, false)
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	resourceCode := q.Get("resource_code")
	limit := 50
	if l := q.Get("limit"); l != "" {
		n, err2 := strconv.Atoi(l)
		if err2 != nil || n < 1 || n > 100 {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		limit = n
	}
	cursor := q.Get("cursor")
	usage, err := svc.GetUsage(r.Context(), who, tid, resourceCode, limit, cursor)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, usage)
}

func serveBillingGetBuckets(w http.ResponseWriter, r *http.Request, svc billing.BillingSummaryService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, tid, err := s.tenantRequest(r, false)
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	resourceCode := q.Get("resource_code")
	if resourceCode == "" {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	limit := 50
	if l := q.Get("limit"); l != "" {
		n, err2 := strconv.Atoi(l)
		if err2 != nil || n < 1 || n > 100 {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		limit = n
	}
	cursor := q.Get("cursor")
	page, err := svc.GetBuckets(r.Context(), who, tid, resourceCode, limit, cursor)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, page)
}

func serveBillingCreateQuote(w http.ResponseWriter, r *http.Request, svc billing.QuoteService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, tid, err := s.tenantRequest(r, true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	rev, err := revision(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req billing.QuoteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.Kind == "" {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	quote, err := svc.CreateQuote(r.Context(), who, tid, req, rev, key)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, quote)
}

func serveBillingCreateOrder(w http.ResponseWriter, r *http.Request, svc billing.OrderService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, tid, err := s.tenantRequest(r, true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	rev, err := revision(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req billing.OrderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.QuoteID == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	accepted, err := svc.CreateOrder(r.Context(), who, tid, req, rev, key)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 202, accepted)
}

func serveBillingGetOrder(w http.ResponseWriter, r *http.Request, svc billing.OrderService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, tid, err := s.tenantRequest(r, false)
	if err != nil {
		writeError(w, r, err)
		return
	}
	orderID, err := routeID(r, "order")
	if err != nil {
		writeError(w, r, err)
		return
	}
	order, err := svc.GetOrder(r.Context(), who, tid, orderID)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, order)
}

func serveBillingCreateChange(w http.ResponseWriter, r *http.Request, svc billing.ChangeService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, tid, err := s.tenantRequest(r, true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	rev, err := revision(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req billing.ChangeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.QuoteID == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	change, err := svc.CreateChange(r.Context(), who, tid, req, rev, key)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 202, change)
}

func serveBillingCancelChange(w http.ResponseWriter, r *http.Request, svc billing.ChangeService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, tid, err := s.tenantRequest(r, true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	rev, err := revision(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	changeID, err := routeID(r, "change")
	if err != nil {
		writeError(w, r, err)
		return
	}
	change, err := svc.CancelChange(r.Context(), who, tid, changeID, rev, key)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, change)
}

func serveBillingCreatePortalSession(w http.ResponseWriter, r *http.Request, svc billing.PortalService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, tid, err := s.tenantRequest(r, true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	var req billing.PortalRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.ReturnRoute != "billing" && req.ReturnRoute != "profile" {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	accepted, err := svc.CreatePortalSession(r.Context(), who, tid, req, key)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 202, accepted)
}

// serveStripeWebhook handles POST /api/v1/webhooks/stripe/{account}.
// No user session/CSRF: only raw body and Stripe-Signature header.
func serveStripeWebhook(w http.ResponseWriter, r *http.Request, svc billing.WebhookIngressService) {
	if r.Context().Err() != nil {
		return
	}
	accountID, err := routeID(r, "account")
	if err != nil {
		writeError(w, r, domain.NewError("NOT_FOUND", 404))
		return
	}
	sig := r.Header.Get("Stripe-Signature")
	if sig == "" {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 400))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 400))
		return
	}
	if len(body) == 0 {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 400))
		return
	}
	received, err := svc.ReceiveStripeWebhook(r.Context(), accountID, body, sig)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, received)
}

func serveLegalGetRequirements(w http.ResponseWriter, r *http.Request, svc legal.Service) {
	if r.Context().Err() != nil {
		return
	}
	q := r.URL.Query()
	locale := q.Get("locale")
	accountKind := q.Get("account_kind")
	if err := legal.ValidateRequirementsQuery(locale, accountKind); err != nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	set, err := svc.GetRequirements(r.Context(), locale, accountKind)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, set)
}

func serveLegalRecordAcceptance(w http.ResponseWriter, r *http.Request, svc legal.Service, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, err := s.mutation(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	var req legal.Acceptance
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	result, err := svc.RecordAcceptance(r.Context(), who, req, key)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, result)
}

func servePrivacyUpdatePreferences(w http.ResponseWriter, r *http.Request, svc privacy.PreferencesService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, err := s.mutation(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key=="" { writeError(w,r,domain.NewError("VALIDATION_FAILED",400));return }
 if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	rev, err := revision(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req privacy.PreferencesUpdate
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	prefs, err := svc.UpdatePreferences(r.Context(), who, req, rev, key)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, prefs)
}

func servePrivacyRequestDeletion(w http.ResponseWriter, r *http.Request, svc privacy.Service, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, err := s.mutation(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	var req privacy.DeletionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	accepted, err := svc.RequestDeletion(r.Context(), who, req, key)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 202, accepted)
}

func servePrivacyGetDeletionStatus(w http.ResponseWriter, r *http.Request, svc privacy.Service) {
	if r.Context().Err() != nil {
		return
	}
	requestID, err := routeID(r, "request")
	if err != nil {
		writeError(w, r, err)
		return
	}
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		writeError(w, r, domain.NewError("UNAUTHORIZED", 401))
		return
	}
	status, err := svc.GetDeletionStatus(r.Context(), requestID, authHeader)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, status)
}
