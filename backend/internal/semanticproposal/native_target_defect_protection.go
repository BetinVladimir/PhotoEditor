package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

// Target-only proof. It cannot be admitted by source/reference or generic gates.
// The root preserves the report's complete probability mask without resampling.
type nativeTargetDefectProtectionPreparation struct {
	document  uuid.UUID
	body      string
	completed completedNativeTargetDefect
}
type publishedNativeTargetDefectProtection struct {
	preparation nativeTargetDefectProtectionPreparation
	root        v2input.Ref
}
type registeredNativeTargetDefectProtection struct {
	published publishedNativeTargetDefectProtection
	object    defectEvidenceObject
	verified  VerifiedProtection
}

func targetDefectProtectionDocument(namespace uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte("semantic.defect.protection:reference"))
}
func (s SQLTransferSourceReader) prepareNativeTargetDefectProtection(ctx context.Context, who auth.Identity, plan nativeTargetDefectDraftPlan, color TransferColorProfile) (nativeTargetDefectProtectionPreparation, error) {
	var zero nativeTargetDefectProtectionPreparation
	result, err := s.readCompletedNativeTargetDefect(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	image := json.RawMessage(plan.prepared.preparation.landmarks.detector.completed.sourceBody)
	body, pixels, err := v2input.BuildSemanticDefectProtectionDocument(ctx, result.catalog.Params, result.evidence.Params, result.catalog.Outputs[4], result.evidence.Report, result.catalog.Outputs[3], result.evidence.Proposal, plan.anchor.entry.Binding, image)
	if err != nil {
		return zero, err
	}
	if pixels != result.catalog.Outputs[1] {
		return zero, v2input.ErrConflict
	}
	// Build above is pure: readCompleted already rechecks all catalogs and
	// physical dependencies after IO. Publication/read bracket their own IO.
	return nativeTargetDefectProtectionPreparation{document: targetDefectProtectionDocument(plan.anchor.entry.Namespace), body: string(body), completed: result}, ctx.Err()
}
func (s SQLTransferSourceReader) publishNativeTargetDefectProtection(ctx context.Context, who auth.Identity, plan nativeTargetDefectDraftPlan, color TransferColorProfile, prepared nativeTargetDefectProtectionPreparation, publisher v2draft.Publisher) (publishedNativeTargetDefectProtection, error) {
	var zero publishedNativeTargetDefectProtection
	current, err := s.prepareNativeTargetDefectProtection(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, prepared) || publisher.DB == nil || publisher.Objects == nil || publisher.Bucket != s.Bucket {
		return zero, v2input.ErrConflict
	}
	root, err := publisher.PublishSemanticProtectionDocument(ctx, who, plan.shell.Tenant, prepared.document, plan.anchor.entry.Binding, []byte(plan.prepared.preparation.landmarks.detector.completed.sourceBody), []byte(prepared.body))
	if err != nil {
		return zero, err
	}
	current, err = s.prepareNativeTargetDefectProtection(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, prepared) {
		return zero, v2input.ErrConflict
	}
	if _, err = v2input.ResolveSemanticMaskPixels(root, []byte(prepared.body), plan.anchor.entry.Binding, []byte(plan.prepared.preparation.landmarks.detector.completed.sourceBody), true); err != nil {
		return zero, err
	}
	return publishedNativeTargetDefectProtection{preparation: prepared, root: root}, ctx.Err()
}

// Caller prepares completed packet before SQL, holds original parent advisory
// lock, and rolls back on refusal. This method performs no object IO.
func (s SQLTransferSourceReader) registerNativeTargetDefectProtectionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeTargetDefectDraftPlan, published publishedNativeTargetDefectProtection, completed json.RawMessage) error {
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return err
	}
	if tx == nil || len(published.preparation.completed.catalog.Outputs) != 5 || published.preparation.document != targetDefectProtectionDocument(plan.anchor.entry.Namespace) || len(completed) < 2 || len(completed) > 32<<20 {
		return v2input.ErrInvalid
	}
	pixels, err := v2input.ResolveSemanticMaskPixels(published.root, []byte(published.preparation.body), plan.anchor.entry.Binding, []byte(plan.prepared.preparation.landmarks.detector.completed.sourceBody), true)
	if err != nil {
		return err
	}
	if pixels != published.preparation.completed.catalog.Outputs[1] {
		return v2input.ErrConflict
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.shell.Tenant.String()); err != nil {
		return err
	}
	r := plan.anchor.request
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.register_semantic_target_defect_protection($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13)", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.RunID, r.RequestSHA, plan.shell.Tasks[0].ID, string(completed), published.preparation.document, published.root.ArtifactID, published.root.SHA256, published.preparation.body, string(published.preparation.completed.evidence.Report)).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) nativeTargetDefectProtectionObject(ctx context.Context, who auth.Identity, plan nativeTargetDefectDraftPlan) (defectEvidenceObject, error) {
	var zero, out defectEvidenceObject
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	r := plan.anchor.request
	err := tenant.WithTenant(ctx, s.DB, who, r.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, "SELECT * FROM photo.semantic_target_defect_protection_object($1,$2,$3,$4,$5,$6,$7)", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.RunID, r.RequestSHA, plan.shell.Tasks[0].ID).Scan(&out.ref.ArtifactID, &out.ref.SHA256, &bucket, &out.object.Key, &out.object.VersionID, &out.object.Bytes, &out.object.ContentType)
		if err != nil {
			return err
		}
		if bucket != s.Bucket || out.ref.ArtifactID == uuid.Nil || out.object.Bytes < 2 || out.object.Bytes > 1<<20 || out.object.Key == "" || out.object.VersionID == "" || out.object.VersionID == "null" || out.object.ContentType != "application/json" {
			return v2input.ErrConflict
		}
		out.object.SHA256 = out.ref.SHA256
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrConflict
	}
	if err != nil {
		return zero, err
	}
	return out, ctx.Err()
}
func (s SQLTransferSourceReader) readRegisteredNativeTargetDefectProtection(ctx context.Context, who auth.Identity, plan nativeTargetDefectDraftPlan, color TransferColorProfile, published publishedNativeTargetDefectProtection) (registeredNativeTargetDefectProtection, error) {
	var zero registeredNativeTargetDefectProtection
	current, err := s.prepareNativeTargetDefectProtection(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, published.preparation) {
		return zero, v2input.ErrConflict
	}
	object, err := s.nativeTargetDefectProtectionObject(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	if object.ref != published.root {
		return zero, v2input.ErrConflict
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object.object, object.ref)
	if err != nil {
		return zero, err
	}
	if string(body) != current.body {
		return zero, v2input.ErrConflict
	}
	pixels, err := v2input.ResolveSemanticMaskPixels(object.ref, body, plan.anchor.entry.Binding, []byte(plan.prepared.preparation.landmarks.detector.completed.sourceBody), true)
	if err != nil {
		return zero, err
	}
	if pixels != current.completed.catalog.Outputs[1] {
		return zero, v2input.ErrConflict
	}
	after, err := s.nativeTargetDefectProtectionObject(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	if after != object {
		return zero, v2input.ErrConflict
	}
	repeated, err := s.prepareNativeTargetDefectProtection(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(repeated, current) {
		return zero, v2input.ErrConflict
	}
	return registeredNativeTargetDefectProtection{published: published, object: object, verified: VerifiedProtection{Root: object.ref, Pixels: pixels, Body: append(json.RawMessage(nil), body...)}}, ctx.Err()
}

// SQL-only consumer barrier: the packet and root pin are read/verified before
// the caller transaction. It locks the registered immutable object version and
// all canonical producer dependencies through the caller's admission commit.
func (s SQLTransferSourceReader) lockNativeTargetDefectProtectionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeTargetDefectDraftPlan, registered registeredNativeTargetDefectProtection, completed json.RawMessage) error {
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return err
	}
	if tx == nil || len(completed) < 2 || len(completed) > 32<<20 || registered.object.ref != registered.published.root || registered.verified.Root != registered.object.ref {
		return v2input.ErrInvalid
	}
	prepared := registered.published.preparation
	if len(prepared.completed.catalog.Outputs) != 5 || prepared.document != targetDefectProtectionDocument(plan.anchor.entry.Namespace) || string(registered.verified.Body) != prepared.body || registered.verified.Pixels != prepared.completed.catalog.Outputs[1] || registered.object.object.SHA256 != registered.object.ref.SHA256 {
		return v2input.ErrConflict
	}
	pixels, err := v2input.ResolveSemanticMaskPixels(registered.verified.Root, registered.verified.Body, plan.anchor.entry.Binding, []byte(plan.prepared.preparation.landmarks.detector.completed.sourceBody), true)
	if err != nil {
		return err
	}
	if pixels != registered.verified.Pixels {
		return v2input.ErrConflict
	}
	o := registered.object
	root, err := json.Marshal(map[string]any{"ref": o.ref, "bucket": s.Bucket, "key": o.object.Key, "version": o.object.VersionID, "bytes": o.object.Bytes, "media": o.object.ContentType})
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
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.shell.Tenant.String()); err != nil {
		return err
	}
	r := plan.anchor.request
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_target_defect_protection($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb)", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.RunID, r.RequestSHA, plan.shell.Tasks[0].ID, string(completed), string(root)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
