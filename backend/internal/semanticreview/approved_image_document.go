package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
)

// ReadApprovedImageDocument binds target/reference image to private review,
// reads exact versioned bytes and repeats authenticated metadata after IO.
// RGB/alpha/geometry children require separate scope and physical verification.
func ReadApprovedImageDocument(ctx context.Context, db ApprovedProtectionQueryer,
	tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	targetID uuid.UUID, reference bool, read PlanObjectReader) (PlanDocument, error) {
	empty := PlanDocument{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || read == nil || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return empty, ErrInvalid
	}
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil {
		return empty, err
	}
	var selected *ApprovedEdit
	for i := range edits {
		if edits[i].TargetID == targetID {
			selected = &edits[i]
		}
	}
	if selected == nil {
		return empty, ErrInvalid
	}
	pin := selected.Source.Source
	if reference {
		owned, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
		if err != nil {
			return empty, err
		}
		pin = owned.Intent.Reference.Source
	}
	var revision int64
	for _, target := range targets {
		if target.ID == targetID {
			revision = target.Revision
		}
	}
	args := []any{tenant, intent, actor, session, prepared.Sources.Revision, targetID, revision, selected.Source.AssetVersionID, selected.Candidate.ArtifactID, selected.Candidate.SHA256, reference, pin.ArtifactID, pin.SHA256}
	load := func() (PlanSource, error) {
		var source PlanSource
		err := db.QueryRow(ctx, "SELECT contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_approved_image_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", args...).Scan(&source.Contract, &source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bytes, &source.Bucket, &source.Key, &source.Version, &source.MediaType)
		if err != nil {
			return PlanSource{}, err
		}
		if source.Contract != "WorkingImage" || source.Ref != pin || source.Bytes < 2 || source.Bytes > 8388608 || source.Bucket == "" || source.Key == "" || source.Version == "" || source.Version == "null" || source.MediaType != "application/json" {
			return PlanSource{}, ErrInvalid
		}
		return source, nil
	}
	before, err := load()
	if err != nil {
		return empty, err
	}
	raw, err := read(ctx, before)
	if err != nil {
		return empty, err
	}
	if int64(len(raw)) != before.Bytes {
		return empty, ErrInvalid
	}
	raw = append([]byte(nil), raw...)
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != before.Bytes || hex.EncodeToString(sum[:]) != pin.SHA256 {
		return empty, ErrInvalid
	}
	after, err := load()
	if err != nil {
		return empty, err
	}
	if before != after {
		return empty, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return PlanDocument{Source: before, Body: raw}, nil
}
