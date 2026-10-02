package asset

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type Service struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
}
type Version struct {
	ID               uuid.UUID       `json:"id"`
	MasterArtifactID uuid.UUID       `json:"master_artifact_id"`
	ContentHash      string          `json:"content_hash"`
	Metadata         json.RawMessage `json:"metadata"`
	ReviewState      string          `json:"review_state"`
	VersionNo        int             `json:"version_no"`
	CreatedAt        time.Time       `json:"created_at"`
}
type Asset struct {
	ID                 uuid.UUID       `json:"id"`
	TenantID           uuid.UUID       `json:"tenant_id"`
	ProjectID          uuid.UUID       `json:"project_id"`
	OriginalArtifactID uuid.UUID       `json:"original_artifact_id"`
	Metadata           json.RawMessage `json:"metadata"`
	UploadState        string          `json:"upload_state"`
	Provenance         json.RawMessage `json:"provenance"`
	Revision           string          `json:"revision"`
	CreatedAt          time.Time       `json:"created_at"`
	CurrentVersion     *Version        `json:"current_version"`
}
type DeleteRequest struct {
	ExpectedRevision  int64     `json:"expected_revision"`
	ConfirmResourceID uuid.UUID `json:"confirm_resource_id"`
}
type JobAccepted struct {
	JobID  uuid.UUID `json:"job_id"`
	Status string    `json:"status"`
}
type SimilarRequest struct {
	AssetVersionID uuid.UUID  `json:"asset_version_id"`
	ProjectID      *uuid.UUID `json:"project_id,omitempty"`
	Limit          int        `json:"limit"`
	MinCosine      float64    `json:"min_cosine"`
}
type SimilarAsset struct {
	Asset  Asset   `json:"asset"`
	Cosine float64 `json:"cosine"`
}
type SimilarPage struct {
	Items []SimilarAsset `json:"items"`
}

const assetFields = `a.id,a.tenant_id,a.project_id,a.original_artifact_id,a.metadata,a.upload_state,a.provenance,a.revision,a.created_at,v.id,v.master_artifact_id,v.content_hash,v.metadata,v.review_state,v.version_no,v.created_at`

func scan(row pgx.Row) (Asset, error) {
	var out Asset
	var revision int64
	var vid, master *uuid.UUID
	var hash, state *string
	var metadata []byte
	var versionNo *int
	var created *time.Time
	err := row.Scan(&out.ID, &out.TenantID, &out.ProjectID, &out.OriginalArtifactID, &out.Metadata, &out.UploadState, &out.Provenance, &revision, &out.CreatedAt, &vid, &master, &hash, &metadata, &state, &versionNo, &created)
	if err == nil {
		out.Revision = strconv.FormatInt(revision, 10)
		if vid != nil {
			out.CurrentVersion = &Version{ID: *vid, MasterArtifactID: *master, ContentHash: *hash, Metadata: metadata, ReviewState: *state, VersionNo: *versionNo, CreatedAt: *created}
		}
	}
	return out, err
}
func versionJoin() string {
	return ` LEFT JOIN LATERAL(SELECT v0.id,v0.master_artifact_id,v0.content_hash,v0.metadata,v0.review_state,v0.version_no,v0.created_at FROM photo.asset_versions v0 WHERE v0.tenant_id=a.tenant_id AND v0.asset_id=a.id ORDER BY v0.version_no DESC,v0.id DESC LIMIT 1)v ON true`
}

