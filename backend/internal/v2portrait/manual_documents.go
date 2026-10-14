package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type ManualPairDocuments struct {
	Origin          ManualPairOrigin
	RootRaw         []byte
	ParamsRaw       []byte
	VectorsRaw      []byte
	VectorsRef      v2input.Ref
	VectorsContract string
}

// ReadManualPairDocuments composes SQL lineage with independent exact-byte
// checks. sourceRaw must be independently authorized against base.Source.
// This does not verify mask pixel rasterization or admit a live worker lease.
func ReadManualPairDocuments(ctx context.Context, db *pgxpool.Pool, objects v2input.SessionAnalysisObjectReader,
	who auth.Identity, tenant, project, snapshot uuid.UUID, base v2input.SessionVersionBinding,
	sourceRaw []byte, target, guard, targetPixel, guardPixel v2input.Ref, bucket string) (ManualPairDocuments, error) {
	var zero ManualPairDocuments
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if objects == nil || len(sourceRaw) < 2 || len(sourceRaw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	sum := sha256.Sum256(sourceRaw)
	if hex.EncodeToString(sum[:]) != base.Source.SHA256 {
		return zero, v2input.ErrInvalid
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	hash, err := canonical.Hash(source.Canvas)
	if err != nil || hash != base.CanvasHash {
		return zero, v2input.ErrInvalid
	}
	pair, err := ReadManualPairObjects(ctx, db, who, tenant, project, snapshot, base, target, guard, targetPixel, guardPixel, bucket)
	if err != nil {
		return zero, err
	}
	result := ManualPairDocuments{Origin: pair.Origin}
	result.RootRaw, err = v2input.ReadPinnedSemanticDocument(ctx, objects, pair.Root, pair.Origin.Root)
	if err != nil {
		return zero, err
	}
	result.ParamsRaw, err = v2input.ReadPinnedSemanticDocument(ctx, objects, pair.Params, pair.Origin.Params)
	if err != nil {
		return zero, err
	}
	root, err := review.ParseManualRasterDocument(result.RootRaw, review.ArtifactRef{ID: pair.Origin.Root.ArtifactID, SHA256: pair.Origin.Root.SHA256})
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	if err = review.VerifyManualMaskDocumentRefs(root, review.ArtifactRef{ID: pair.Origin.Root.ArtifactID, SHA256: pair.Origin.Root.SHA256}, review.ArtifactRef{ID: target.ArtifactID, SHA256: target.SHA256}, review.ArtifactRef{ID: guard.ArtifactID, SHA256: guard.SHA256}); err != nil {
		return zero, v2input.ErrInvalid
	}
	if root.Target.Artifact.ID != targetPixel.ArtifactID || root.Target.Artifact.SHA256 != targetPixel.SHA256 || root.Protected.Artifact.ID != guardPixel.ArtifactID || root.Protected.Artifact.SHA256 != guardPixel.SHA256 {
		return zero, v2input.ErrInvalid
	}
	external, err := review.VerifyManualMaskParams(result.ParamsRaw, review.ArtifactRef{ID: pair.Origin.Params.ArtifactID, SHA256: pair.Origin.Params.SHA256}, result.RootRaw, review.ArtifactRef{ID: pair.Origin.Root.ArtifactID, SHA256: pair.Origin.Root.SHA256}, root.Target, source.Source(review.ArtifactRef{ID: base.Source.ArtifactID, SHA256: base.Source.SHA256}))
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	if external != nil {
		ref := v2input.Ref{ArtifactID: external.ID, SHA256: external.SHA256}
		object, e := ReadManualVectorsObject(ctx, db, who, tenant, project, snapshot, base, target, guard, targetPixel, guardPixel, pair.Origin, ref, bucket)
		if e != nil {
			return zero, e
		}
		result.VectorsRaw, err = v2input.ReadPinnedSemanticDocument(ctx, objects, object, ref)
		if err != nil {
			return zero, err
		}
	}
	result.VectorsRef = pair.Origin.Params
	result.VectorsContract = "Params"
	if external != nil {
		if err = review.VerifyManualVectorsDocument(result.VectorsRaw, *external, source.Source(review.ArtifactRef{ID: base.Source.ArtifactID, SHA256: base.Source.SHA256})); err != nil {
			return zero, v2input.ErrInvalid
		}
		result.VectorsRef = v2input.Ref{ArtifactID: external.ID, SHA256: external.SHA256}
		result.VectorsContract = "ManualMask"
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return result, nil
}
