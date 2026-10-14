package v2optical

import (
	"context"
	"photoeditor/backend/internal/auth"
)

// prepareGeometry reloads line originals under current source authority. It does
// not accept client-provided line bytes or create missing publication metadata.
func (c PerspectiveCoordinator) prepareGeometry(ctx context.Context, who auth.Identity, request PerspectivePrepareRequest, params PerspectiveParams, width, height int64) (PerspectiveManualBounds, [][]byte, error) {
	if params.Mode == "manual" {
		bounds, err := CompilePerspectiveManualGeometry(ctx, params, width, height, c.MaxPixels)
		return bounds.PerspectiveManualBounds, nil, err
	}
	lines, err := (PerspectiveLinesLoader{Catalog: SQLPerspectiveLinesCatalog{DB: c.DB, Bucket: c.Bucket}, Objects: c.Reader, Bucket: c.Bucket}).Load(ctx, who, request.Tenant, request.Project, request.Snapshot, params)
	if err != nil {
		return PerspectiveManualBounds{}, nil, err
	}
	raw, _, err := lines.Compile()
	if err != nil {
		return PerspectiveManualBounds{}, nil, err
	}
	if _, err := VerifyPerspectiveLinesOriginal(params, raw); err != nil {
		return PerspectiveManualBounds{}, nil, err
	}
	geometry, err := CompilePerspectiveAutoGeometry(ctx, params, lines, width, height, c.MaxPixels)
	if err != nil {
		return PerspectiveManualBounds{}, nil, err
	}
	return PerspectiveManualBounds{Width: geometry.Width, Height: geometry.Height, CropFraction: geometry.CropFraction}, [][]byte{raw}, nil
}
