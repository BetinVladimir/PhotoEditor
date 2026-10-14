package v2optical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/v2input"
)

func VerifyPerspectiveLinesOriginal(params PerspectiveParams, raw []byte) (PerspectiveLineSegments, error) {
	var zero PerspectiveLineSegments
	if _, _, err := params.Compile(); err != nil {
		return zero, err
	}
	if params.Mode != "auto" || params.Lines == nil || len(raw) < 2 || len(raw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != params.Lines.SHA256 {
		return zero, v2input.ErrInvalid
	}
	return ParsePerspectiveLineSegments(raw)
}
func CompilePerspectiveAutoExecutionProfile(ctx context.Context, params PerspectiveParams, source, linesRaw []byte, qualified VerifiedPerspectiveQualification, maxPixels int64, capacity PerspectiveResourceEstimate) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lines, err := VerifyPerspectiveLinesOriginal(params, linesRaw)
	if err != nil {
		return nil, err
	}
	image, err := VerifyPerspectiveWorkingImage(params, source)
	if err != nil {
		return nil, err
	}
	geometry, err := CompilePerspectiveAutoGeometry(ctx, params, lines, image.Canvas.Width, image.Canvas.Height, maxPixels)
	if err != nil {
		return nil, err
	}
	profile, err := compilePerspectiveSizedProfile(params, source, qualified, geometry.Width, geometry.Height, capacity)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return profile, nil
}
