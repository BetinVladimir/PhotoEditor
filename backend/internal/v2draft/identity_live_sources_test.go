package v2draft

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestIdentityLiveSourceGateRequiresTransactionAndEvidence(t *testing.T) {
	if err := CheckIdentityLiveSourcesInTx(context.Background(), nil,
		PreparedIdentityAdoption{}); err != v2input.ErrInvalid {
		t.Fatalf("missing transaction or source evidence accepted: %v", err)
	}
}
