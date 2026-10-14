package semanticproposal

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestNativePersonPolicySelectionCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	selected, err := reader.selectNativePersonProtectionPolicy(ctx, auth.Identity{}, uuid.New(), 16<<20)
	if err != context.Canceled || selected.Report != (v2input.Ref{}) || selected.Expected.Operation != "" {
		t.Fatal("cancelled person policy selection exposed authority", err)
	}
	report, err := reader.discoverPersonQualificationReport(ctx, auth.Identity{}, uuid.New(), v2qualification.Expected{Operation: "mask.person", Version: "1"})
	if err != context.Canceled || report != (v2input.Ref{}) {
		t.Fatal("cancelled person discovery reached IO", err)
	}
}

func TestNativePersonPolicySelectionRefusesMissingPolicyAndBudget(t *testing.T) {
	reader := SQLTransferSourceReader{}
	for _, budget := range []int64{0, -1, 9007199254740992} {
		selected, err := reader.selectNativePersonProtectionPolicy(context.Background(), auth.Identity{}, uuid.New(), budget)
		if err == nil || selected.Report != (v2input.Ref{}) {
			t.Fatal("invalid budget exposed person report")
		}
	}
	for _, op := range []string{"", "face.detect", "semantic.transfer", "defects.propose"} {
		report, err := reader.discoverPersonQualificationReport(context.Background(), auth.Identity{}, uuid.New(), v2qualification.Expected{Operation: op, Version: "1"})
		if err == nil || report != (v2input.Ref{}) {
			t.Fatal("non-person discovery accepted", op)
		}
	}
}
