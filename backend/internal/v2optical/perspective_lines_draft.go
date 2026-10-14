package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"time"
)

// PerspectiveLinesDraftSnapshot is backend-owned publication intent, not evidence
// of completed extraction, source authorization or object publication.
type PerspectiveLinesDraftSnapshot struct {
	SchemaVersion int                           `json:"schema_version"`
	Tenant        uuid.UUID                     `json:"tenant_id"`
	Project       uuid.UUID                     `json:"project_id"`
	Actor         uuid.UUID                     `json:"actor_id"`
	Session       uuid.UUID                     `json:"auth_session_id"`
	Snapshot      uuid.UUID                     `json:"snapshot_id"`
	Run           uuid.UUID                     `json:"run_id"`
	Owner         uuid.UUID                     `json:"owner_id"`
	Artifact      uuid.UUID                     `json:"artifact_id"`
	Base          v2input.SessionVersionBinding `json:"base"`
	SourceJCS     string                        `json:"source_jcs"`
	LinesJCS      string                        `json:"lines_jcs"`
	SHA256        string                        `json:"sha256"`
	Bytes         int64                         `json:"bytes"`
}
type PerspectiveLinesDraft struct{ raw string }

func (d PerspectiveLinesDraft) Snapshot() (PerspectiveLinesDraftSnapshot, error) {
	var out PerspectiveLinesDraftSnapshot
	if len(d.raw) < 2 || json.Unmarshal([]byte(d.raw), &out) != nil {
		return out, v2input.ErrInvalid
	}
	return out, nil
}

// CompilePerspectiveLinesDraft owns exact source and canonical line bytes under
// one stable key. Geometry validation is separate from publication permission.
func CompilePerspectiveLinesDraft(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, key string, base v2input.SessionVersionBinding, source []byte, lines PerspectiveLineSegments) (PerspectiveLinesDraft, error) {
	var zero PerspectiveLinesDraft
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, v2input.ErrInvalid
	}
	// Reuse strict source verification with a full-frame manual identity intent.
	p := PerspectiveParams{SchemaVersion: 1, Base: base, Mode: "manual", Strength: 0, Corners: []PerspectivePoint{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, MaxCropFraction: 1}
	image, err := VerifyPerspectiveWorkingImage(p, source)
	if err != nil {
		return zero, err
	}
	body, sha, err := lines.Compile()
	if err != nil {
		return zero, err
	}
	// Refuse unsupported family geometry before reserving a document. This seed
	// check does not choose output pixels or silently alter author-supplied lines.
	if _, err := CompilePerspectiveVanishingGeometry(ctx, lines, image.Canvas.Width, image.Canvas.Height); err != nil {
		return zero, err
	}
	run := uuid.NewSHA1(tenant, []byte("V29.PerspectiveLines:"+project.String()+":"+who.UserID.String()+":"+key))
	owner := uuid.NewSHA1(run, []byte("line-publication"))
	artifact := uuid.NewSHA1(owner, []byte("line-segments"))
	if artifact == base.Source.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	doc := PerspectiveLinesDraftSnapshot{SchemaVersion: 1, Tenant: tenant, Project: project, Actor: who.UserID, Session: who.SessionID, Snapshot: snapshot, Run: run, Owner: owner, Artifact: artifact, Base: base, SourceJCS: string(source), LinesJCS: string(body), SHA256: sha, Bytes: int64(len(body))}
	raw, err := canonical.Marshal(doc)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return PerspectiveLinesDraft{raw: string(raw)}, nil
}

// SavePerspectiveLinesDraft stores publication intent only, before object IO.
func SavePerspectiveLinesDraft(ctx context.Context, db PerspectiveTransactions, who auth.Identity, tenant uuid.UUID, draft PerspectiveLinesDraft) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := draft.Snapshot()
	if err != nil {
		return err
	}
	if db == nil || tenant == uuid.Nil || p.Tenant != tenant || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || p.Actor != who.UserID || p.Session != who.SessionID {
		return v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var saved bool
	if err = tx.QueryRow(ctx, "SELECT photo.save_v29_perspective_lines_draft($1,$2,$3,$4)", tenant, who.UserID, who.SessionID, draft.raw).Scan(&saved); err != nil {
		return err
	}
	if !saved {
		return v2input.ErrExpired
	}
	return tx.Commit(ctx)
}
