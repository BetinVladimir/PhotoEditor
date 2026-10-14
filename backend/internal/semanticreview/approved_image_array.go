package semanticreview

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

// VerifyApprovedImageArray streams a pinned source array without retaining it.
// RGB and dense geometry require finite values; alpha accepts unit float32 or
// legacy uint16. ICC, geometric invertibility and normalization are separate.
func VerifyApprovedImageArray(ctx context.Context, db ApprovedProtectionQueryer, objects planObjectStore, bucket string,
	tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	targetID uuid.UUID, reference bool, document PlanDocument, kind string, step int) (PlanSource, error) {
	empty := PlanSource{}
	if objects == nil || bucket == "" || len(document.Body) > 8388608 {
		return empty, ErrInvalid
	}
	document.Body = append([]byte(nil), document.Body...)
	load := func() (PlanSource, error) {
		return ResolveApprovedImageBinaryMetadata(ctx, db, tenant, intent, actor, session, prepared, targets, request, targetID, reference, document, kind, step)
	}
	before, err := load()
	if err != nil {
		return empty, err
	}
	if before.Bucket != bucket {
		return empty, ErrInvalid
	}
	image, err := review.ParseWorkingImage(document.Body)
	if err != nil {
		return empty, ErrInvalid
	}
	width, height := int64(image.Canvas.Width), int64(image.Canvas.Height)
	channels, unit := 3, false
	switch kind {
	case "alpha":
		channels, unit = 0, true
	case "inverse_field":
		var chain struct {
			Steps []struct {
				To review.Canvas `json:"to_canvas"`
			}
		}
		if json.Unmarshal(image.TransformChain, &chain) != nil || step < 0 || step >= len(chain.Steps) {
			return empty, ErrInvalid
		}
		width, height = int64(chain.Steps[step].To.Width), int64(chain.Steps[step].To.Height)
		channels = 2
	case "pixels":
	default:
		return empty, ErrInvalid
	}
	stream, err := objects.Open(ctx, objectstore.Object{Key: before.Key, VersionID: before.Version, SHA256: before.Ref.SHA256, Bytes: before.Bytes, ContentType: before.MediaType})
	if err != nil {
		return empty, err
	}
	if stream == nil {
		return empty, ErrInvalid
	}
	var verifyErr error
	if kind == "alpha" {
		verifyErr = masknpy.VerifyAlphaNPY(ctx, stream, before.Bytes, before.Ref.SHA256, width, height)
	} else {
		verifyErr = masknpy.VerifyFloat32ArrayNPY(ctx, stream, before.Bytes, before.Ref.SHA256, width, height, channels, unit)
	}
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
