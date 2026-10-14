package v2draft

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestIdentityAdoptionCommitRefusesUnverifiedEvidenceBeforeDatabase(t *testing.T) {
	e := PreparedIdentityAdoption{Scope: IdentityResultScope{Tenant: uuid.New(),
		Task: uuid.New(), Result: uuid.New()}}
	if err := (SQLIdentityAdoptionStore{Bucket: "photos"}).Commit(context.Background(), e, 1, 1); err == nil {
		t.Fatal("unverified identity result entered adoption")
	}
}
