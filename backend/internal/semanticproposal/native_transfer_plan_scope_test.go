package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativePlanGatesRejectMissingAuthorityBeforeSQL(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := Request{TenantID: uuid.New(), ActorID: who.UserID, RunID: uuid.New()}
	base := WorkflowPlan{tenant: request.TenantID, actor: who.UserID, run: request.RunID, project: uuid.New(), sourcesRequired: true, tasks: []v2input.DraftTask{{}}, sources: make([]sourcePreparationProof, 2), protection: make([]protectionProof, 2), person: make([]personProtectionProof, 1), inventory: CatalogInventory{Targets: []v2input.SessionVersionBinding{{}}}}
	gates := []struct {
		name string
		run  func(context.Context, pgx.Tx, auth.Identity, Request, WorkflowPlan) error
	}{{"source", LockPreparedSourcesInTransaction}, {"person", LockPersonProducerInTransaction}, {"protection", LockProtectionInTransaction}}
	for _, mode := range []string{"missing-proof", "stripped-required", "missing-private-authority", "missing-target-person", "missing-transfer-qualification", "stripped-source-required"} {
		plan := base
		switch mode {
		case "missing-proof":
			plan.nativeReferenceRequired = true
		case "stripped-required":
			plan.nativeReference = &nativeSourceContinuationProof{}
		case "missing-private-authority":
			plan.nativeReferenceRequired = true
			plan.nativeReference = &nativeSourceContinuationProof{}
		case "missing-transfer-qualification":
			plan.nativeReferenceRequired = true
			plan.nativeReference = &nativeSourceContinuationProof{}
			plan.transferQualificationRequired = true
		case "stripped-source-required":
			plan.nativeReferenceRequired = true
			plan.nativeReference = &nativeSourceContinuationProof{}
			plan.sourcesRequired = false
		case "missing-target-person":
			plan.nativeReferenceRequired = true
			plan.nativeReference = &nativeSourceContinuationProof{}
			plan.person = nil
		}
		for _, gate := range gates {
			tx := &manualLockTx{role: "photo_scheduler"}
			if err := gate.run(context.Background(), tx, who, request, plan); err != v2input.ErrInvalid || len(tx.calls) != 0 {
				t.Fatal("incomplete native plan reached SQL", mode, gate.name, err, tx.calls)
			}
		}
	}
}
