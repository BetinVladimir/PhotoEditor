package v2draft

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestIdentityQualificationGateRequiresTransactionAndEvidence(t *testing.T) {
	_, err := CheckIdentityLiveQualificationInTx(context.Background(), nil,
		PreparedIdentityAdoption{})
	if err != v2input.ErrInvalid {
		t.Fatalf("missing qualification authority accepted: %v", err)
	}
}
