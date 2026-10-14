package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"time"
)

type RenderInputSnapshot struct {
	ID        uuid.UUID `json:"id"`
	Digest    string    `json:"digest"`
	ItemCount int       `json:"item_count"`
}
type SQLRenderInputSnapshotService struct{ DB *pgxpool.Pool }

// Create freezes only source inventory. Recipe/object/qualification verification
// remains mandatory at Render admission; no object IO occurs under these locks.
func (s SQLRenderInputSnapshotService) Create(ctx context.Context, who auth.Identity, tid, pid uuid.UUID, key string, sources []v2input.SessionVersionBinding) (idempotency.Response, error) {
	if s.DB == nil || tid == uuid.Nil || pid == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return idempotency.Response{}, v2input.ErrInvalid
	}
	sources = append([]v2input.SessionVersionBinding(nil), sources...)
	if len(sources) < 1 || len(sources) > 5000 {
		return idempotency.Response{}, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, source := range sources {
		_, err := domain.ParseDigest(source.CanvasHash)
		if source.AssetVersionID == uuid.Nil || !validLayerArtifactRef(source.Source) || err != nil || seen[source.AssetVersionID] {
			return idempotency.Response{}, v2input.ErrInvalid
		}
		seen[source.AssetVersionID] = true
	}
	digest, err := canonical.Hash(sources)
	if err != nil {
		return idempotency.Response{}, err
	}
	id, _ := RenderInputSnapshotID(tid, who.UserID, pid, key)
	scope := idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Render.InputSnapshot." + pid.String(), Key: key}
	var out idempotency.Response
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, err := project.Authorize(ctx, tx, tid, pid, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		var err error
		out, err = idempotency.Use(ctx, tx, scope, sources, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			if _, err := tx.Exec(ctx, `INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES($1,$2,'v2','user',$3,$4,$5,$6)`, id, tid, who.UserID, digest, len(sources), time.Now().Add(24*time.Hour)); err != nil {
				return idempotency.Response{}, err
			}
			for ordinal, source := range sources {
				payload, err := canonical.Marshal(struct {
					CanvasHash string `json:"canvas_hash"`
				}{source.CanvasHash})
				if err != nil {
					return idempotency.Response{}, err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, tid, id, ordinal, source.AssetVersionID, source.Source.ArtifactID, source.Source.SHA256, payload); err != nil {
					return idempotency.Response{}, err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE photo.snapshots SET expires_at=NULL WHERE tenant_id=$1 AND id=$2`, tid, id); err != nil {
				return idempotency.Response{}, err
			}
			var ready bool
			if err := tx.QueryRow(ctx, `SELECT photo.v25_lens_snapshot_ready($1,$2,$3,$4,$5)`, tid, pid, id, who.UserID, who.SessionID).Scan(&ready); err != nil {
				return idempotency.Response{}, err
			}
			if !ready {
				return idempotency.Response{}, domain.NewError("SNAPSHOT_EXPIRED", 410)
			}
			body, err := canonical.Marshal(RenderInputSnapshot{id, digest, len(sources)})
			return idempotency.Response{Status: 201, Body: body, ResourceID: &id}, err
		})
		if err != nil {
			return err
		}
		_, decodeErr := DecodeRenderInputSnapshotResult(out.Body, id, digest, len(sources))
		if decodeErr != nil || out.Status != 201 || out.ResourceID == nil || *out.ResourceID != id {
			return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_lens_snapshot_ready($1,$2,$3,$4,$5)`, tid, pid, id, who.UserID, who.SessionID).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return domain.NewError("SNAPSHOT_EXPIRED", 410)
		}
		return nil
	})
	return out, err
}
