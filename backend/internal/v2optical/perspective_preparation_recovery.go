package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"time"
)

// RecoverPrepared rebuilds owned intent from original qualified bytes and reads
// the committed Params reference. It never writes objects or repairs quota.
func (c PerspectiveCoordinator) RecoverPrepared(ctx context.Context, who auth.Identity, request PerspectivePrepareRequest) (PreparedPerspective, error) {
	var zero PreparedPerspective
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Reader == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || request.Tenant == uuid.Nil || request.Project == uuid.Nil || request.Snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(request.Key) {
		return zero, v2input.ErrInvalid
	}
	request.Params = append([]byte(nil), request.Params...)
	request.Source = append([]byte(nil), request.Source...)
	params, err := ParsePerspectiveParams(request.Params)
	if err != nil {
		return zero, err
	}
	image, err := VerifyPerspectiveWorkingImage(params, request.Source)
	if err != nil {
		return zero, err
	}
	// Reject unsupported modes and malformed/under-provisioned requests before IO.
	bounds, originals, err := c.prepareGeometry(ctx, who, request, params, image.Canvas.Width, image.Canvas.Height)
	if err != nil {
		return zero, err
	}
	required, err := PerspectiveResources(image.Canvas.Width, image.Canvas.Height, bounds.Width, bounds.Height)
	if err != nil {
		return zero, err
	}
	if err = ValidatePerspectiveCapacity(required, c.Capacity); err != nil {
		return zero, err
	}
	located, err := LoadPerspectiveQualificationEvidence(ctx, SQLPerspectiveQualificationCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, who, request.Tenant, request.Project, c.Pin, c.Locks, params, image.Color.ICCSHA256)
	if err != nil {
		return zero, err
	}
	qualified, err := located.Qualification()
	if err != nil {
		return zero, err
	}
	draft, err := CompilePerspectiveDraft(ctx, who, request.Tenant, request.Project, request.Snapshot, request.Key, request.Params, request.Source, qualified, c.MaxPixels, c.Capacity, originals...)
	if err != nil {
		return zero, err
	}
	if err = VerifyPerspectiveSourceObjects(ctx, SQLPerspectiveSourceArrayCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, who, request.Tenant, request.Project, request.Snapshot, params, request.Source); err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = (perspectiveParamsRepository{}).authorize(ctx, tx, artifactjson.Scope{Tenant: request.Tenant}); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.read_v29_perspective_prepared_params($1,$2,$3,$4)", request.Tenant, who.UserID, who.SessionID, draft.raw).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 4096 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var published artifactjson.Ref
	if json.Unmarshal(normal, &published) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(published)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	frozen, err := draft.Snapshot()
	if err != nil {
		return zero, err
	}
	if published.ID != frozen.ParamsDocument || published.SHA256 != frozen.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	if err = LockLocatedPerspectiveQualification(ctx, tx, who, request.Tenant, request.Project, located); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return PreparedPerspective{draft: draft, params: published, qualification: located}, nil
}