func (s Service) Get(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (Asset, error) {
	var out Asset
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var err error
		out, err = scan(tx.QueryRow(ctx, `SELECT `+assetFields+` FROM photo.assets a`+versionJoin()+` WHERE a.tenant_id=$1 AND a.id=$2 AND a.deleted_at IS NULL`, tid, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		_, err = project.Authorize(ctx, tx, tid, out.ProjectID, who.UserID, role, domain.Viewer, false)
		return err
	})
	return out, err
}

func (s Service) SearchSimilar(ctx context.Context, who auth.Identity, tid uuid.UUID, req SimilarRequest) (SimilarPage, error) {
	out := SimilarPage{Items: []SimilarAsset{}}
	if req.AssetVersionID == uuid.Nil || req.Limit < 1 || req.Limit > 100 || math.IsNaN(req.MinCosine) || math.IsInf(req.MinCosine, 0) || req.MinCosine < 0 || req.MinCosine > 1 || (req.ProjectID != nil && *req.ProjectID == uuid.Nil) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var sourceProject uuid.UUID
		var source []float64
		var lock string
		e := tx.QueryRow(ctx, `SELECT a.project_id,e.values,e.model_lock_id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.deleted_at IS NULL JOIN photo.asset_embeddings e ON e.tenant_id=v.tenant_id AND e.asset_version_id=v.id AND e.state='ready' WHERE v.tenant_id=$1 AND v.id=$2`, tid, req.AssetVersionID).Scan(&sourceProject, &source, &lock)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("FEATURES_NOT_READY", 409)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tid, sourceProject, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT a.id,e.values FROM photo.asset_embeddings e JOIN photo.asset_versions v ON v.tenant_id=e.tenant_id AND v.id=e.asset_version_id JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.deleted_at IS NULL JOIN photo.projects p ON p.tenant_id=a.tenant_id AND p.id=a.project_id AND p.archived_at IS NULL WHERE e.tenant_id=$1 AND e.state='ready' AND e.model_lock_id=$2 AND v.id<>$3 AND ($4::uuid IS NULL OR a.project_id=$4) AND ($5::boolean OR p.visibility='workspace' OR EXISTS(SELECT FROM photo.project_acl acl WHERE acl.tenant_id=a.tenant_id AND acl.project_id=a.project_id AND acl.user_id=$6))`, tid, lock, req.AssetVersionID, req.ProjectID, role.Allows(domain.Admin), who.UserID)
		if e != nil {
			return e
		}
		defer rows.Close()
		type scored struct {
			id    uuid.UUID
			score float64
		}
		var scores []scored
		for rows.Next() {
			var id uuid.UUID
			var v []float64
			if e = rows.Scan(&id, &v); e != nil {
				return e
			}
			if len(v) != len(source) {
				return errors.New("stored embedding dimension mismatch")
			}
			dot := 0.0
			for i := range v {
				dot += source[i] * v[i]
			}
			if dot >= req.MinCosine {
				scores = append(scores, scored{id, dot})
			}
		}
		if e = rows.Err(); e != nil {
			return e
		}
		sort.Slice(scores, func(i, j int) bool {
			if scores[i].score == scores[j].score {
				return scores[i].id.String() < scores[j].id.String()
			}
			return scores[i].score > scores[j].score
		})
		if len(scores) > req.Limit {
			scores = scores[:req.Limit]
		}
		for _, v := range scores {
			item, e := scan(tx.QueryRow(ctx, `SELECT `+assetFields+` FROM photo.assets a`+versionJoin()+` WHERE a.tenant_id=$1 AND a.id=$2`, tid, v.id))
			if e != nil {
				return e
			}
			out.Items = append(out.Items, SimilarAsset{Asset: item, Cosine: v.score})
		}
		return nil
	})
	return out, err
}

func (s Service) List(ctx context.Context, who auth.Identity, tid uuid.UUID, limit int, raw, projectRaw, status, search string) (pagination.Page[Asset], error) {
	out := pagination.Page[Asset]{Items: []Asset{}}
	if limit < 1 || limit > 100 || len([]rune(search)) > 200 || (status != "" && status != "pending" && status != "verified" && status != "rejected") {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var projectID *uuid.UUID
	if projectRaw != "" {
		id, err := uuid.Parse(projectRaw)
		if err != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		projectID = &id
	}
	filter, err := canonical.Hash(map[string]any{"project_id": projectRaw, "status": status, "search": search})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: filter, Sort: "asset:created_at,id"}
	var afterID *uuid.UUID
	var afterTime *time.Time
	if raw != "" {
		cursor, e := s.Cursors.Decode(raw, scope)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, e := uuid.Parse(cursor.LastID)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		at, e := time.Parse(time.RFC3339Nano, cursor.LastPosition)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterID = &id
		afterTime = &at
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		rows, e := tx.Query(ctx, `SELECT `+assetFields+` FROM photo.assets a JOIN photo.projects p ON p.tenant_id=a.tenant_id AND p.id=a.project_id AND p.archived_at IS NULL`+versionJoin()+` WHERE a.tenant_id=$1 AND a.deleted_at IS NULL AND ($2::boolean OR p.visibility='workspace' OR EXISTS(SELECT FROM photo.project_acl acl WHERE acl.tenant_id=a.tenant_id AND acl.project_id=a.project_id AND acl.user_id=$3)) AND ($4::uuid IS NULL OR a.project_id=$4) AND ($5='' OR a.upload_state=$5) AND ($6='' OR strpos(lower(a.metadata::text),lower($6))>0) AND ($7::uuid IS NULL OR (a.created_at,a.id)>($8::timestamptz,$7::uuid)) ORDER BY a.created_at,a.id LIMIT $9`, tid, role.Allows(domain.Admin), who.UserID, projectID, status, search, afterID, afterTime, limit+1)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			item, e := scan(rows)
			if e != nil {
				return e
			}
			out.Items = append(out.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID = last.ID.String()
		scope.LastPosition = last.CreatedAt.Format(time.RFC3339Nano)
		next, e := s.Cursors.Encode(scope)
		if e != nil {
			return out, e
		}
		out.NextCursor = &next
	}
	return out, nil
}

func (s Service) Delete(ctx context.Context, who auth.Identity, tid, id uuid.UUID, expected int64, req DeleteRequest) (JobAccepted, error) {
	var out JobAccepted
	if expected < 1 || req.ExpectedRevision != expected || req.ConfirmResourceID != id {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		var revision int64
		err := tx.QueryRow(ctx, `SELECT project_id,revision FROM photo.assets WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tid, id).Scan(&projectID, &revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		if revision != expected {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		var used bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.collection_items i JOIN photo.collections c ON c.tenant_id=i.tenant_id AND c.id=i.collection_id WHERE i.tenant_id=$1 AND i.asset_id=$2 AND c.deleted_at IS NULL AND c.state='published')`, tid, id).Scan(&used); err != nil {
			return err
		}
		if used {
			return domain.NewError("ASSET_IN_USE", 409)
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.assets SET deleted_at=clock_timestamp(),revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, id); err != nil {
			return err
		}
		out = JobAccepted{JobID: uuid.New(), Status: "queued"}
		if _, err = tx.Exec(ctx, `INSERT INTO photo.deletion_jobs(id,tenant_id,resource_type,resource_id,status,requested_by) VALUES($1,$2,'asset',$3,'queued',$4)`, out.JobID, tid, id, who.UserID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES($1,'user',$2,'asset.delete','asset',$3,$4,'{}')`, tid, who.UserID, id, uuid.New())
		return err
	})
	return out, err
}
