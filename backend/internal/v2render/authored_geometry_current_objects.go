package v2render

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type authoredGeometryCurrentObjectScope struct {
	Scheduler *pgxpool.Pool
	Source    AuthoredInstructionSource
	Params    CompiledAuthoredGeometryTaskParams
	Ref       v2input.Ref
	Witnesses AuthoredGeometryScopeWitnesses
}

func (s SQLAuthoredGeometryPhaseObjects) currentObject(ctx context.Context, who auth.Identity, tenant, project, parent uuid.UUID, ordinal int, ref v2input.Ref) (objectstore.Object, bool, error) {
	zero := objectstore.Object{}
	p := s.current
	if e := ctx.Err(); e != nil {
		return zero, false, e
	}
	if p == nil || p.Scheduler == nil || who.UserID != p.Source.Actor || who.SessionID != p.Source.Session || tenant != p.Source.Tenant || project != p.Source.Project || parent != p.Source.Parent || ordinal != p.Source.Instruction.Ordinal || ValidateAuthoredGeometryTaskParams(p.Params, p.Ref) != nil {
		return zero, false, v2input.ErrInvalid
	}
	params, e := canonical.Marshal(p.Ref)
	if e != nil {
		return zero, false, e
	}
	witnesses, e := canonical.Marshal(p.Witnesses)
	if e != nil {
		return zero, false, e
	}
	var raw []byte
	e = database.TenantTransaction(ctx, p.Scheduler, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.read_authored_render_geometry_task_input_object($1,$2,$3,$4::jsonb,$5,$6::jsonb,$7,$8)`, tenant, parent, ordinal, params, string(p.Params.JSON), witnesses, ref.ArtifactID, ref.SHA256).Scan(&raw)
	})
	if e != nil {
		return zero, false, e
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return zero, false, nil
	}
	var row struct {
		Ref     v2input.Ref `json:"ref"`
		Bucket  string      `json:"bucket"`
		Key     string      `json:"object_key"`
		Version string      `json:"object_version"`
		Bytes   int64       `json:"bytes"`
		Media   string      `json:"media_type"`
	}
	if decodeAuthoredParams(raw, &row) != nil || row.Ref != ref || row.Bucket != s.Bucket || row.Key == "" || row.Version == "" || row.Version == "null" || row.Bytes < 1 {
		return zero, false, v2input.ErrInvalid
	}
	return objectstore.Object{Key: row.Key, VersionID: row.Version, SHA256: ref.SHA256, Bytes: row.Bytes, ContentType: row.Media}, true, ctx.Err()
}
