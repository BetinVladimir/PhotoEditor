package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type nativeTargetFaceResult struct {
	planSeal, operation, body, paramsBody, seal string
	catalog                                     nativeFaceResultCatalog
	landmarks                                   []review.Face68
	labels                                      review.FaceLabels19
	counts                                      [19]int64
	objects                                     [2]defectEvidenceObject
	pixels                                      defectEvidenceObject
	detector                                    nativeTargetDetectorResult
}

func targetFaceResultDigest(p nativeTargetFaceResult) (string, error) {
	detector, err := targetDetectorResultDigest(p.detector)
	if err != nil || detector != p.detector.seal {
		return "", v2input.ErrConflict
	}
	return canonical.Hash(map[string]any{"plan": p.planSeal, "operation": p.operation, "catalog": p.catalog, "body": p.body, "params": p.paramsBody, "landmarks": p.landmarks, "labels": p.labels, "counts": p.counts, "detector": detector, "objects": []any{map[string]any{"ref": p.objects[0].ref, "object": p.objects[0].object}, map[string]any{"ref": p.objects[1].ref, "object": p.objects[1].object}}, "pixels": map[string]any{"ref": p.pixels.ref, "object": p.pixels.object}})
}

// Physical validation precedes the consumer transaction. No source-wide latest
// discovery or original request reference substitution is permitted.
func (s SQLTransferSourceReader) readNativeTargetFaceResult(ctx context.Context, who auth.Identity, plan nativeTargetFaceDraftPlan, color TransferColorProfile) (nativeTargetFaceResult, error) {
	var zero nativeTargetFaceResult
	if err := verifyNativeTargetFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	before, err := s.nativeTargetFaceResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	out := nativeTargetFaceResult{planSeal: plan.seal, operation: plan.prepared.operation}
	if out.operation == "face.parse" {
		out.pixels, err = s.faceParsePreparationPixels(ctx, who, plan.shell.Tenant, plan.detector.anchor.entry.Binding, before.Outputs[1], plan.prepared.detector.Output, before.Outputs[0])
		if err != nil {
			return zero, err
		}
		parsed, err := s.readCompletedNativeTargetFaceParse(ctx, who, plan, color)
		if err != nil {
			return zero, err
		}
		out.catalog, out.body, out.paramsBody, out.labels, out.counts, out.objects = parsed.catalog, parsed.body, parsed.paramsBody, parsed.labels, parsed.counts, parsed.objects
		after, err := s.faceParsePreparationPixels(ctx, who, plan.shell.Tenant, plan.detector.anchor.entry.Binding, before.Outputs[1], plan.prepared.detector.Output, before.Outputs[0])
		if err != nil {
			return zero, err
		}
		if after != out.pixels {
			return zero, v2input.ErrConflict
		}
	} else if out.operation == "face.landmarks" {
		parsed, err := s.readCompletedNativeTargetLandmarks(ctx, who, plan, color)
		if err != nil {
			return zero, err
		}
		out.catalog, out.body, out.paramsBody, out.landmarks, out.objects = parsed.catalog, parsed.body, parsed.paramsBody, parsed.faces, parsed.objects
	} else {
		return zero, v2input.ErrInvalid
	}
	beforeSHA, err := canonical.Hash(before)
	if err != nil {
		return zero, err
	}
	afterSHA, err := canonical.Hash(out.catalog)
	if err != nil || beforeSHA != afterSHA {
		return zero, v2input.ErrConflict
	}
	out.detector, err = s.readNativeTargetDetectorResult(ctx, who, plan.detector, color)
	if err != nil {
		return zero, err
	}
	if out.detector.seal != plan.result.seal {
		return zero, v2input.ErrConflict
	}
	out.seal, err = targetFaceResultDigest(out)
	if err != nil {
		return zero, err
	}
	return out, ctx.Err()
}

// Caller owns the original parent advisory lock and rollback. No object IO.
func (s SQLTransferSourceReader) lockNativeTargetFaceResult(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetFaceDraftPlan, built, detectorBuilt v2input.Built, p nativeTargetFaceResult) error {
	digest, err := targetFaceResultDigest(p)
	if err != nil {
		return err
	}
	if tx == nil || p.seal == "" || digest != p.seal || p.planSeal != plan.seal || p.operation != plan.prepared.operation || p.detector.seal != plan.result.seal {
		return v2input.ErrConflict
	}
	packet, err := s.marshalNativeTargetFaceAdmissionEvidence(ctx, who, original, plan, built, detectorBuilt)
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
	outputs, err := canonical.Marshal(p.catalog.Outputs)
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_target_face_result($1,$2,$3::jsonb,$4,$5::jsonb)", plan.shell.Tenant, plan.shell.Run, string(packet), p.catalog.ResultID, string(outputs)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Complete only this prerequisite run after current physical result authority.
func (s SQLTransferSourceReader) finishNativeTargetFaceInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetFaceDraftPlan, built, detectorBuilt v2input.Built, proof nativeTargetFaceResult) error {
	if err := s.lockNativeTargetFaceResult(ctx, tx, who, original, plan, built, detectorBuilt, proof); err != nil {
		return err
	}
	packet, err := s.marshalNativeTargetFaceAdmissionEvidence(ctx, who, original, plan, built, detectorBuilt)
	if err != nil {
		return err
	}
	outputs, err := canonical.Marshal(proof.catalog.Outputs)
	if err != nil {
		return err
	}
	var finished bool
	if err := tx.QueryRow(ctx, "SELECT photo.finish_semantic_target_face($1,$2,$3::jsonb,$4,$5::jsonb)", plan.shell.Tenant, plan.shell.Run, string(packet), proof.catalog.ResultID, string(outputs)).Scan(&finished); err != nil {
		return err
	}
	if !finished {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
