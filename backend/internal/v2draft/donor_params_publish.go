package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// SQLDonorParamsPublisher commits exact prepublication intent before object IO.
// SQL independently rechecks authority at reserve, replay and completion; the
// final intent check rejects revocation during IO. This does not admit a graph.
type SQLDonorParamsPublisher struct {
	DB        v2portrait.DonorConfirmationTransactions
	Publisher v2input.DocumentPublisher
}

func (s SQLDonorParamsPublisher) Publish(ctx context.Context, who auth.Identity, tenant, run, workflow uuid.UUID,
	ordinal int, loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence) (v2portrait.PublishedDonorTaskPlan, error) {
	var zero v2portrait.PublishedDonorTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Publisher == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || run == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	pin, err := verifyPreparedDonorQualification(loaded, prepared)
	if err != nil {
		return zero, err
	}
	// Rebuild all portions before any DB or publisher callbacks. The synthetic
	// Params ref is used only to validate the prepublication closure and aliases.
	if _, err = BindDonorParamsInventory(ctx, loaded, prepared, v2input.Ref{ArtifactID: uuid.New(), SHA256: loaded.Plan.ParamsSHA}); err != nil {
		return zero, err
	}
	owner, err := v2input.TaskID(workflow, "portrait.open_eyes", ordinal)
	if err != nil {
		return zero, err
	}
	key, err := v2input.NodeKey("portrait.open_eyes", ordinal)
	if err != nil {
		return zero, err
	}
	objects, err := prepared.qualification.Objects()
	if err != nil {
		return zero, err
	}
	// Own the complete return plan before a publisher can mutate caller slices.
	planBytes, err := json.Marshal(loaded.Plan)
	if err != nil {
		return zero, err
	}
	var plan v2portrait.DonorTaskPlan
	if err = json.Unmarshal(planBytes, &plan); err != nil {
		return zero, err
	}
	request, err := canonical.Marshal(donorParamsIntent{who.UserID, who.SessionID, key, owner, string(plan.Params), plan.ParamsSHA, pin, objects, plan.ReservedBytes})
	if err != nil {
		return zero, err
	}
	if _, err = ParseDonorParamsRecoveryIntent(ctx, who, workflow, request); err != nil {
		return zero, err
	}
	ids := make(map[uuid.UUID]bool, len(prepared.Entries))
	for _, entry := range prepared.Entries {
		ids[entry.Ref.ArtifactID] = true
	}
	if err = s.saveIntent(ctx, tenant, run, request); err != nil {
		return zero, err
	}
	ref, err := s.Publisher.Publish(ctx, artifactjson.Scope{Tenant: tenant, Run: run, Owner: owner, Purpose: "task_params", DonorPortrait: true}, append([]byte(nil), plan.Params...))
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if ref.ID == uuid.Nil || ref.SHA256 != plan.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	if ids[ref.ID] {
		return zero, v2input.ErrInvalid
	}
	if err = s.saveIntent(ctx, tenant, run, request); err != nil {
		return zero, err
	}
	return v2portrait.PublishedDonorTaskPlan{ParamsRef: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Plan: plan}, nil
}

func (s SQLDonorParamsPublisher) saveIntent(ctx context.Context, tenant, run uuid.UUID, request []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(ctx, tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.save_v28_donor_params_intent($1,$2,$3::jsonb)", tenant, run, string(request)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
