package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type previewOutputQueryer interface {
	artifactjson.RowQuerier
	sourceQuerier
}

// PreviewImageRead verifies JSON binding and provenance; pixels are streamed through
// bounded physical NPY validation. Photographic/presentation qualification is separate.
type PreviewImageRead struct {
	Evidence      VerifiedPreviewEvidence
	Image, Pixels PlanSource
	Metadata      v2input.SemanticSourceMetadata
	ImageBytes    []byte
}

func ReadExposurePreviewImage(ctx context.Context, db previewOutputQueryer, store planObjectStore,
	bucket string, who auth.Identity, run uuid.UUID, params ExposurePreviewParams, sourceRaw []byte) (PreviewImageRead, error) {
	empty := PreviewImageRead{}
	if len(sourceRaw) < 2 || len(sourceRaw) > 1048576 {
		return empty, ErrInvalid
	}
	sourceRaw = append([]byte(nil), sourceRaw...)
	evidence, err := ReadExposurePreviewEvidence(ctx, db, store, bucket, who, run, params)
	if err != nil {
		return empty, err
	}
	body, sha, err := FreezeExposurePreviewParams(params)
	if err != nil {
		return empty, err
	}
	load := func(ctx context.Context) ([2]PlanSource, error) {
		var result [2]PlanSource
		rows, err := db.Query(ctx, "SELECT role,artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_preapply_preview_output_sources($1,$2,$3,$4,$5,$6,$7)",
			params.TenantID, run, who.UserID, who.SessionID, string(body), sha, string(evidence.raw))
		if err != nil {
			return result, err
		}
		defer rows.Close()
		seen := map[string]bool{}
		for rows.Next() {
			var role string
			var item PlanSource
			if err := rows.Scan(&role, &item.Ref.ArtifactID, &item.Ref.SHA256, &item.Bucket, &item.Key, &item.Version, &item.Bytes, &item.MediaType); err != nil {
				return result, err
			}
			if seen[role] || item.Bucket != bucket || item.Bytes < 2 {
				return result, ErrInvalid
			}
			seen[role] = true
			switch role {
			case "working_image":
				if item.Ref != evidence.Evidence.WorkingImage || item.MediaType != "application/json" || item.Bytes > 1048576 {
					return result, ErrInvalid
				}
				result[0] = item
			case "pixels":
				if item.Ref != evidence.Evidence.Pixels || item.MediaType != "application/x-npy" || item.Bytes > 2400016384 {
					return result, ErrInvalid
				}
				result[1] = item
			default:
				return result, ErrInvalid
			}
		}
		if err := rows.Err(); err != nil {
			return result, err
		}
		if !seen["working_image"] || !seen["pixels"] {
			return result, ErrUnavailable
		}
		return result, nil
	}
	outputs, err := load(ctx)
	if err != nil {
		return empty, err
	}
	read, err := NewPlanObjectReader(store, bucket)
	if err != nil {
		return empty, err
	}
	raw, err := read(ctx, outputs[0])
	if err != nil {
		return empty, err
	}
	metadata, err := VerifyExposurePreviewImageBinding(evidence.Evidence, sourceRaw, raw)
	if err != nil {
		return empty, err
	}
	stream, err := store.Open(ctx, objectstore.Object{Key: outputs[1].Key, VersionID: outputs[1].Version,
		SHA256: outputs[1].Ref.SHA256, Bytes: outputs[1].Bytes, ContentType: outputs[1].MediaType})
	if err != nil {
		return empty, err
	}
	if stream == nil {
		return empty, ErrInvalid
	}
	verifyErr := masknpy.VerifyNonnegativeFloat32RGBNPY(ctx, stream, outputs[1].Bytes, outputs[1].Ref.SHA256, metadata.Width, metadata.Height)
	closeErr := stream.Close()
	if verifyErr != nil {
		return empty, verifyErr
	}
	if closeErr != nil {
		return empty, closeErr
	}
	current, err := load(ctx)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if current != outputs {
		return empty, ErrConflict
	}
	return PreviewImageRead{Evidence: evidence, Image: outputs[0], Pixels: outputs[1], Metadata: metadata, ImageBytes: append([]byte(nil), raw...)}, nil
}
