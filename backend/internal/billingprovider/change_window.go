package billingprovider

import (
	"context"
	"fmt"
	"time"
)

// ChangeWindowChecker verifies whether a plan change is permitted at a given
// point in time. A change is blocked when:
//   - The provider subscription has an open renewal invoice, OR
//   - Fewer than 24 hours remain before the period end
//
// Criteria A: in either case, return BILLING_CHANGE_WINDOW_CLOSED.
// Criteria C: the time guard does not replace compensation — a race after
// a quote still requires the saga to compensate if the window closes.
type ChangeWindowChecker struct {
	provider Provider
}

// NewChangeWindowChecker returns a ChangeWindowChecker.
func NewChangeWindowChecker(p Provider) *ChangeWindowChecker {
	return &ChangeWindowChecker{provider: p}
}

// ChangeWindowResult holds the outcome of a window check.
type ChangeWindowResult struct {
	Allowed    bool
	RenewalAt  time.Time // next period start if window is closed due to renewal proximity
	Reason     string    // "open_renewal_invoice" | "renewal_too_close" | ""
}

// ErrChangeWindowClosed is returned when a plan change is not permitted.
type ErrChangeWindowClosed struct {
	Reason    string
	RenewalAt time.Time
}

func (e ErrChangeWindowClosed) Error() string {
	return fmt.Sprintf("BILLING_CHANGE_WINDOW_CLOSED: %s (renewal_at: %s)", e.Reason, e.RenewalAt.Format(time.RFC3339))
}

// Check verifies whether a plan change is permitted for the subscription.
//
// subscriptionID is read from the provider to get the authoritative state.
// invoiceID is the open renewal invoice, if already known; pass "" to skip
// that check (the subscription period end is always checked).
//
// Criteria A: open renewal invoice → ErrChangeWindowClosed.
// Criteria A: period_end within 24 hours → ErrChangeWindowClosed.
func (cwc *ChangeWindowChecker) Check(ctx context.Context, subscriptionID, openInvoiceID string) (ChangeWindowResult, error) {
	sub, err := cwc.provider.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return ChangeWindowResult{}, fmt.Errorf("get subscription: %w", err)
	}

	// Criteria A: check open renewal invoice.
	if openInvoiceID != "" {
		inv, err := cwc.provider.GetInvoice(ctx, openInvoiceID)
		if err != nil {
			return ChangeWindowResult{}, fmt.Errorf("get invoice: %w", err)
		}
		if inv.Status == "open" || inv.Status == "draft" {
			return ChangeWindowResult{
				Allowed:   false,
				RenewalAt: sub.PeriodEnd,
				Reason:    "open_renewal_invoice",
			}, ErrChangeWindowClosed{Reason: "open_renewal_invoice", RenewalAt: sub.PeriodEnd}
		}
	}

	// Criteria A: check proximity to period end (< 24h).
	timeToRenewal := time.Until(sub.PeriodEnd)
	if timeToRenewal < 24*time.Hour {
		return ChangeWindowResult{
			Allowed:   false,
			RenewalAt: sub.PeriodEnd,
			Reason:    "renewal_too_close",
		}, ErrChangeWindowClosed{Reason: "renewal_too_close", RenewalAt: sub.PeriodEnd}
	}

	return ChangeWindowResult{Allowed: true}, nil
}

// CheckSnapshot checks a previously-read subscription snapshot without a
// provider call — used for race detection after a quote is issued.
// Criteria B: if the window closed after the quote was issued, the saga must
// compensate rather than applying the delta.
func (cwc *ChangeWindowChecker) CheckSnapshot(sub Subscription, openInvoiceStatus string) (ChangeWindowResult, error) {
	if openInvoiceStatus == "open" || openInvoiceStatus == "draft" {
		return ChangeWindowResult{
			Allowed:   false,
			RenewalAt: sub.PeriodEnd,
			Reason:    "open_renewal_invoice",
		}, ErrChangeWindowClosed{Reason: "open_renewal_invoice", RenewalAt: sub.PeriodEnd}
	}

	timeToRenewal := time.Until(sub.PeriodEnd)
	if timeToRenewal < 24*time.Hour {
		return ChangeWindowResult{
			Allowed:   false,
			RenewalAt: sub.PeriodEnd,
			Reason:    "renewal_too_close",
		}, ErrChangeWindowClosed{Reason: "renewal_too_close", RenewalAt: sub.PeriodEnd}
	}

	return ChangeWindowResult{Allowed: true}, nil
}
