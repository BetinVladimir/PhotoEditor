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

type MultiIntent struct {
	SchemaVersion int             `json:"schema_version"`
	Snapshot      uuid.UUID       `json:"snapshot_id"`
	Params        json.RawMessage `json:"params"`
}

func DecodeMultiIntent(operation string, raw []byte) (MultiIntent, error) {
	var zero MultiIntent
	if len(raw) < 2 || len(raw) > 66560 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var intent MultiIntent
	if json.Unmarshal(normal, &intent) != nil || intent.SchemaVersion != 1 || intent.Snapshot == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	switch operation {
	case "optical.panorama":
		if _, err := ParsePanoramaParams(intent.Params); err != nil {
			return zero, err
		}
	case "optical.focus_stack":
		if _, err := ParseFocusStackParams(intent.Params); err != nil {
			return zero, err
		}
	default:
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(intent)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	intent.Params = append(json.RawMessage(nil), intent.Params...)
	return intent, nil
}

type MultiService struct {
	coordinator     MultiCoordinator
	tenant, project uuid.UUID
}

func NewMultiService(c MultiCoordinator, tenant, project uuid.UUID) (*MultiService, error) {
	if (c.Operation != "optical.panorama" && c.Operation != "optical.focus_stack") || c.DB == nil || c.Reader == nil || c.Writer == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || tenant == uuid.Nil || project == uuid.Nil || c.MaxPixels < 1 || c.MaxPixels > 200000000 || c.Capacity.PixelCount < 1 || c.Capacity.PixelCount > 200000000 || c.Capacity.VRAMBytes != 0 {
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
	policy, err := MultiPolicySHA(c.Pin.PolicyID, c.Locks)
	if err != nil || policy != c.Pin.PolicySHA || expected.Operation != c.Operation || expected.Version != "1" || expected.Backend != "cpu" {
		return nil, v2input.ErrInvalid
	}
	return &MultiService{coordinator: c, tenant: tenant, project: project}, nil
}

// SubmitProjectIntent resolves original documents through current snapshot
// authority. HTTP input never supplies source bytes or deployment qualification.
func (s *MultiService) SubmitProjectIntent(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent MultiIntent) (MultiReceipt, error) {
	var zero MultiReceipt
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
	intent, err = DecodeMultiIntent(s.coordinator.Operation, raw)
	if err != nil {
		return zero, err
	}
	c := s.coordinator
	if err := ClaimMultiIntent(ctx, c.DB, who, tenant, project, c.Operation, key, intent); err != nil {
		return zero, err
	}
	originals, err := (SQLMultiSourceReader{DB: c.DB, Objects: c.Reader, Bucket: c.Bucket}).Load(ctx, who, tenant, project, intent.Snapshot, c.Operation, intent.Params)
	if err != nil {
		return zero, err
	}
	return SubmitMulti(ctx, c, who, MultiPrepareRequest{Tenant: tenant, Project: project, Snapshot: intent.Snapshot, Key: key, Params: intent.Params, Sources: originals})
}
