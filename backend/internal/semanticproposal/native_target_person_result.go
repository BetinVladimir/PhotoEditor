package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type nativeTargetPersonResult struct {
	planSeal string
	result   uuid.UUID
	outputs  []v2input.Ref
	mask     VerifiedPersonMask
	seal     string
}

func targetPersonResultDigest(p nativeTargetPersonResult) (string, error) {
	return canonical.Hash(map[string]any{"plan": p.planSeal, "result": p.result, "outputs": p.outputs, "root": p.mask.Root, "pixels": p.mask.Pixels, "body": p.mask.Body})
}

// Discover the exact canonical task result, never a source-wide candidate. All
// physical IO finishes before the consumer's registration transaction.
func (s SQLTransferSourceReader) readNativeTargetPersonResult(ctx context.Context, who auth.Identity, plan nativeTargetPersonPlan) (nativeTargetPersonResult, error) {
	var zero nativeTargetPersonResult
	if err := verifyNativeTargetPersonPlan(ctx, plan); err != nil {
		return zero, err
	}
	rebuilt, err := s.compileNativeTargetPersonPlan(ctx, who, plan.anchor, plan.person.prepared, plan.person.policy, plan.person.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if rebuilt.seal != plan.seal || s.DB == nil {
		return zero, v2input.ErrConflict
	}
	type saved struct {
		Result  uuid.UUID     `json:"result_id"`
		Outputs []v2input.Ref `json:"outputs"`
	}
	read := func() (saved, error) {
		var value saved
		err := tenant.WithTenant(ctx, s.DB, who, plan.anchor.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
			var raw []byte
			if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_person_canonical_outputs($1,$2,$3,$4,$5,$6)", plan.anchor.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.person.shell.Run, plan.person.shell.ConfigSHA).Scan(&raw); err != nil {
				return err
			}
			if len(raw) == 0 || len(raw) > 8192 || json.Unmarshal(raw, &value) != nil || value.Result == uuid.Nil || len(value.Outputs) != 4 {
				return v2input.ErrConflict
			}
			return nil
		})
		return value, err
	}
	first, err := read()
	if err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(plan.person.policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	mask, err := s.ReadQualifiedPersonEvidence(ctx, who, plan.anchor.request.TenantID, plan.anchor.entry.Binding, first.Outputs[2], plan.person.prepared.qualification.report.ref, selected["mask.person"])
	if err != nil {
		return zero, err
	}
	if mask.Pixels != first.Outputs[0] || mask.Root != first.Outputs[2] {
		return zero, v2input.ErrConflict
	}
	pins, err := s.personEvidenceObjects(ctx, who, plan.anchor.request.TenantID, plan.anchor.entry.Binding, mask)
	if err != nil {
		return zero, err
	}
	if len(pins) != 2 || pins[1].ref != first.Outputs[3] {
		return zero, v2input.ErrConflict
	}
	after, err := s.ReadQualifiedPersonEvidence(ctx, who, plan.anchor.request.TenantID, plan.anchor.entry.Binding, first.Outputs[2], plan.person.prepared.qualification.report.ref, selected["mask.person"])
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(mask, after) {
		return zero, v2input.ErrConflict
	}
	currentPins, err := s.personEvidenceObjects(ctx, who, plan.anchor.request.TenantID, plan.anchor.entry.Binding, after)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(pins, currentPins) {
		return zero, v2input.ErrConflict
	}
	current, err := read()
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(first, current) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, plan.anchor.request.TenantID, plan.person.policy); err != nil {
		return zero, err
	}
	proof := nativeTargetPersonResult{planSeal: plan.seal, result: first.Result, outputs: append([]v2input.Ref(nil), first.Outputs...), mask: VerifiedPersonMask{Root: mask.Root, Pixels: mask.Pixels, Body: append(json.RawMessage(nil), mask.Body...)}}
	proof.seal, err = targetPersonResultDigest(proof)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return proof, nil
}

// Caller holds the original parent lock and owns rollback. This locks canonical
// consumer authority only; it never completes the parent or creates queue work.
func (s SQLTransferSourceReader) lockNativeTargetPersonResult(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetPersonPlan, built v2input.Built, p nativeTargetPersonResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest, err := targetPersonResultDigest(p)
	if err != nil {
		return err
	}
	if tx == nil || p.seal == "" || digest != p.seal || p.planSeal != plan.seal || p.result == uuid.Nil || len(p.outputs) != 4 || p.outputs[0] != p.mask.Pixels || p.outputs[2] != p.mask.Root {
		return v2input.ErrConflict
	}
	packet, err := s.marshalNativeTargetPersonAdmissionEvidence(ctx, who, d, plan, built)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.anchor.request.TenantID.String()); err != nil {
		return err
	}
	outputs, err := canonical.Marshal(p.outputs)
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_target_person_result($1,$2,$3::jsonb,$4,$5::jsonb)", plan.anchor.request.TenantID, plan.person.shell.Run, string(packet), p.result, string(outputs)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Finalize only this canonical producer. The SQL transition releases residual
// run quota and emits completion once; parent/next-stage admission is separate.
func (s SQLTransferSourceReader) finishNativeTargetPersonInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetPersonPlan, built v2input.Built, proof nativeTargetPersonResult) error {
	if err := s.lockNativeTargetPersonResult(ctx, tx, who, d, plan, built, proof); err != nil {
		return err
	}
	packet, err := s.marshalNativeTargetPersonAdmissionEvidence(ctx, who, d, plan, built)
	if err != nil {
		return err
	}
	outputs, err := canonical.Marshal(proof.outputs)
	if err != nil {
		return err
	}
	var finished bool
	if err := tx.QueryRow(ctx, "SELECT photo.finish_semantic_target_person($1,$2,$3::jsonb,$4,$5::jsonb)", plan.anchor.request.TenantID, plan.person.shell.Run, string(packet), proof.result, string(outputs)).Scan(&finished); err != nil {
		return err
	}
	if !finished {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
