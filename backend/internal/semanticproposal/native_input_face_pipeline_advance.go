package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeFacePipelineAdvance struct {
	phase     nativeInputPreparationPhase
	operation string
	producers nativeInputRestoredFaceProducers
}

// One bounded advancement of original saved producer stages. No new draft or
// object is synthesized from a missing checkpoint. The durable caller retains
// scheduling ownership and the parent lock. A registered blocked stage is sealed
// using its exact restored plan; child receipts never leave this boundary.
func (s SQLTransferSourceReader) advanceSavedNativeFacePipeline(ctx context.Context, scheduler *pgxpool.Pool, catalog v2draft.ArtifactCatalog, who auth.Identity, anchor nativePreparationAnchor) (nativeFacePipelineAdvance, error) {
	return s.advanceNativeFacePipeline(ctx, scheduler, catalog, who, anchor, nil)
}

// Creation is an explicit server-owned capability; saved recovery retains its
// original no-publication behavior when no starter is supplied.
type nativeFacePipelineStarter func(context.Context, detectorDraftPlan, nativeFaceDraftPlan, string) error

func (s SQLTransferSourceReader) advanceNativeFacePipeline(ctx context.Context, scheduler *pgxpool.Pool, catalog v2draft.ArtifactCatalog, who auth.Identity, anchor nativePreparationAnchor, start nativeFacePipelineStarter) (nativeFacePipelineAdvance, error) {
	var zero nativeFacePipelineAdvance
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil || catalog == nil || s.DB == nil || s.Objects == nil {
		return zero, v2input.ErrInvalid
	}
	request, err := ownNativePlanRequest(ctx, who, anchor.Request, anchor.Key)
	if err != nil {
		return zero, err
	}
	state := func(operation string) (nativeInputPreparationPhase, error) {
		observation, err := s.readNativeInputSavedStageState(ctx, who, request, anchor.Published.Ref, anchor.Snapshot, operation)
		if err != nil {
			return "", err
		}
		return nativeInputStagePreparationPhase(observation)
	}
	pending := func(operation string, phase nativeInputPreparationPhase) nativeFacePipelineAdvance {
		return nativeFacePipelineAdvance{phase: phase, operation: operation}
	}
	phase, err := state("face.detect")
	if err != nil {
		return zero, err
	}
	if phase == nativePreparationWaiting || phase == nativePreparationFailed {
		return pending("face.detect", phase), nil
	}
	detector, err := s.restoreDetectorDraftPlan(ctx, catalog, who, request, anchor.Key, anchor.Published, anchor.Snapshot, append([]byte(nil), anchor.SnapshotBody...), anchor.Color)
	if err != nil {
		return zero, err
	}
	if phase == nativePreparationBlocked {
		if _, err := s.resumeDetectorPreparation(ctx, scheduler, who, detector); err != nil {
			return zero, err
		}
		return pending("face.detect", nativePreparationWaiting), nil
	}
	completed, err := s.readCompletedDetectorPreparation(ctx, who, detector, anchor.Color)
	if err != nil {
		return zero, err
	}
	producers := nativeInputRestoredFaceProducers{detector: detector}
	for _, operation := range []string{"face.landmarks", "face.parse"} {
		if start != nil {
			presence, err := s.readNativeStagePresence(ctx, who, detector, operation)
			if err != nil {
				return zero, err
			}
			if presence == nativeStageConflict {
				return zero, v2input.ErrConflict
			}
			if presence == nativeStageAbsent {
				if err := start(ctx, detector, nativeFaceDraftPlan{}, operation); err != nil {
					return zero, err
				}
				return pending(operation, nativePreparationWaiting), nil
			}
		}
		phase, err = state(operation)
		if err != nil {
			return zero, err
		}
		if phase == nativePreparationWaiting || phase == nativePreparationFailed {
			return pending(operation, phase), nil
		}
		plan, err := s.restoreNativeFaceDraftPlan(ctx, catalog, who, detector, anchor.Color, operation)
		if err != nil {
			return zero, err
		}
		if phase == nativePreparationBlocked {
			if start != nil {
				if err := start(ctx, detector, nativeFaceDraftPlan{}, operation); err != nil {
					return zero, err
				}
				return pending(operation, nativePreparationWaiting), nil
			}
			if _, err := s.resumeNativeFacePreparation(ctx, scheduler, who, plan, completed); err != nil {
				return zero, err
			}
			return pending(operation, nativePreparationWaiting), nil
		}
		if operation == "face.landmarks" {
			if _, err := s.readCompletedNativeLandmarks(ctx, who, plan, anchor.Color); err != nil {
				return zero, err
			}
			producers.landmarks = plan
		} else {
			if _, err := s.readCompletedNativeFaceParse(ctx, who, plan, anchor.Color); err != nil {
				return zero, err
			}
			producers.parser = plan
		}
	}
	if start != nil {
		presence, err := s.readNativeStagePresence(ctx, who, detector, "mask.face_skin")
		if err != nil {
			return zero, err
		}
		if presence == nativeStageConflict {
			return zero, v2input.ErrConflict
		}
		if presence == nativeStageAbsent {
			if err := start(ctx, detector, producers.parser, "mask.face_skin"); err != nil {
				return zero, err
			}
			return pending("mask.face_skin", nativePreparationWaiting), nil
		}
	}
	phase, err = state("mask.face_skin")
	if err != nil {
		return zero, err
	}
	if phase == nativePreparationWaiting || phase == nativePreparationFailed {
		return pending("mask.face_skin", phase), nil
	}
	skin, err := s.restoreNativeSkinDraftPlan(ctx, who, producers.parser, anchor.Color)
	if err != nil {
		return zero, err
	}
	if phase == nativePreparationBlocked {
		if start != nil {
			if err := start(ctx, detector, producers.parser, "mask.face_skin"); err != nil {
				return zero, err
			}
			return pending("mask.face_skin", nativePreparationWaiting), nil
		}
		if _, err := s.resumeNativeSkinPreparation(ctx, scheduler, who, skin); err != nil {
			return zero, err
		}
		return pending("mask.face_skin", nativePreparationWaiting), nil
	}
	if _, err := s.readCompletedNativeSkin(ctx, who, skin, anchor.Color); err != nil {
		return zero, err
	}
	// Reconstruct all original plans again after later IO to reject changed saved
	// configurations. Complete downstream admission still locks physical evidence.
	restored, err := s.restoreNativeInputFaceProducers(ctx, catalog, who, request, anchor.Key, anchor.Published, anchor.Snapshot, anchor.SnapshotBody, anchor.Color)
	if err != nil {
		return zero, err
	}
	if restored.detector.seal != detector.seal || restored.landmarks.seal != producers.landmarks.seal || restored.parser.seal != producers.parser.seal || restored.skin.seal != skin.seal {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeFacePipelineAdvance{phase: nativePreparationSucceeded, producers: restored}, nil
}
