package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type nativeTargetDefectPrerequisites struct {
	person     nativeTargetPersonPlan
	landmarks  nativeTargetFaceDraftPlan
	skin       nativeTargetSkinDraftPlan
	transports nativeTargetDefectTransports
}

// Restore only exact completed prerequisite plans and persisted transports.
// Caller holds the original parent lock; all physical IO precedes SQL locks.
func (p TransferDraftPreparer) restoreNativeTargetDefectPrerequisites(ctx context.Context, who auth.Identity, d detectorDraftPlan, manifest nativeTargetPreparationManifest, ordinal int, catalogs NativeParentArtifactCatalogFactory) (nativeTargetDefectPrerequisites, error) {
	var zero, out nativeTargetDefectPrerequisites
	anchor := func(op string) (nativeTargetAnchor, error) {
		return compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, manifest, ordinal, op)
	}
	a, err := anchor("mask.person")
	if err != nil {
		return zero, err
	}
	catalog, err := catalogs(ctx, who, a.request.TenantID, p.Reader.ProjectID, a.entry.Binding.AssetVersionID)
	if err != nil {
		return zero, err
	}
	person, err := p.Reader.readNativeTargetPersonSaved(ctx, who, a)
	if err != nil {
		return zero, err
	}
	if person.State != "saved" || person.RunState != "completed" {
		return zero, v2input.ErrConflict
	}
	out.person, err = p.Reader.restoreNativeTargetPersonPlan(ctx, catalog, who, a, p.Configuration.Color, person)
	if err != nil {
		return zero, err
	}
	a, err = anchor("face.detect")
	if err != nil {
		return zero, err
	}
	detectorSaved, err := p.Reader.readNativeTargetDetectorSaved(ctx, who, a)
	if err != nil {
		return zero, err
	}
	if detectorSaved.State != "saved" || detectorSaved.RunState != "completed" {
		return zero, v2input.ErrConflict
	}
	detector, err := p.Reader.restoreNativeTargetDetectorPlan(ctx, catalog, who, a, p.Configuration.Color, detectorSaved)
	if err != nil {
		return zero, err
	}
	face := func(op string) (nativeTargetFaceDraftPlan, error) {
		a, err := anchor(op)
		if err != nil {
			return nativeTargetFaceDraftPlan{}, err
		}
		saved, err := p.Reader.readNativeTargetFaceSaved(ctx, who, a)
		if err != nil {
			return nativeTargetFaceDraftPlan{}, err
		}
		if saved.State != "saved" || saved.RunState != "completed" {
			return nativeTargetFaceDraftPlan{}, v2input.ErrConflict
		}
		return p.Reader.restoreNativeTargetFacePlan(ctx, catalog, who, detector, a, p.Configuration.Color, saved)
	}
	out.landmarks, err = face("face.landmarks")
	if err != nil {
		return zero, err
	}
	parser, err := face("face.parse")
	if err != nil {
		return zero, err
	}
	a, err = anchor("mask.face_skin")
	if err != nil {
		return zero, err
	}
	skin, err := p.Reader.readNativeTargetSkinSaved(ctx, who, a)
	if err != nil {
		return zero, err
	}
	if skin.State != "saved" || skin.RunState != "completed" {
		return zero, v2input.ErrConflict
	}
	out.skin, err = p.Reader.restoreNativeTargetSkinPlan(ctx, who, parser, a, p.Configuration.Color, skin)
	if err != nil {
		return zero, err
	}
	err = database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
		var err error
		out.transports.person, err = p.Reader.recoverNativeTargetPersonTransport(ctx, tx, who, d, out.person)
		if err != nil {
			return err
		}
		out.transports.detector, err = p.Reader.recoverNativeTargetDetectorTransport(ctx, tx, who, d, detector)
		if err != nil {
			return err
		}
		out.transports.landmarks, err = p.Reader.recoverNativeTargetFaceTransport(ctx, tx, who, d, out.landmarks)
		if err != nil {
			return err
		}
		out.transports.parser, err = p.Reader.recoverNativeTargetFaceTransport(ctx, tx, who, d, parser)
		if err != nil {
			return err
		}
		out.transports.skin, err = p.Reader.recoverNativeTargetSkinTransport(ctx, tx, who, d, out.skin)
		return err
	})
	if err != nil {
		return zero, err
	}
	return out, ctx.Err()
}
