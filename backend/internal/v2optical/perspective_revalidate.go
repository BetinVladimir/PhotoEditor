package v2optical

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

// revalidate repeats original byte and live source authority without writes.
func (c PerspectiveCoordinator) revalidate(ctx context.Context, who auth.Identity, request PerspectivePrepareRequest, prepared PreparedPerspective) (PreparedPerspective, []byte, error) {
	if err := ctx.Err(); err != nil {
		return PreparedPerspective{}, nil, err
	}
	if c.DB == nil || c.Reader == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 {
		return PreparedPerspective{}, nil, v2input.ErrInvalid
	}
	snapshot, err := prepared.draft.Snapshot()
	if err != nil {
		return PreparedPerspective{}, nil, err
	}
	if snapshot.Tenant != request.Tenant || snapshot.Project != request.Project || snapshot.SnapshotID != request.Snapshot || snapshot.Actor != who.UserID || snapshot.Session != who.SessionID || snapshot.ParamsJCS != string(request.Params) || snapshot.SourceJCS != string(request.Source) {
		return PreparedPerspective{}, nil, v2input.ErrInvalid
	}
	if !idempotency.ValidKey(request.Key) || uuid.NewSHA1(request.Tenant, []byte("V29.Perspective:"+request.Project.String()+":"+who.UserID.String()+":"+request.Key)) != snapshot.Run {
		return PreparedPerspective{}, nil, v2input.ErrInvalid
	}
	descriptor, err := prepared.Descriptor(ctx)
	if err != nil {
		return PreparedPerspective{}, nil, err
	}
	// Own request before catalog/object callbacks and rebuild under current policy.
	request.Params = append([]byte(nil), request.Params...)
	request.Source = append([]byte(nil), request.Source...)
	params, err := ParsePerspectiveParams(request.Params)
	if err != nil {
		return PreparedPerspective{}, nil, err
	}
	image, err := VerifyPerspectiveWorkingImage(params, request.Source)
	if err != nil {
		return PreparedPerspective{}, nil, err
	}
	_, originals, err := c.prepareGeometry(ctx, who, request, params, image.Canvas.Width, image.Canvas.Height)
	if err != nil {
		return PreparedPerspective{}, nil, err
	}
	located, err := LoadPerspectiveQualificationEvidence(ctx, SQLPerspectiveQualificationCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, who, request.Tenant, request.Project, c.Pin, c.Locks, params, image.Color.ICCSHA256)
	if err != nil {
		return PreparedPerspective{}, nil, err
	}
	qualified, err := located.Qualification()
	if err != nil {
		return PreparedPerspective{}, nil, err
	}
	rebuilt, err := CompilePerspectiveDraft(ctx, who, request.Tenant, request.Project, request.Snapshot, request.Key, request.Params, request.Source, qualified, c.MaxPixels, c.Capacity, originals...)
	if err != nil {
		return PreparedPerspective{}, nil, err
	}
	if rebuilt.raw != prepared.draft.raw {
		return PreparedPerspective{}, nil, v2input.ErrInvalid
	}
	current := PreparedPerspective{draft: rebuilt, params: prepared.params, qualification: located}
	expected, err := current.Descriptor(ctx)
	if err != nil || !bytes.Equal(expected, descriptor) {
		return PreparedPerspective{}, nil, v2input.ErrInvalid
	}
	if err = VerifyPerspectiveSourceObjects(ctx, SQLPerspectiveSourceArrayCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, who, request.Tenant, request.Project, request.Snapshot, params, request.Source); err != nil {
		return PreparedPerspective{}, nil, err
	}
	return current, descriptor, nil
}
