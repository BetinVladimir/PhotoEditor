package workflow

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
)

type RenderDerivativeResult struct {
	Task, Result               uuid.UUID
	Attempt                    int
	Fence                      int64
	DerivativeRef, ManifestRef review.ArtifactRef
	Derivative, ManifestObject objectstore.Object
	Manifest                   RenderDerivativeManifest
	ManifestBytes              []byte
}

// LoadResult closes SQL rows before object IO and exposes only hash-checked,
// source-bound manifest bytes. Encoded bytes and final locked adoption remain separate.
func (l RenderDerivativeLoader) LoadResult(ctx context.Context, intent v2render.RenderDerivativeIntent, variant string) (RenderDerivativeResult, error) {
	fail := func() (RenderDerivativeResult, error) { return RenderDerivativeResult{}, objectstore.ErrIntegrity }
	if err := ctx.Err(); err != nil {
		return RenderDerivativeResult{}, err
	}
	if l.DB == nil || l.Objects == nil || l.Bucket == "" || intent.Tenant == uuid.Nil || intent.Run == uuid.Nil || (variant != "preview1600" && variant != "preview2560") {
		return RenderDerivativeResult{}, objectstore.ErrInvalid
	}
	root, imageBytes, err := l.Load(ctx, intent)
	if err != nil {
		return RenderDerivativeResult{}, err
	}
	docs, err := BuildRenderDerivativeDocuments(intent, root, imageBytes)
	if err != nil {
		return RenderDerivativeResult{}, err
	}
	var expected RenderDerivativeDocument
	for _, doc := range docs {
		if doc.Variant == variant {
			expected = doc
		}
	}
	image, err := review.ParseWorkingImage(imageBytes)
	if err != nil {
		return RenderDerivativeResult{}, err
	}
	rows, err := l.DB.Query(ctx, "SELECT * FROM photo.v26_render_derivative_result($1,$2,$3)", intent.Tenant, intent.Run, variant)
	if err != nil {
		return RenderDerivativeResult{}, err
	}
	var result RenderDerivativeResult
	count := 0
	for rows.Next() {
		var task, id uuid.UUID
		var attempt int
		var fence int64
		var params, role, bucket string
		var ref review.ArtifactRef
		var o objectstore.Object
		if err = rows.Scan(&task, &id, &attempt, &fence, &params, &role, &ref.ID, &bucket, &o.Key, &o.VersionID, &ref.SHA256, &o.Bytes, &o.ContentType); err != nil {
			rows.Close()
			return RenderDerivativeResult{}, err
		}
		o.SHA256 = ref.SHA256
		if count >= 2 || task != expected.ID || id == uuid.Nil || attempt < 1 || fence < 1 || params != string(expected.Params) || bucket != l.Bucket || !validExpansionRef(ref) || o.Key == "" || o.VersionID == "" || o.VersionID == "null" || o.Bytes < 1 {
			rows.Close()
			return fail()
		}
		if count == 0 {
			result.Task, result.Result, result.Attempt, result.Fence = task, id, attempt, fence
		} else if result.Task != task || result.Result != id || result.Attempt != attempt || result.Fence != fence {
			rows.Close()
			return fail()
		}
		switch role {
		case "derivative":
			if result.DerivativeRef.ID != uuid.Nil || o.Bytes > 54525952 || (o.ContentType != "image/jpeg" && o.ContentType != "image/webp") {
				rows.Close()
				return fail()
			}
			result.DerivativeRef, result.Derivative = ref, o
		case "derivative_manifest":
			if result.ManifestRef.ID != uuid.Nil || o.Bytes > 1048576 || o.ContentType != "application/json" {
				rows.Close()
				return fail()
			}
			result.ManifestRef, result.ManifestObject = ref, o
		default:
			rows.Close()
			return fail()
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return RenderDerivativeResult{}, err
	}
	if count == 0 {
		return RenderDerivativeResult{}, objectstore.ErrMissing
	}
	if count != 2 || result.DerivativeRef.ID == uuid.Nil || result.ManifestRef.ID == uuid.Nil {
		return fail()
	}
	result.ManifestBytes, err = readRenderDerivativeJSON(ctx, l.Objects, result.ManifestObject)
	if err != nil {
		return RenderDerivativeResult{}, err
	}
	result.Manifest, err = ParseRenderDerivativeManifest(result.ManifestBytes, result.ManifestRef, expected.Inputs[0], result.DerivativeRef, image, variant, intent.SRGBSHA)
	if err != nil {
		return RenderDerivativeResult{}, err
	}
	if result.Manifest.MediaType != result.Derivative.ContentType {
		return fail()
	}
	return result, nil
}
