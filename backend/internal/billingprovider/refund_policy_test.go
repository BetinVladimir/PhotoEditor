package billingprovider_test

import (
	"context"
	"testing"
	"time"

	"photoeditor/backend/internal/billingprovider"
)

func newRefundPolicy() (*billingprovider.RefundPolicyService, *billingprovider.FakeProvider) {
	p := billingprovider.NewFakeProvider()
	rs := billingprovider.NewInMemoryRefundRecordStore()
	ds := billingprovider.NewInMemoryDisputeStore()
	return billingprovider.NewRefundPolicyService(p, rs, ds), p
}

// Criteria A: consumed stays, unused is revoked.
func TestRefundPolicy_ConsumedPreserved_UnusedRevoked(t *testing.T) {
	ctx := context.Background()
	svc, fp := newRefundPolicy()

	fp.SeedRefund(billingprovider.Refund{
		ID: "re_1", ChargeID: "ch_1", Amount: 500, Status: "succeeded", CreatedAt: time.Now(),
	})

	r, err := svc.RecordRefund(ctx, "t1", "order-r1", "re_1",
		[]billingprovider.RefundAllocation{{LineItemID: "li_1", Quantity: 1, Amount: 500}},
		100, // 100 unused bytes
	)
	if err != nil {
		t.Fatalf("RecordRefund: %v", err)
	}
	if !r.ConsumedRemains {
		t.Error("consumed entitlement must remain (criteria A)")
	}
	if !r.UnusedRevoked {
		t.Error("unused remainder must be revoked (criteria A)")
	}
}

// Criteria B: refund without explicit allocation is rejected.
func TestRefundPolicy_NoAllocation_Rejected(t *testing.T) {
	ctx := context.Background()
	svc, fp := newRefundPolicy()

	fp.SeedRefund(billingprovider.Refund{
		ID: "re_2", ChargeID: "ch_2", Amount: 500, Status: "succeeded", CreatedAt: time.Now(),
	})

	_, err := svc.RecordRefund(ctx, "t1", "order-r2", "re_2", nil, 0)
	if err == nil {
		t.Error("refund without explicit allocation must be rejected (criteria B)")
	}
}

// Criteria D: no provider refund found → error (invoice/logs would remain intact).
func TestRefundPolicy_UnknownRefund_Rejected(t *testing.T) {
	ctx := context.Background()
	svc, _ := newRefundPolicy()

	_, err := svc.RecordRefund(ctx, "t1", "order-r3", "re_nonexistent",
		[]billingprovider.RefundAllocation{{LineItemID: "li_1", Quantity: 1, Amount: 100}},
		0,
	)
	if err == nil {
		t.Error("unknown provider refund should be rejected")
	}
}

// Criteria C: dispute blocks risky admissions, not deletion/data rights.
func TestRefundPolicy_Dispute_BlocksRiskyOnly(t *testing.T) {
	ctx := context.Background()
	svc, _ := newRefundPolicy()

	d, err := svc.OpenDispute(ctx, "tenant-dispute", "dp_1")
	if err != nil {
		t.Fatalf("OpenDispute: %v", err)
	}
	if !d.BlocksRiskyAdmissions {
		t.Error("open dispute must block risky admissions")
	}
	// Deletion/data rights are not blocked (structural: BlocksRiskyAdmissions is
	// separate from deletion_blocked, which doesn't exist here — criteria C).

	blocked, err := svc.IsRiskyAdmissionBlocked(ctx, "tenant-dispute")
	if err != nil || !blocked {
		t.Errorf("risky admission should be blocked: err=%v blocked=%v", err, blocked)
	}
}

// Criteria C: closed dispute unblocks.
func TestRefundPolicy_Dispute_Closed_Unblocks(t *testing.T) {
	ctx := context.Background()
	svc, _ := newRefundPolicy()

	svc.OpenDispute(ctx, "tenant-close", "dp_2")
	svc.CloseDispute(ctx, "dp_2")

	blocked, err := svc.IsRiskyAdmissionBlocked(ctx, "tenant-close")
	if err != nil || blocked {
		t.Errorf("closed dispute should unblock: err=%v blocked=%v", err, blocked)
	}
}

// Idempotent: same refund ID returns existing record.
func TestRefundPolicy_Idempotent(t *testing.T) {
	ctx := context.Background()
	svc, fp := newRefundPolicy()

	fp.SeedRefund(billingprovider.Refund{
		ID: "re_idem", ChargeID: "ch_idem", Amount: 100, Status: "succeeded", CreatedAt: time.Now(),
	})
	allocs := []billingprovider.RefundAllocation{{LineItemID: "li_1", Quantity: 1, Amount: 100}}

	r1, _ := svc.RecordRefund(ctx, "t1", "order-idem", "re_idem", allocs, 0)
	r2, err := svc.RecordRefund(ctx, "t1", "order-idem", "re_idem", allocs, 0)
	if err != nil {
		t.Fatalf("second RecordRefund: %v", err)
	}
	if r1.ID != r2.ID {
		t.Errorf("idempotent repeat returned different ID: %q vs %q", r1.ID, r2.ID)
	}
}
