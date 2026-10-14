package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

// VerifyApprovedProtectionBinary streams the exact authorized object, checks
// float32 C-order mask shape/finite unit values and SHA, then reauthorizes its
// canonical producer and metadata. No array is retained in RAM or returned.
func VerifyApprovedProtectionBinary(ctx context.Context, db ApprovedProtectionQueryer, objects planObjectStore, bucket string,
	tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	targetID uuid.UUID, document PlanDocument) (PlanSource, error) {
	return verifyApprovedProtectionBinary(ctx, db, objects, bucket, tenant, intent, actor, session, prepared, targets, request, targetID, document, true)
}

func verifyApprovedProtectionBinary(ctx context.Context, db ApprovedProtectionQueryer, objects planObjectStore, bucket string,
	tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	targetID uuid.UUID, document PlanDocument, physical bool) (PlanSource, error) {
	empty := PlanSource{}
	if objects == nil || bucket == "" || len(document.Body) > 8388608 {
		return empty, ErrInvalid
	}
	document.Body = append([]byte(nil), document.Body...)
	binary, err := VerifyApprovedProtectionOrigin(ctx, db, tenant, intent, actor, session, prepared, targets, request, targetID, document)
	if err != nil {
		return empty, err
	}
	mask, err := review.ParseMaskRef(document.Body)
	if err != nil {
		return empty, ErrInvalid
	}
	width, height := int64(mask.Canvas.Width), int64(mask.Canvas.Height)
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return empty, ErrInvalid
	}
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil {
		return empty, err
	}
	var selected *ApprovedEdit
	var revision int64
	for i := range edits {
		if edits[i].TargetID == targetID {
			selected = &edits[i]
		}
	}
	for _, target := range targets {
		if target.ID == targetID {
			revision = target.Revision
		}
	}
	if selected == nil {
		return empty, ErrInvalid
	}
	args := []any{tenant, intent, actor, session, prepared.Sources.Revision, targetID, revision, selected.Source.AssetVersionID, selected.Candidate.ArtifactID, selected.Candidate.SHA256, document.Source.Ref.ArtifactID, document.Source.Ref.SHA256, binary.ArtifactID, binary.SHA256, string(document.Body)}
	load := func() (PlanSource, error) {
		var source PlanSource
		err := db.QueryRow(ctx, "SELECT contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_approved_protection_binary($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)", args...).Scan(&source.Contract, &source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bytes, &source.Bucket, &source.Key, &source.Version, &source.MediaType)
		if err != nil {
			return empty, err
		}
		pixels := width * height
		if source.Contract != "NpyArray" || source.Ref != binary || source.Bucket != bucket || source.Key == "" || source.Version == "" || source.Version == "null" || source.Bytes < 32 || pixels < 1 || pixels > 200000000 || source.Bytes > 4*pixels+16384 || (source.MediaType != "application/octet-stream" && source.MediaType != "application/x-npy") {
			return empty, ErrInvalid
		}
		return source, nil
	}
	before, err := load()
	if err != nil {
		return empty, err
	}
	if !physical {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		return before, nil
	}
	stream, err := objects.Open(ctx, objectstore.Object{Key: before.Key, VersionID: before.Version, SHA256: before.Ref.SHA256, Bytes: before.Bytes, ContentType: before.MediaType})
	if err != nil {
		return empty, err
	}
	if stream == nil {
		return empty, ErrInvalid
	}
	verifyErr := masknpy.VerifyFloat32MaskNPY(ctx, stream, before.Bytes, before.Ref.SHA256, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	closeErr := stream.Close()
	if errors.Is(verifyErr, masknpy.ErrInvalid) {
		return empty, ErrInvalid
	}
	if verifyErr != nil {
		return empty, verifyErr
	}
	if closeErr != nil {
		return empty, closeErr
	}
	if err := ctx.Err(); err != nil {
		return empty, err
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
	return before, nil
}
