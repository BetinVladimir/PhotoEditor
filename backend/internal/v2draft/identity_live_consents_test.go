package v2draft

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestIdentityLiveConsentGateRequiresTransactionAndEvidence(t *testing.T) {
	if err := CheckIdentityLiveConsentsInTx(context.Background(), nil,
		PreparedIdentityAdoption{}); err != v2input.ErrInvalid {
		t.Fatalf("missing transaction or evidence accepted: %v", err)
	}
}
