package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestQualificationPlanScopeAndPinDenyBeforeSQL(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := Request{TenantID: uuid.New(), ActorID: who.UserID, RunID: uuid.New(), RequestSHA: "request"}
	pin := v2input.SemanticQualificationPin{PolicyID: uuid.New()}
	project := uuid.New()
	config := v2input.SemanticDraftConfig{ProjectID: project, Semantic: v2input.SemanticFrozenNodes{AuthSessionID: who.SessionID, Qualification: &pin}}
	body, _ := canonical.Marshal(config)
	plan := WorkflowPlan{tenant: request.TenantID, actor: who.UserID, run: request.RunID, project: project, requestSHA: request.RequestSHA, config: body, transferQualification: &transferQualificationPreparationProof{pin: pin}}
	for _, change := range []func(*WorkflowPlan){
		func(p *WorkflowPlan) { p.transferQualificationRequired = true; p.transferQualification = nil },
		func(p *WorkflowPlan) { p.tenant = uuid.New() },
		func(p *WorkflowPlan) { p.actor = uuid.New() },
		func(p *WorkflowPlan) {
			cfg := config
			cfg.Semantic.AuthSessionID = uuid.New()
			p.config, _ = canonical.Marshal(cfg)
		},
		func(p *WorkflowPlan) { cfg := config; cfg.ProjectID = uuid.New(); p.config, _ = canonical.Marshal(cfg) },
		func(p *WorkflowPlan) {
			proof := *p.transferQualification
			proof.pin.PolicyID = uuid.New()
			p.transferQualification = &proof
		},
	} {
		changed := plan
		change(&changed)
		tx := &manualLockTx{role: "photo_scheduler"}
		if err := LockQualificationInTransaction(context.Background(), tx, who, request, changed); err != v2input.ErrInvalid || len(tx.calls) != 0 {
			t.Fatal("invalid plan reached SQL", err, tx.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := LockQualificationInTransaction(ctx, tx, who, request, plan); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancelled lock reached SQL", err)
	}
}
