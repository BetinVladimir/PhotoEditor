package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type StyleImportLease struct {
	TenantID, RunID, StyleID, TaskID, PackageID, Token uuid.UUID
	ManifestID                                         *uuid.UUID
	Params                                             json.RawMessage
	ParamsSHA                                          string
}
type StyleImportStore struct{ DB artifactjson.RowQuerier }

func (s StyleImportStore) Claim(ctx context.Context) (*StyleImportLease, error) {
	if s.DB == nil {
		return nil, errors.New("style import database required")
	}
	v := new(StyleImportLease)
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,run_id,style_id,task_id,package_artifact_id,manifest_artifact_id,params,params_sha256,token FROM photo.claim_style_import_build()`).Scan(&v.TenantID, &v.RunID, &v.StyleID, &v.TaskID, &v.PackageID, &v.ManifestID, &v.Params, &v.ParamsSHA, &v.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}
func (s StyleImportStore) Artifact(ctx context.Context, lease StyleImportLease, id uuid.UUID) (review.ArtifactRef, int64, error) {
	var r review.ArtifactRef
	var n int64
	e := s.DB.QueryRow(ctx, `SELECT id,sha256,bytes FROM photo.style_import_artifact_ref($1,$2,$3,$4,$5)`, lease.TenantID, lease.RunID, lease.TaskID, lease.Token, id).Scan(&r.ID, &r.SHA256, &n)
	return r, n, e
}
func (s StyleImportStore) Materialize(ctx context.Context, l StyleImportLease, params artifactjson.Ref, digest string, profile []byte) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.materialize_style_import_task($1,$2,$3,$4,$5,$6,$7)`, l.TenantID, l.RunID, l.Token, params.ID, params.SHA256, digest, profile).Scan(&ok)
	return ok, e
}

type StyleImportDocuments struct {
	DB     artifactjson.RowQuerier
	Bucket string
}

func (r StyleImportDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	e := r.DB.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_style_import_document($1,$2,$3,$4,$5,$6)`, s.Tenant, s.Run, s.Owner, sha, size, r.Bucket).Scan(&outcome, &id, &token, &ready)
	if e != nil {
		return artifactjson.Reservation{}, e
	}
	if outcome == "quota" {
		return artifactjson.Reservation{}, artifactjson.ErrQuota
	}
	if outcome == "conflict" {
		return artifactjson.Reservation{}, artifactjson.ErrConflict
	}
	if outcome == "forbidden" || outcome == "stale" {
		return artifactjson.Reservation{}, artifactjson.ErrStale
	}
	if id == nil || token == nil {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	v := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		v.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	return v, nil
}
func (r StyleImportDocuments) Complete(ctx context.Context, s artifactjson.Scope, t artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	var ok bool
	e := r.DB.QueryRow(ctx, `SELECT photo.complete_style_import_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.Tenant, s.Run, s.Owner, t.ArtifactID, t.Token, o.SHA256, o.Bytes, r.Bucket, o.VersionID).Scan(&ok)
	if e != nil {
		return artifactjson.Ref{}, e
	}
	if !ok {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: t.ArtifactID, SHA256: o.SHA256}, nil
}

type StyleImportCoordinator struct {
	Store     StyleImportStore
	Publisher ContractPublisher
}

func (c StyleImportCoordinator) ProcessOne(ctx context.Context) (bool, error) {
	l, e := c.Store.Claim(ctx)
	if e != nil || l == nil {
		return false, e
	}
	if l.Token == uuid.Nil || l.TaskID == uuid.Nil {
		return false, errors.New("invalid style import lease")
	}
	refs := []review.ArtifactRef{}
	total := int64(0)
	for _, id := range []uuid.UUID{l.PackageID} {
		r, n, e := c.Store.Artifact(ctx, *l, id)
		if e != nil {
			return false, e
		}
		refs = append(refs, r)
		total += n
	}
	if l.ManifestID != nil {
		r, n, e := c.Store.Artifact(ctx, *l, *l.ManifestID)
		if e != nil {
			return false, e
		}
		refs = append(refs, r)
		total += n
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID.String() < refs[j].ID.String() })
	ref, e := c.Publisher.Publish(ctx, artifactjson.Scope{Tenant: l.TenantID, Run: l.RunID, Owner: l.TaskID, Purpose: "task_params"}, l.Params)
	if e != nil {
		return false, e
	}
	digest, e := repairInputDigest("style.import", "1", review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}, refs)
	if e != nil {
		return false, e
	}
	profile, e := canonical.Marshal(map[string]any{"backend": "cpu", "precision": "float32", "tile_size": 256, "max_pixels": int64(0), "resource_estimate": map[string]int64{"ram_bytes": 536870912, "vram_bytes": 0, "scratch_bytes": total + 104857600, "output_bytes": 104857600 + 129*1048576, "pixel_count": 0}})
	if e != nil {
		return false, e
	}
	ok, e := c.Store.Materialize(ctx, *l, ref, digest, profile)
	if e != nil {
		return false, e
	}
	if !ok {
		return false, errors.New("style import materialization rejected")
	}
	return true, nil
}
