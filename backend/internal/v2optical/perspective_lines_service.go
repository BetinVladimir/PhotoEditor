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

// PerspectiveLinesIntent authors normalized line endpoints only. Object keys,
// versions, source bytes and publication authority are never client inputs.
type PerspectiveLinesIntent struct {
	SchemaVersion int                           `json:"schema_version"`
	Snapshot      uuid.UUID                     `json:"snapshot_id"`
	Base          v2input.SessionVersionBinding `json:"base"`
	Lines         PerspectiveLineSegments       `json:"line_segments"`
}

func DecodePerspectiveLinesIntent(raw []byte) (PerspectiveLinesIntent, error) {
	var zero PerspectiveLinesIntent
	if len(raw) < 2 || len(raw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
	}
	var intent PerspectiveLinesIntent
	if json.Unmarshal(normal, &intent) != nil || intent.SchemaVersion != 1 || intent.Snapshot == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	p := perspectiveIdentityParams(intent.Base)
	if _, _, err := p.Compile(); err != nil {
		return zero, err
	}
	if _, _, err := intent.Lines.Compile(); err != nil {
		return zero, err
	}
	exact, err := canonical.Marshal(intent)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	return intent, nil
}
func perspectiveIdentityParams(base v2input.SessionVersionBinding) PerspectiveParams {
	return PerspectiveParams{SchemaVersion: 1, Base: base, Mode: "manual", Strength: 0, Corners: []PerspectivePoint{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, MaxCropFraction: 1}
}

type PerspectiveLinesReceipt struct {
	SchemaVersion int                           `json:"schema_version"`
	Tenant        uuid.UUID                     `json:"tenant_id"`
	Project       uuid.UUID                     `json:"project_id"`
	Snapshot      uuid.UUID                     `json:"snapshot_id"`
	Base          v2input.SessionVersionBinding `json:"base"`
	Lines         v2input.Ref                   `json:"lines"`
}

// PublishProjectLines reuses the installed project's source reader and bounded
// immutable publisher; readback verifies both original bytes and current scope.
// This is explicit line authoring, not an automatic line extraction model.
func (s *PerspectiveService) PublishProjectLines(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent PerspectiveLinesIntent) (PerspectiveLinesReceipt, error) {
	var zero PerspectiveLinesReceipt
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
	intent, err = DecodePerspectiveLinesIntent(raw)
	if err != nil {
		return zero, err
	}
	c := s.coordinator
	source, err := (SQLPerspectiveSourceReader{DB: c.DB, Objects: c.Reader, Bucket: c.Bucket}).Load(ctx, who, tenant, project, intent.Snapshot, perspectiveIdentityParams(intent.Base))
	if err != nil {
		return zero, err
	}
	draft, err := CompilePerspectiveLinesDraft(ctx, who, tenant, project, intent.Snapshot, key, intent.Base, source, intent.Lines)
	if err != nil {
		return zero, err
	}
	ref, err := PublishPerspectiveLines(ctx, c.DB, c.Writer, c.Bucket, who, tenant, draft)
	if err != nil {
		return zero, err
	}
	expected, err := draft.Snapshot()
	if err != nil {
		return zero, err
	}
	if ref.ID != expected.Artifact || ref.SHA256 != expected.SHA256 {
		return zero, v2input.ErrInvalid
	}
	params := PerspectiveParams{SchemaVersion: 1, Base: intent.Base, Mode: "auto", Strength: 1, Lines: &v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, MaxCropFraction: 1}
	readback, err := (PerspectiveLinesLoader{Catalog: SQLPerspectiveLinesCatalog{DB: c.DB, Bucket: c.Bucket}, Objects: c.Reader, Bucket: c.Bucket}).Load(ctx, who, tenant, project, intent.Snapshot, params)
	if err != nil {
		return zero, err
	}
	body, _, err := readback.Compile()
	if err != nil || string(body) != expected.LinesJCS {
		return zero, v2input.ErrInvalid
	}
	return PerspectiveLinesReceipt{SchemaVersion: 1, Tenant: tenant, Project: project, Snapshot: intent.Snapshot, Base: intent.Base, Lines: *params.Lines}, nil
}
