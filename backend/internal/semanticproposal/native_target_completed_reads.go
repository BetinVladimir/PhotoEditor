package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
	"slices"
)

func (s SQLTransferSourceReader) readCompletedNativeTargetDetector(ctx context.Context, who auth.Identity, p nativeTargetDetectorPlan, c TransferColorProfile) (completedNativeDetector, error) {
	if err := verifyNativeTargetDetectorPlan(ctx, p); err != nil {
		return completedNativeDetector{}, s.nativeReadFailure(err)
	}
	return nativeRead(ctx, s, who, c, nativeReadKey{"target-detector", p.seal}, cloneNativeDetector, func(r SQLTransferSourceReader) (completedNativeDetector, error) {
		return r.readCompletedNativeTargetDetectorUncached(ctx, who, p, c)
	}, func(r SQLTransferSourceReader, v completedNativeDetector) func(context.Context) error {
		return func(ctx context.Context) error {
			current, err := r.nativeTargetDetectorResultCatalogCurrent(ctx, who, p)
			if err != nil {
				return err
			}
			if current != v.catalog {
				return v2input.ErrConflict
			}
			objects, err := r.facePreparationObjects(ctx, who, p.detector.request.TenantID, p.anchor.entry.Binding, v.catalog.Output, "face.detect")
			if err != nil {
				return err
			}
			if objects != v.objects {
				return v2input.ErrConflict
			}
			if err := r.recheckSourcePreparationProof(ctx, who, p.detector.request.TenantID, p.anchor.entry.Binding, c, true, p.detector.prepared.source); err != nil {
				return err
			}
			selected, err := selectNativeQualificationPolicies(p.detector.policy, []string{"face.detect"})
			if err != nil {
				return err
			}
			if err := r.recheckDetectorQualificationProof(ctx, who, p.detector.request.TenantID, p.detector.prepared.qualification, selected["face.detect"]); err != nil {
				return err
			}
			return r.recheckNativePlanPolicy(ctx, who, p.detector.request.TenantID, p.detector.policy)
		}
	})
}

func cloneNativeTargetFace(v nativeTargetFaceResult) nativeTargetFaceResult {
	v.catalog.Outputs = slices.Clone(v.catalog.Outputs)
	v.detector.completed = cloneNativeDetector(v.detector.completed)
	v.landmarks = slices.Clone(v.landmarks)
	for i := range v.landmarks {
		v.landmarks[i].Points = slices.Clone(v.landmarks[i].Points)
		v.landmarks[i].TransformChain = slices.Clone(v.landmarks[i].TransformChain)
	}
	v.labels.TransformChain = slices.Clone(v.labels.TransformChain)
	v.labels.Warnings = slices.Clone(v.labels.Warnings)
	return v
}
func (s SQLTransferSourceReader) readNativeTargetFaceResult(ctx context.Context, who auth.Identity, p nativeTargetFaceDraftPlan, c TransferColorProfile) (nativeTargetFaceResult, error) {
	if err := verifyNativeTargetFacePlanSeal(ctx, p); err != nil {
		return nativeTargetFaceResult{}, s.nativeReadFailure(err)
	}
	return nativeRead(ctx, s, who, c, nativeReadKey{"target-face-" + p.prepared.operation, p.seal}, cloneNativeTargetFace, func(r SQLTransferSourceReader) (nativeTargetFaceResult, error) {
		return r.readNativeTargetFaceResultUncached(ctx, who, p, c)
	}, func(r SQLTransferSourceReader, v nativeTargetFaceResult) func(context.Context) error {
		return func(ctx context.Context) error {
			current, err := r.nativeTargetFaceResultCatalogCurrent(ctx, who, p)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, v.catalog) {
				return v2input.ErrConflict
			}
			manifest, err := nativeTargetFaceManifest(p.prepared.operation, v.catalog.Outputs)
			if err != nil {
				return err
			}
			objects, err := r.facePreparationObjects(ctx, who, p.shell.Tenant, p.detector.anchor.entry.Binding, manifest, p.prepared.operation)
			if err != nil {
				return err
			}
			if objects != v.objects {
				return v2input.ErrConflict
			}
			if p.prepared.operation == "face.parse" {
				pixels, err := r.faceParsePreparationPixels(ctx, who, p.shell.Tenant, p.detector.anchor.entry.Binding, v.catalog.Outputs[1], v.detector.completed.catalog.Output, v.catalog.Outputs[0])
				if err != nil {
					return err
				}
				if err := verifyNativeParsePixelPin(v.pixels, pixels); err != nil {
					return err
				}
			}
			var expected v2qualification.Expected
			if json.Unmarshal([]byte(p.prepared.expected), &expected) != nil {
				return v2input.ErrInvalid
			}
			report, err := r.faceQualificationObject(ctx, who, p.shell.Tenant, expected, p.prepared.report.ref)
			if err != nil {
				return err
			}
			if report != p.prepared.report {
				return v2input.ErrConflict
			}
			return nil
		}
	})
}

