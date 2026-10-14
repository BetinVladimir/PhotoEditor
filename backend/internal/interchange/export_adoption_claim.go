package interchange

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// ExportAdoptionClaim is a leased discovery receipt. It grants no permission
// to read result objects or to adopt them; each later phase must recheck the
// exact token and current canonical result under its own SQL authority.
type ExportAdoptionClaim struct {
	Tenant, Workflow, Run, Task, Result uuid.UUID
	Actor, Token                        uuid.UUID
	Key, RequestSHA                     string
	Request                             CreateRequest
}

type SQLExportAdoptionClaimStore struct{ DB *pgxpool.Pool }

// CurrentExportAdoptionClaim checks the exact lease after scheduler callbacks.
// A true receipt only permits the next separately authorized preparation step.
func (s SQLExportAdoptionClaimStore) CurrentExportAdoptionClaim(ctx context.Context, c ExportAdoptionClaim) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || c.validate() != nil {
		return false, v2input.ErrInvalid
	}
	var current bool
	err := s.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_claim_current($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		c.Tenant, c.Workflow, c.Run, c.Task, c.Result, c.Actor, c.Key, c.RequestSHA, c.Token).Scan(&current)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return current, nil
}

// RootExportAdoption pins only the first canonical output and repeats the
// claim fence around its SQL lookup. Callers must still independently qualify
// the root bytes and all transitive outputs before any adoption transaction.
func (s SQLExportAdoptionClaimStore) RootExportAdoption(ctx context.Context, c ExportAdoptionClaim, bucket string) (v2input.Ref, objectstore.Object, error) {
	var noRef v2input.Ref
	var noObject objectstore.Object
	if err := ctx.Err(); err != nil {
		return noRef, noObject, err
	}
	if s.DB == nil || c.validate() != nil || len(bucket) < 3 || len(bucket) > 63 {
		return noRef, noObject, v2input.ErrInvalid
	}
	current, err := s.CurrentExportAdoptionClaim(ctx, c)
	if err != nil {
		return noRef, noObject, err
	}
	if !current {
		return noRef, noObject, v2input.ErrInvalid
	}
	var ref v2input.Ref
	var object objectstore.Object
	var foundBucket string
	err = s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.v30_export_adoption_root_output($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		c.Tenant, c.Workflow, c.Run, c.Task, c.Result, c.Actor, c.Key, c.RequestSHA, c.Token).
		Scan(&ref.ArtifactID, &ref.SHA256, &foundBucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return noRef, noObject, v2input.ErrInvalid
	}
	if err != nil {
		return noRef, noObject, err
	}
	object.SHA256 = ref.SHA256
	if !validImportRef(ref) || foundBucket != bucket || object.Key == "" ||
		object.VersionID == "" || object.VersionID == "null" ||
		object.Bytes < 2 || object.Bytes > 65536 || object.ContentType != "application/json" {
		return noRef, noObject, v2input.ErrInvalid
	}
	current, err = s.CurrentExportAdoptionClaim(ctx, c)
	if err != nil {
		return noRef, noObject, err
	}
	if !current {
		return noRef, noObject, v2input.ErrInvalid
	}
	return ref, object, ctx.Err()
}

func (s SQLExportAdoptionClaimStore) NextExportAdoption(ctx context.Context) (*ExportAdoptionClaim, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, v2input.ErrInvalid
	}
	var c ExportAdoptionClaim
	var request []byte
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,workflow_id,run_id,task_id,result_id,actor_id,idempotency_key,request_sha,frozen_request,claim_token FROM photo.v30_claim_next_export_adoption()`).
		Scan(&c.Tenant, &c.Workflow, &c.Run, &c.Task, &c.Result, &c.Actor, &c.Key, &c.RequestSHA, &request, &c.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(request, &c.Request); err != nil {
		return nil, v2input.ErrInvalid
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, ctx.Err()
}

func (c ExportAdoptionClaim) validate() error {
	if c.Tenant == uuid.Nil || c.Workflow == uuid.Nil || c.Run == uuid.Nil ||
		c.Task == uuid.Nil || c.Result == uuid.Nil || c.Actor == uuid.Nil ||
		c.Token == uuid.Nil || len(c.Key) > 120 || !idempotency.ValidKey(c.Key) ||
		c.Request.Direction != "export" || c.Request.validate() != nil {
		return v2input.ErrInvalid
	}
	run := uuid.NewSHA1(c.Tenant, []byte("interchange:create:"+c.Actor.String()+":"+c.Key))
	workflow := uuid.NewSHA1(run, []byte("workflow"))
	task := uuid.NewSHA1(workflow, []byte("v2.interchange.export:0"))
	if c.Run != run || c.Workflow != workflow || c.Task != task {
		return v2input.ErrInvalid
	}
	sha, err := canonical.Hash(c.Request)
	if err != nil || sha != c.RequestSHA {
		return v2input.ErrInvalid
	}
	return nil
}
