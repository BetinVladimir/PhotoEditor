package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type MediaRetryLease struct {
	EventID, Token, TenantID, BulkID, ItemID, AssetVersionID uuid.UUID
	Variants                                                 []string
	Master                                                   MediaMaster
}

type MediaRetryStore struct {
	DB      artifactjson.RowQuerier
	Objects ArtifactOpener
	Bucket  string
}

func (s MediaRetryStore) Claim(ctx context.Context) (*MediaRetryLease, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return nil, errors.New("media retry store unavailable")
	}
	var lease MediaRetryLease
	var variants []byte
	var a styleArtifact
	var width, height *int64
	var role string
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := s.DB.QueryRow(claim, "SELECT event_id,materialize_token,tenant_id,bulk_id,item_id,asset_version_id,variants,master_artifact_id,bucket,object_key,object_version,sha256,bytes,media_type,width,height,output_role FROM photo.claim_media_retry_job()").Scan(&lease.EventID, &lease.Token, &lease.TenantID, &lease.BulkID, &lease.ItemID, &lease.AssetVersionID, &variants, &a.ArtifactID, &a.Bucket, &a.Key, &a.VersionID, &a.SHA256, &a.Bytes, &a.MediaType, &width, &height, &role)
	cancel()
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lease.EventID == uuid.Nil || lease.Token == uuid.Nil || a.Bucket != s.Bucket || role != "working_image" || a.MediaType != "application/json" {
		return nil, errors.New("invalid media retry lease")
	}
	if json.Unmarshal(variants, &lease.Variants) != nil || len(lease.Variants) < 1 || len(lease.Variants) > 2 {
		return nil, errors.New("invalid media retry variants")
	}
	read, cancel := context.WithTimeout(ctx, 20*time.Second)
	image, err := readWorking(read, s.Objects, s.Bucket, a)
	cancel()
	if err != nil {
		return nil, err
	}
	lease.Master = MediaMaster{Artifact: review.ArtifactRef{ID: a.ArtifactID, SHA256: a.SHA256}, Bytes: a.Bytes, Width: int64(image.Canvas.Width), Height: int64(image.Canvas.Height), MediaType: a.MediaType, Working: &image}
	return &lease, nil
}

func (s MediaRetryStore) Renew(ctx context.Context, l MediaRetryLease) (bool, error) {
	return s.transition(ctx, "SELECT photo.renew_media_retry_job($1,$2)", l)
}
func (s MediaRetryStore) Release(ctx context.Context, l MediaRetryLease) (bool, error) {
	return s.transition(ctx, "SELECT photo.release_media_retry_job($1,$2)", l)
}
func (s MediaRetryStore) transition(ctx context.Context, q string, l MediaRetryLease) (bool, error) {
	if s.DB == nil || l.EventID == uuid.Nil || l.Token == uuid.Nil {
		return false, errors.New("invalid media retry lease")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, q, l.EventID, l.Token).Scan(&ok)
	return ok, err
}

func (s MediaRetryStore) Begin(ctx context.Context, l MediaRetryLease, run uuid.UUID, hash string, config json.RawMessage) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.begin_media_retry_plan($1,$2,$3,$4,$5)`, l.EventID, l.Token, run, hash, config).Scan(&ok)
	return ok, err
}

func (s MediaRetryStore) Register(ctx context.Context, l MediaRetryLease, run uuid.UUID, n MediaRetryDocument, paramsSHA string) (bool, error) {
	payload := struct {
		ID             uuid.UUID            `json:"id"`
		AssetVersionID uuid.UUID            `json:"asset_version_id"`
		Variant        string               `json:"variant"`
		Operation      string               `json:"operation"`
		ProfileHash    string               `json:"profile_hash"`
		Inputs         []review.ArtifactRef `json:"inputs"`
		ParamsSHA256   string               `json:"params_sha256"`
	}{n.ID, n.AssetVersionID, n.Variant, n.Operation, n.ProfileHash, n.Inputs, paramsSHA}
	raw, err := canonical.Marshal(payload)
	if err != nil {
		return false, err
	}
	var ok bool
	err = s.DB.QueryRow(ctx, `SELECT photo.register_media_retry_node($1,$2,$3,$4,$5)`, l.EventID, l.Token, run, raw, n.EventID).Scan(&ok)
	return ok, err
}

func (s MediaRetryStore) Materialize(ctx context.Context, l MediaRetryLease, run uuid.UUID, task PreparedTask) (bool, error) {
	raw, err := canonical.Marshal(task)
	if err != nil {
		return false, err
	}
	var ok bool
	err = s.DB.QueryRow(ctx, `SELECT photo.materialize_media_retry_task($1,$2,$3,$4)`, l.EventID, l.Token, run, raw).Scan(&ok)
	return ok, err
}

func (s MediaRetryStore) Seal(ctx context.Context, l MediaRetryLease, run uuid.UUID) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.seal_media_retry_plan($1,$2,$3)`, l.EventID, l.Token, run).Scan(&ok)
	return ok, err
}

type MediaRetryDocuments struct {
	DB     artifactjson.RowQuerier
	Bucket string
}

type MediaRetryFinalizer struct{ DB artifactjson.RowQuerier }

func (f MediaRetryFinalizer) ProcessOne(ctx context.Context) (bool, error) {
	if f.DB == nil {
		return false, errors.New("media retry finalizer unavailable")
	}
	var ok bool
	err := f.DB.QueryRow(ctx, `SELECT photo.finalize_media_retry_job()`).Scan(&ok)
	return ok, err
}

func (r MediaRetryDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if r.DB == nil || s.Purpose != "task_params" {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err := r.DB.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_media_retry_document($1,$2,$3,$4,$5,$6)`, s.Tenant, s.Run, s.Owner, sha, size, r.Bucket).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	if outcome == "conflict" {
		return artifactjson.Reservation{}, artifactjson.ErrConflict
	}
	if outcome == "quota" {
		return artifactjson.Reservation{}, artifactjson.ErrQuota
	}
	if outcome == "stale" || outcome == "forbidden" {
		return artifactjson.Reservation{}, artifactjson.ErrStale
	}
	if id == nil || token == nil {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	return result, nil
}

func (r MediaRetryDocuments) Complete(ctx context.Context, s artifactjson.Scope, v artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	key, err := artifactjson.DocumentKey(s, v.ArtifactID)
	if err != nil || o.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var ok bool
	err = r.DB.QueryRow(ctx, `SELECT photo.complete_media_retry_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.Tenant, s.Run, s.Owner, v.ArtifactID, v.Token, o.SHA256, o.Bytes, r.Bucket, o.VersionID).Scan(&ok)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !ok {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: v.ArtifactID, SHA256: o.SHA256}, nil
}