func cloneNativeTargetSkin(v completedNativeTargetSkin) completedNativeTargetSkin {
	v.catalog.Outputs = slices.Clone(v.catalog.Outputs)
	v.parser = cloneNativeTargetFace(v.parser)
	v.mask = cloneNativeMask(v.mask)
	return v
}
func (s SQLTransferSourceReader) readCompletedNativeTargetSkin(ctx context.Context, who auth.Identity, p nativeTargetSkinDraftPlan, c TransferColorProfile) (completedNativeTargetSkin, error) {
	if err := verifyNativeTargetSkinPlanSeal(ctx, p); err != nil {
		return completedNativeTargetSkin{}, s.nativeReadFailure(err)
	}
	return nativeRead(ctx, s, who, c, nativeReadKey{"target-skin", p.seal}, cloneNativeTargetSkin, func(r SQLTransferSourceReader) (completedNativeTargetSkin, error) {
		return r.readCompletedNativeTargetSkinUncached(ctx, who, p, c)
	}, func(r SQLTransferSourceReader, v completedNativeTargetSkin) func(context.Context) error {
		return func(ctx context.Context) error {
			current, err := r.nativeTargetSkinResultCatalogCurrent(ctx, who, p)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, v.catalog) {
				return v2input.ErrConflict
			}
			objects, err := r.nativeTargetSkinResultObjects(ctx, who, p, current)
			if err != nil {
				return err
			}
			if objects != v.objects {
				return v2input.ErrConflict
			}
			labels, err := r.faceParsePreparationPixels(ctx, who, p.shell.Tenant, p.parser.detector.anchor.entry.Binding, v.parser.catalog.Outputs[1], v.parser.detector.completed.catalog.Output, v.parser.catalog.Outputs[0])
			if err != nil {
				return err
			}
			return verifyNativeParsePixelPin(v.parser.pixels, labels)
		}
	})
}

func cloneNativeTargetDefect(v completedNativeTargetDefect) completedNativeTargetDefect {
	copied := cloneNativeDefect(completedNativeInputDefect{catalog: v.catalog, objects: v.objects, evidence: v.evidence})
	v.catalog = copied.catalog
	v.evidence = copied.evidence
	return v
}
func (s SQLTransferSourceReader) readCompletedNativeTargetDefect(ctx context.Context, who auth.Identity, p nativeTargetDefectDraftPlan, c TransferColorProfile) (completedNativeTargetDefect, error) {
	if err := verifyNativeTargetDefectPlanSeal(ctx, p); err != nil {
		return completedNativeTargetDefect{}, s.nativeReadFailure(err)
	}
	return nativeRead(ctx, s, who, c, nativeReadKey{"target-defect", p.seal}, cloneNativeTargetDefect, func(r SQLTransferSourceReader) (completedNativeTargetDefect, error) {
		return r.readCompletedNativeTargetDefectUncached(ctx, who, p, c)
	}, func(r SQLTransferSourceReader, v completedNativeTargetDefect) func(context.Context) error {
		return func(ctx context.Context) error {
			current, err := r.nativeTargetDefectResultCatalogCurrent(ctx, who, p)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, v.catalog) {
				return v2input.ErrConflict
			}
			objects, err := r.nativeTargetDefectResultObjects(ctx, who, p, current)
			if err != nil {
				return err
			}
			if objects != v.objects {
				return v2input.ErrConflict
			}
			prep := p.prepared.preparation
			q := p.prepared.qualification
			if err := verifyNativeTargetDefectPreparation(ctx, prep); err != nil {
				return err
			}
			if err := r.verifyNativeTargetDefectQualification(ctx, who, prep, q); err != nil {
				return err
			}
			person, err := r.readNativeTargetPersonResult(ctx, who, prep.personPlan)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(person, prep.person) {
				return v2input.ErrConflict
			}
			selected, err := selectNativeQualificationPolicies(q.policy, []string{"defects.propose"})
			if err != nil {
				return err
			}
			if err := r.recheckDefectQualificationPreparation(ctx, who, prep.landmarksPlan.shell.Tenant, q.report, selected["defects.propose"]); err != nil {
				return err
			}
			return r.recheckNativePlanPolicy(ctx, who, prep.landmarksPlan.shell.Tenant, q.policy)
		}
	})
}

// Canonical output order is operation-specific, never an unordered set.
func nativeTargetFaceManifest(operation string, outputs []v2input.Ref) (v2input.Ref, error) {
	switch operation {
	case "face.landmarks":
		if len(outputs) == 1 {
			return outputs[0], nil
		}
	case "face.parse":
		if len(outputs) == 2 {
			return outputs[1], nil
		}
	}
	return v2input.Ref{}, v2input.ErrConflict
}
