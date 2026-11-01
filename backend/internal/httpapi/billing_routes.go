package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"photoeditor/backend/internal/domain"
)

func (s *Server) billingAPIRoutes(r chi.Router) {
	// Public: no authentication required
	r.Get("/api/v1/billing/catalog", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingCatalog == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingGetCatalog(w, r, s.BillingCatalog)
	})

	r.Get("/api/v1/legal/requirements", func(w http.ResponseWriter, r *http.Request) {
		if s.Legal == nil {
			writeError(w, r, unavailable())
			return
		}
		serveLegalGetRequirements(w, r, s.Legal)
	})

	// Stripe webhook ingress: no user session, no CSRF
	r.Post("/api/v1/webhooks/stripe/{account}", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingWebhook == nil {
			writeError(w, r, unavailable())
			return
		}
		serveStripeWebhook(w, r, s.BillingWebhook)
	})

	// Deletion status: scoped status token, not user session
	r.Get("/api/v1/me/deletion/{request}/status", func(w http.ResponseWriter, r *http.Request) {
		if s.Privacy == nil {
			writeError(w, r, unavailable())
			return
		}
		servePrivacyGetDeletionStatus(w, r, s.Privacy)
	})

	if s.Auth == nil {
		return
	}

	// Authenticated tenant-scoped billing routes
	r.Get("/api/v1/tenants/{t}/billing", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingSummary == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingGetSummary(w, r, s.BillingSummary, s)
	})

	r.Get("/api/v1/tenants/{t}/billing/usage", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingSummary == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingGetUsage(w, r, s.BillingSummary, s)
	})

	r.Get("/api/v1/tenants/{t}/billing/buckets", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingSummary == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingGetBuckets(w, r, s.BillingSummary, s)
	})

	r.Post("/api/v1/tenants/{t}/billing/quotes", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingQuotes == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingCreateQuote(w, r, s.BillingQuotes, s)
	})

	r.Post("/api/v1/tenants/{t}/billing/orders", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingOrders == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingCreateOrder(w, r, s.BillingOrders, s)
	})

	r.Get("/api/v1/tenants/{t}/billing/orders/{order}", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingOrders == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingGetOrder(w, r, s.BillingOrders, s)
	})

	r.Post("/api/v1/tenants/{t}/billing/changes", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingChanges == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingCreateChange(w, r, s.BillingChanges, s)
	})

	r.Post("/api/v1/tenants/{t}/billing/changes/{change}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingChanges == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingCancelChange(w, r, s.BillingChanges, s)
	})

	r.Post("/api/v1/tenants/{t}/billing/portal", func(w http.ResponseWriter, r *http.Request) {
		if s.BillingPortal == nil {
			writeError(w, r, unavailable())
			return
		}
		serveBillingCreatePortalSession(w, r, s.BillingPortal, s)
	})

	// User-scoped (no tenant) routes
	r.Post("/api/v1/me/legal/acceptances", func(w http.ResponseWriter, r *http.Request) {
		if s.Legal == nil {
			writeError(w, r, unavailable())
			return
		}
		serveLegalRecordAcceptance(w, r, s.Legal, s)
	})

	r.Patch("/api/v1/me/privacy", func(w http.ResponseWriter, r *http.Request) {
		if s.Privacy == nil {
			writeError(w, r, unavailable())
			return
		}
		servePrivacyUpdatePreferences(w, r, s.Privacy, s)
	})

	r.Post("/api/v1/me/deletion", func(w http.ResponseWriter, r *http.Request) {
		if s.Privacy == nil {
			writeError(w, r, unavailable())
			return
		}
		servePrivacyRequestDeletion(w, r, s.Privacy, s)
	})
}

func unavailable() error {
	return domain.NewError("SERVICE_UNAVAILABLE", 503)
}
