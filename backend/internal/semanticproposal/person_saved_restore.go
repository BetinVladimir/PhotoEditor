package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Requalifies the original person preparation without publication, graph repair
// or admission. Saved metadata is a locator, never authority. Legacy shells
// without recovery inputs fail closed rather than replacing their immutable config.
func (s SQLTransferSourceReader) restorePersonDraftPlan(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, anchor nativePreparationAnchor) (personDraftPlan, error) {
	var zero personDraftPlan
	owned, err := ownNativePlanRequest(ctx, who, anchor.Request, anchor.Key)
	if err != nil {
		return zero, err
	}
	if catalog == nil {
		return zero, v2input.ErrInvalid
	}
	saved, err := s.readNativeInputSavedStage(ctx, who, owned, anchor.Published.Ref, anchor.Snapshot, "mask.person")
	if err != nil {
		return zero, err
	}
	m := saved.recovery
	if m.Report == nil || m.RequestKey != anchor.Key {
		return zero, v2input.ErrConflict
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, owned.TenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	raw, err := s.readNativeSavedStageParams(ctx, who, owned, "mask.person", saved.params)
	if err != nil {
		return zero, err
	}
	var params struct {
		GuidedTile int `json:"guided_tile"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	body, err := s.Read(ctx, who, owned.TenantID, owned.Intent.Reference)
	if err != nil {
		return zero, err
	}
	expected := selected["mask.person"]
	compiled, err := v2input.PrepareSemanticPersonParams(ctx, owned.Intent.Reference, body, expected.ModelLockSHA, params.GuidedTile)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(raw, compiled.Params) {
		return zero, v2input.ErrConflict
	}
	document := uuid.NewSHA1(owned.RunID, []byte("semantic.mask.person.params:reference"))
	prepared, err := s.prepareSourcePersonTask(ctx, catalog, who, owned.TenantID, owned.RunID, owned.Intent.Reference, anchor.Color, document, saved.params, *m.Report, expected, params.GuidedTile, m.Costs, m.RAM, m.VRAM, m.Scratch, m.Output, m.PersonProfileVersion)
	if err != nil {
		return zero, err
	}
 var queue map[string]json.RawMessage
 if json.Unmarshal([]byte(saved.config),&queue)!=nil{return zero,v2input.ErrInvalid}
 rawQueue,present:=queue["queue_class"]
 if present&&string(rawQueue)!=`"batch"`{return zero,v2input.ErrInvalid}
 prepared.legacyQueueOmitted=!present
	prepared.continuation = m.Continuation
	prepared.ordinaryPolicy = m.OrdinaryPolicy
	plan, err := s.compilePersonDraftPlan(ctx, who, owned, anchor.Key, anchor.Published, anchor.Snapshot, anchor.SnapshotBody, prepared, policy, m.Reserved)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(saved, plan.shell); err != nil {
		return zero, err
	}
	current, err := s.readNativeSavedStageParams(ctx, who, owned, "mask.person", saved.params)
	if err != nil {
		return zero, err
	}
	after, err := s.readNativeInputSavedStage(ctx, who, owned, anchor.Published.Ref, anchor.Snapshot, "mask.person")
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(saved, after) || !bytes.Equal(raw, current) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, owned.TenantID, policy); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return plan, nil
}
