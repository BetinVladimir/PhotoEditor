package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeInputTransferScopeRejectsIncompletePlanBeforeSQL(t *testing.T) {
	reader := SQLTransferSourceReader{}
	for _, mode := range []string{"empty", "missing-frozen", "legacy-authority", "missing-qualification"} {
		p := nativeInputTransferWorkflowPlan{}
		switch mode {
		case "missing-frozen":
			p.core.tasks = []v2input.DraftTask{{}}
		case "legacy-authority":
			p.core.nativeReferenceRequired = true
		case "missing-qualification":
			p.core.transferQualificationRequired = true
		}
		tx := &manualLockTx{role: "photo_scheduler"}
		err := reader.lockNativeInputTransferPreparedInputs(context.Background(), tx, auth.Identity{}, Request{}, p, TransferPreparation{}, nil)
		if !errors.Is(err, v2input.ErrInvalid) || len(tx.calls) != 0 {
			t.Fatalf("%s reached SQL: %v %v", mode, err, tx.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := reader.lockNativeInputTransferPreparedInputs(ctx, nil, auth.Identity{}, Request{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled gate: %v", err)
	}
}

func TestNativeInputTransferInventoryRejectsMissingExtraAndDuplicate(t *testing.T) {
	a := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Contract: "NpyArray"}
	b := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Contract: "MaskRef"}
	wanted := []v2input.Entry{a, b, a}
	if err := matchNativeInputTransferInventory([]v2input.Entry{b, a}, wanted); err != nil {
		t.Fatal(err)
	}
	changed := a
	changed.Ref.SHA256 = strings.Repeat("c", 64)
	for _, actual := range [][]v2input.Entry{{a}, {a, b, a}, {changed, b}, {a, b, changed}} {
		if err := matchNativeInputTransferInventory(actual, wanted); !errors.Is(err, v2input.ErrConflict) {
			t.Fatalf("accepted invalid inventory: %v", err)
		}
	}
}
