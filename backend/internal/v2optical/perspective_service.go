package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

type PerspectiveIntent struct {
	SchemaVersion int               `json:"schema_version"`
	Snapshot      uuid.UUID         `json:"snapshot_id"`
	Params        PerspectiveParams `json:"params"`
}

func DecodePerspectiveIntent(raw []byte) (PerspectiveIntent, error) {
	var zero PerspectiveIntent
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var intent PerspectiveIntent
	if json.Unmarshal(normal, &intent) != nil || intent.SchemaVersion != 1 || intent.Snapshot == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, _, err = intent.Params.Compile(); err != nil {
		return zero, err
	}
	exact, err := canonical.Marshal(intent)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	return intent, nil
}

type PerspectiveService struct {
	coordinator     PerspectiveCoordinator
	tenant, project uuid.UUID
}

func NewPerspectiveService(c PerspectiveCoordinator, tenant, project uuid.UUID) (*PerspectiveService, error) {
	if c.DB == nil || c.Reader == nil || c.Writer == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || tenant == uuid.Nil || project == uuid.Nil || c.MaxPixels < 1 || c.MaxPixels > 200000000 || c.Capacity.PixelCount < 1 || c.Capacity.PixelCount > 200000000 || c.Capacity.VRAMBytes != 0 {
		return nil, v2input.ErrInvalid
	}
	for _, value := range []int64{c.Capacity.RAMBytes, c.Capacity.ScratchBytes, c.Capacity.OutputBytes} {
		if value < 1 || value > 9007199254740991 {
			return nil, v2input.ErrInvalid
		}
	}
	if _, err := c.Pin.Compile(); err != nil {
		return nil, err
	}
	// Own all mutable deployment expectation lists before retaining the service.
	c.Locks.RequiredChecks = append([]string(nil), c.Locks.RequiredChecks...)
	c.Locks.CalibrationSHAs = append([]string(nil), c.Locks.CalibrationSHAs...)
	c.Locks.TestFixtureSHAs = append([]string(nil), c.Locks.TestFixtureSHAs...)
	c.Locks.EvidenceSHAs = append([]string(nil), c.Locks.EvidenceSHAs...)
	expected, err := c.Locks.Expected()
	if err != nil {
		return nil, err
	}
	policy, err := PerspectivePolicySHA(c.Pin.PolicyID, c.Locks)
	if err != nil || policy != c.Pin.PolicySHA || expected.Operation != "optical.perspective" || expected.Version != "1" || expected.Backend != "cpu" {
		return nil, v2input.ErrInvalid
	}
	return &PerspectiveService{coordinator: c, tenant: tenant, project: project}, nil
}

// SubmitProjectIntent accepts geometry only; source bytes, qualification and
// capacity come from the deployment and current accepted snapshot authority.
func (s *PerspectiveService) SubmitProjectIntent(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent PerspectiveIntent) (PerspectiveReceipt, error) {
	var zero PerspectiveReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	intent, err = DecodePerspectiveIntent(raw)
	if err != nil {
		return zero, err
	}
	c := s.coordinator
	source, err := (SQLPerspectiveSourceReader{DB: c.DB, Objects: c.Reader, Bucket: c.Bucket}).Load(ctx, who, tenant, project, intent.Snapshot, intent.Params)
	if err != nil {
		return zero, err
	}
	image, err := VerifyPerspectiveWorkingImage(intent.Params, source)
	if err != nil {
		return zero, err
	}
	bounds, _, err := c.prepareGeometry(ctx, who, PerspectivePrepareRequest{Tenant: tenant, Project: project, Snapshot: intent.Snapshot, Key: key}, intent.Params, image.Canvas.Width, image.Canvas.Height)
	if err != nil {
		return zero, err
	}
	if bounds.Width*bounds.Height > c.Capacity.PixelCount {
		return zero, v2input.ErrInvalid
	}
	// PixelCount is an exact task binding in the worker profile. Other capacity
	// fields retain the deployment ceilings and cannot be raised by the request.
	c.Capacity.PixelCount = bounds.Width * bounds.Height
	params, _, err := intent.Params.Compile()
	if err != nil {
		return zero, err
	}
	return SubmitPerspective(ctx, c, who, PerspectivePrepareRequest{Tenant: tenant, Project: project, Snapshot: intent.Snapshot, Key: key, Params: params, Source: source})
}
