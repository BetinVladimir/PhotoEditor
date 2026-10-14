package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// PerspectiveCoordinator is provisioned by the deployment. Policy expectations
// and resource ceilings must not come from an HTTP request.
type PerspectiveCoordinator struct {
	DB        PerspectiveTransactions
	Reader    v2input.SessionAnalysisObjectReader
	Writer    artifactjson.Objects
	Bucket    string
	Pin       PerspectiveQualificationPin
	Locks     v2qualification.DeploymentLocks
	MaxPixels int64
	Capacity  PerspectiveResourceEstimate
}

type PerspectivePrepareRequest struct {
	Tenant, Project, Snapshot uuid.UUID
	Key                       string
	Params, Source            []byte
}

type PreparedPerspective struct {
	draft         PerspectiveDraft
	params        artifactjson.Ref
	qualification LocatedPerspectiveQualification
}

func (p PreparedPerspective) Draft() PerspectiveDraft  { return PerspectiveDraft{raw: p.draft.raw} }
func (p PreparedPerspective) Params() artifactjson.Ref { return p.params }

// PrepareParams verifies original evidence and every physical source occurrence
// before persisting a preliminary draft or reserving document quota. It does
// not create tasks, publish input transport, seal a graph or admit work.
func (c PerspectiveCoordinator) PrepareParams(ctx context.Context, who auth.Identity, request PerspectivePrepareRequest) (PreparedPerspective, error) {
	var zero PreparedPerspective
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Reader == nil || c.Writer == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || request.Tenant == uuid.Nil || request.Project == uuid.Nil || request.Snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(request.Key) {
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
	published, err := PublishPerspectiveParams(ctx, c.DB, c.Writer, c.Bucket, who, request.Tenant, draft)
	if err != nil {
		return zero, err
	}
	frozen, err := draft.Snapshot()
	if err != nil {
		return zero, err
	}
	if published.ID != frozen.ParamsDocument || published.SHA256 != frozen.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	return PreparedPerspective{draft: draft, params: published, qualification: located}, nil
}
