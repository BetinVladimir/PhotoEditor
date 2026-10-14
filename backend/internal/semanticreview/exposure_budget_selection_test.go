package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestExposureBudgetSelectionFreezesDeploymentAndExcludesRejections(t *testing.T) {
	sha := strings.Repeat("a", 64)
	config := ExposureDeploymentBudget{
		Qualification: ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: sha, Backend: "cpu", Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha},
		OwnerCosts:    ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20},
		Limits:        ExposureResourceEstimate{RAMBytes: 1 << 30, ScratchBytes: 1 << 30, OutputBytes: 1 << 30, PixelCount: 200000000},
		ModelLockSHA:  sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
	}
	selectBudget, err := NewExposureBudgetSelection(config)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, intent, approved := uuid.New(), uuid.New(), uuid.New()
	request := ApplyDecision{PlanSHA: sha, ExpectedRevision: "1", TargetDecisions: []TargetDecision{{TargetID: approved, Decision: "approve"}, {TargetID: uuid.New(), Decision: "reject"}}}
	original := config.Qualification
	config.Qualification.Backend = "cuda"
	config.OwnerCosts.RuntimeRAMBytes = 0
	budgets, err := selectBudget(context.Background(), who, tenant, intent, request)
	if err != nil || len(budgets) != 1 || budgets[0].TargetID != approved || budgets[0].Qualification != original || budgets[0].OwnerCosts.RuntimeRAMBytes != 1<<20 {
		t.Fatal("mutable installation or rejection entered approved budget", err)
	}
	budgets[0].Qualification.Backend = "cuda"
	again, err := selectBudget(context.Background(), who, tenant, intent, request)
	if err != nil || again[0].Qualification != original {
		t.Fatal("returned budget changed installation", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = selectBudget(ctx, who, tenant, intent, request); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled selection", err)
	}
	config.Qualification = original
	config.OwnerCosts.RuntimeRAMBytes = 1 << 20
	config.RuntimeLockSHA = strings.Repeat("b", 64)
	if _, err = NewExposureBudgetSelection(config); !errors.Is(err, ErrInvalid) {
		t.Fatal("runtime substitution accepted", err)
	}
}
