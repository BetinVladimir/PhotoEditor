package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ResolveApprovedImageBinaryMetadata proves an exact child of an authenticated
// source document. It does not read or numerically validate NPY bytes. Consumers
// must repeat this authority after physical IO and before atomic admission.
func ResolveApprovedImageBinaryMetadata(ctx context.Context, db ApprovedProtectionQueryer,
	tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	targetID uuid.UUID, reference bool, document PlanDocument, kind string, step int) (PlanSource, error) {
	empty := PlanSource{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
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
	binding := selected.Source
	if reference {
		owned, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
		if err != nil {
			return empty, err
		}
		binding = owned.Intent.Reference
	}
	if document.Source.Contract != "WorkingImage" || document.Source.Ref != binding.Source || int64(len(document.Body)) != document.Source.Bytes {
		return empty, ErrInvalid
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, document.Body)
	if err != nil {
		return empty, ErrInvalid
	}
	pin := metadata.Inputs.Pixels
	components, pixels := int64(3), metadata.Width*metadata.Height
	switch kind {
	case "pixels":
		if step != -1 {
			return empty, ErrInvalid
		}
	case "alpha":
		if step != -1 || metadata.Inputs.Alpha == nil {
			return empty, ErrInvalid
		}
		pin = *metadata.Inputs.Alpha
		components = 1
	case "inverse_field":
		var chain struct {
			Steps []struct {
				Kind    string        `json:"kind"`
				Inverse *v2input.Ref  `json:"inverse_field"`
				To      review.Canvas `json:"to_canvas"`
			}
		}
		image, err := review.ParseWorkingImage(document.Body)
		if err != nil || json.Unmarshal(image.TransformChain, &chain) != nil || step < 0 || step >= len(chain.Steps) || step >= 32 {
			return empty, ErrInvalid
		}
		chosen := chain.Steps[step]
		if chosen.Kind != "dense_warp" || chosen.Inverse == nil {
			return empty, ErrInvalid
		}
		pin = *chosen.Inverse
		components = 2
		width, height := int64(chosen.To.Width), int64(chosen.To.Height)
		if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
			return empty, ErrInvalid
		}
		pixels = width * height
	default:
		return empty, ErrInvalid
	}
	var revision int64
	for _, target := range targets {
		if target.ID == targetID {
			revision = target.Revision
		}
	}
	var source PlanSource
	err = db.QueryRow(ctx, "SELECT contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_approved_image_binary($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)", tenant, intent, actor, session, prepared.Sources.Revision, targetID, revision, selected.Source.AssetVersionID, selected.Candidate.ArtifactID, selected.Candidate.SHA256, reference, binding.Source.ArtifactID, binding.Source.SHA256, string(document.Body), kind, step).Scan(&source.Contract, &source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bytes, &source.Bucket, &source.Key, &source.Version, &source.MediaType)
	if err != nil {
		return empty, err
	}
	if source.Contract != "NpyArray" || source.Ref != pin || source.Bytes < 32 || source.Bytes > 4*components*pixels+16384 || source.Bucket == "" || source.Key == "" || source.Version == "" || source.Version == "null" || (source.MediaType != "application/octet-stream" && source.MediaType != "application/x-npy") {
		return empty, ErrInvalid
	}
	// Immutable legacy body-warp inputs use octet-stream. The array reader
	// still verifies exact SHA, NPY layout, float32 XY shape and finite values.
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return source, nil
}
