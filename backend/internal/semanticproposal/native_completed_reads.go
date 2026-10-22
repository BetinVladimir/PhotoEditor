package semanticproposal

import (
	"context"
	"reflect"
	"slices"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func cloneNativeDetector(v completedNativeDetector) completedNativeDetector {
	v.boxes.Faces = slices.Clone(v.boxes.Faces)
	v.boxes.Warnings = slices.Clone(v.boxes.Warnings)
	return v
}
func cloneNativeLandmarks(v completedNativeLandmarks) completedNativeLandmarks {
	v.catalog.Outputs = slices.Clone(v.catalog.Outputs)
	v.detector = cloneNativeDetector(v.detector)
	v.faces = slices.Clone(v.faces)
	for i := range v.faces {
		v.faces[i].Points = slices.Clone(v.faces[i].Points)
		v.faces[i].TransformChain = slices.Clone(v.faces[i].TransformChain)
	}
	return v
}
func cloneNativeParse(v completedNativeFaceParse) completedNativeFaceParse {
	v.catalog.Outputs = slices.Clone(v.catalog.Outputs)
	v.detector = cloneNativeDetector(v.detector)
	v.labels.TransformChain = slices.Clone(v.labels.TransformChain)
	v.labels.Warnings = slices.Clone(v.labels.Warnings)
	return v
}
func cloneNativeMask(v review.ProtectionMask) review.ProtectionMask {
	v.TransformChain = slices.Clone(v.TransformChain)
	if v.ObjectID != nil {
		x := *v.ObjectID
		v.ObjectID = &x
	}
	if v.ModelLockID != nil {
		x := *v.ModelLockID
		v.ModelLockID = &x
	}
	return v
}
func cloneNativeSkin(v completedNativeSkin) completedNativeSkin {
	v.catalog.Outputs = slices.Clone(v.catalog.Outputs)
	v.parser = cloneNativeParse(v.parser)
	v.mask = cloneNativeMask(v.mask)
	return v
}
func cloneNativeDefect(v completedNativeInputDefect) completedNativeInputDefect {
	v.catalog.Outputs = slices.Clone(v.catalog.Outputs)
	v.evidence.Params = slices.Clone(v.evidence.Params)
	v.evidence.Proposal = slices.Clone(v.evidence.Proposal)
	v.evidence.Report = slices.Clone(v.evidence.Report)
	v.evidence.FaceMasks = slices.Clone(v.evidence.FaceMasks)
	for i := range v.evidence.FaceMasks {
		m := &v.evidence.FaceMasks[i]
		m.Mask = cloneNativeMask(m.Mask)
		if m.ObjectID != nil {
			x := *m.ObjectID
			m.ObjectID = &x
		}
	}
	return v
}

func (s SQLTransferSourceReader) readCompletedDetectorPreparation(ctx context.Context, who auth.Identity, p detectorDraftPlan, c TransferColorProfile) (completedNativeDetector, error) {
	if err := verifyDetectorPlanSeal(ctx, p); err != nil {
		return completedNativeDetector{}, s.nativeReadFailure(err)
	}
	return nativeRead(ctx, s, who, c, nativeReadKey{"detector", p.seal}, cloneNativeDetector, func(r SQLTransferSourceReader) (completedNativeDetector, error) {
		return r.readCompletedDetectorPreparationUncached(ctx, who, p, c)
	}, func(r SQLTransferSourceReader, v completedNativeDetector) func(context.Context) error {
		return func(ctx context.Context) error {
			current, err := r.nativeDetectorResultCatalogCurrent(ctx, who, p)
			if err != nil {
				return err
			}
			if current != v.catalog {
				return v2input.ErrConflict
			}
			objects, err := r.facePreparationObjects(ctx, who, p.request.TenantID, p.request.Intent.Reference, v.catalog.Output, "face.detect")
			if err != nil {
				return err
			}
			if objects != v.objects {
				return v2input.ErrConflict
			}
			if err := r.recheckSourcePreparationProof(ctx, who, p.request.TenantID, p.request.Intent.Reference, c, true, p.prepared.source); err != nil {
				return err
			}
			selected, err := selectNativeQualificationPolicies(p.policy, []string{"face.detect"})
			if err != nil {
				return err
			}
			if err := r.recheckDetectorQualificationProof(ctx, who, p.request.TenantID, p.prepared.qualification, selected["face.detect"]); err != nil {
				return err
			}
			return r.recheckNativePlanPolicy(ctx, who, p.request.TenantID, p.policy)
		}
	})
}
func (s SQLTransferSourceReader) readCompletedNativeLandmarks(ctx context.Context, who auth.Identity, p nativeFaceDraftPlan, c TransferColorProfile) (completedNativeLandmarks, error) {
	if err := verifyNativeFacePlanSeal(ctx, p); err != nil {
		return completedNativeLandmarks{}, s.nativeReadFailure(err)
	}
	return nativeRead(ctx, s, who, c, nativeReadKey{"landmarks", p.seal}, cloneNativeLandmarks, func(r SQLTransferSourceReader) (completedNativeLandmarks, error) {
		return r.readCompletedNativeLandmarksUncached(ctx, who, p, c)
	}, func(r SQLTransferSourceReader, v completedNativeLandmarks) func(context.Context) error {
		return r.nativeFaceWitness(who, p, v.catalog, v.objects)
	})
}
func (s SQLTransferSourceReader) readCompletedNativeFaceParse(ctx context.Context, who auth.Identity, p nativeFaceDraftPlan, c TransferColorProfile) (completedNativeFaceParse, error) {
	if err := verifyNativeFacePlanSeal(ctx, p); err != nil {
		return completedNativeFaceParse{}, s.nativeReadFailure(err)
	}
	return nativeRead(ctx, s, who, c, nativeReadKey{"parse", p.seal}, cloneNativeParse, func(r SQLTransferSourceReader) (completedNativeFaceParse, error) {
		return r.readCompletedNativeFaceParseUncached(ctx, who, p, c)
	}, func(r SQLTransferSourceReader, v completedNativeFaceParse) func(context.Context) error {
		return func(ctx context.Context) error {
			if err := r.nativeFaceWitness(who, p, v.catalog, v.objects)(ctx); err != nil {
				return err
			}
			labels, err := r.faceParsePreparationPixels(ctx, who, p.shell.Tenant, p.detector.request.Intent.Reference, v.catalog.Outputs[1], v.detector.catalog.Output, v.catalog.Outputs[0])
			if err != nil {
				return err
			}
			// The mask-pixel descriptor is independently resolved as well as the manifest.
			if err := verifyNativeParsePixelPin(v.pixels, labels); err != nil {
				return err
			}
			return nil
		}
	})
}
func (s SQLTransferSourceReader) nativeFaceWitness(who auth.Identity, p nativeFaceDraftPlan, catalog nativeFaceResultCatalog, objects [2]defectEvidenceObject) func(context.Context) error {
	return func(ctx context.Context) error {
		current, err := s.nativeFaceResultCatalogCurrent(ctx, who, p)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, catalog) {
			return v2input.ErrConflict
		}
		output := 0
		if p.prepared.operation == "face.parse" {
			output = 1
		}
		pins, err := s.facePreparationObjects(ctx, who, p.shell.Tenant, p.detector.request.Intent.Reference, catalog.Outputs[output], p.prepared.operation)
		if err != nil {
			return err
		}
		if pins != objects {
			return v2input.ErrConflict
		}
		selected, err := selectNativeQualificationPolicies(p.detector.policy, []string{p.prepared.operation})
		if err != nil {
			return err
		}
		report, err := s.faceQualificationObject(ctx, who, p.shell.Tenant, selected[p.prepared.operation], p.prepared.report.ref)
		if err != nil {
			return err
		}
		if report != p.prepared.report {
			return v2input.ErrConflict
		}
		return nil
	}
}
func (s SQLTransferSourceReader) readCompletedNativeSkin(ctx context.Context, who auth.Identity, p nativeSkinDraftPlan, c TransferColorProfile) (completedNativeSkin, error) {
	if err := verifyNativeSkinPlanSeal(ctx, p); err != nil {
		return completedNativeSkin{}, s.nativeReadFailure(err)
	}
	return nativeRead(ctx, s, who, c, nativeReadKey{"skin", p.seal}, cloneNativeSkin, func(r SQLTransferSourceReader) (completedNativeSkin, error) {
		return r.readCompletedNativeSkinUncached(ctx, who, p, c)
	}, func(r SQLTransferSourceReader, v completedNativeSkin) func(context.Context) error {
		return func(ctx context.Context) error {
			current, err := r.nativeSkinResultCatalogCurrent(ctx, who, p)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, v.catalog) {
				return v2input.ErrConflict
			}
			objects, err := r.nativeSkinResultObjects(ctx, who, p, current)
			if err != nil {
				return err
			}
			if objects != v.objects {
				return v2input.ErrConflict
			}
			labels, err := r.faceParsePreparationPixels(ctx, who, p.shell.Tenant, p.parser.detector.request.Intent.Reference, v.parser.catalog.Outputs[1], v.parser.detector.catalog.Output, p.prepared.labels.ref)
			if err != nil {
				return err
			}
			if labels != p.prepared.labels {
				return v2input.ErrConflict
			}
			return nil
		}
	})
}
func (s SQLTransferSourceReader) readCompletedNativeInputDefect(ctx context.Context, who auth.Identity, p nativeDefectDraftPlan, c TransferColorProfile) (completedNativeInputDefect, error) {
	if err := verifyNativeDefectDraftSeal(ctx, p); err != nil {
		return completedNativeInputDefect{}, s.nativeReadFailure(err)
	}
	return nativeRead(ctx, s, who, c, nativeReadKey{"defect", p.seal}, cloneNativeDefect, func(r SQLTransferSourceReader) (completedNativeInputDefect, error) {
		return r.readCompletedNativeInputDefectUncached(ctx, who, p, c)
	}, func(r SQLTransferSourceReader, v completedNativeInputDefect) func(context.Context) error {
		return func(ctx context.Context) error {
			current, err := r.nativeInputDefectResultCatalogCurrent(ctx, who, p)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, v.catalog) {
				return v2input.ErrConflict
			}
			objects, err := r.nativeInputDefectResultObjects(ctx, who, p, current)
			if err != nil {
				return err
			}
			if objects != v.objects {
				return v2input.ErrConflict
			}
			q := p.prepared.qualification
			tenant := p.shell.Tenant
			if err := r.recheckNativePlanPolicy(ctx, who, tenant, q.policy); err != nil {
				return err
			}
			selected, err := selectNativeQualificationPolicies(q.policy, []string{"defects.propose"})
			if err != nil {
				return err
			}
			var expected v2qualification.Expected
			expected = selected["defects.propose"]
			report, err := r.defectQualificationObject(ctx, who, tenant, expected, q.report.report.ref)
			if err != nil {
				return err
			}
			if report != q.report.report {
				return v2input.ErrConflict
			}
			return r.recheckNativeDefectPersonInput(ctx, who, p.prepared.dependencies.preparation)
		}
	})
}

// Pin identity belongs to this parse result, not the union of other objects read
// by a phase (a second valid tracked version is not a replacement authority).
func verifyNativeParsePixelPin(verified, current defectEvidenceObject) error {
	if verified != current {
		return v2input.ErrConflict
	}
	return nil
}

func (s SQLTransferSourceReader) nativeReadFailure(err error) error {
	if s.nativePhase != nil {
		return s.nativePhase.fail(err)
	}
	return err
}
