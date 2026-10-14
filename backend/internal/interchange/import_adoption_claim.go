package interchange

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// ImportAdoptionClaim only leases discovery. It grants no permission to read
// output objects or commit adoption; both require current SQL authority.
type ImportAdoptionClaim struct {
	Tenant, Workflow, Run, Task, Result uuid.UUID
	Attempt                             int32
	Fence                               int64
	Token                               uuid.UUID
}

func (c ImportAdoptionClaim) validate() error {
	if c.Tenant == uuid.Nil || c.Workflow == uuid.Nil || c.Run == uuid.Nil ||
		c.Task == uuid.Nil || c.Result == uuid.Nil || c.Token == uuid.Nil ||
		c.Attempt < 1 || c.Fence < 1 ||
		c.Task != uuid.NewSHA1(c.Workflow, []byte("v2.interchange.import:0")) {
		return v2input.ErrInvalid
	}
	return nil
}

type SQLImportAdoptionClaimStore struct{ DB *pgxpool.Pool }

func (s SQLImportAdoptionClaimStore) NextImportAdoption(ctx context.Context) (*ImportAdoptionClaim, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, v2input.ErrInvalid
	}
	var claim ImportAdoptionClaim
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,workflow_id,run_id,task_id,result_id,attempt,fencing_token,claim_token FROM photo.v30_claim_next_import_adoption()`).
		Scan(&claim.Tenant, &claim.Workflow, &claim.Run, &claim.Task, &claim.Result,
			&claim.Attempt, &claim.Fence, &claim.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := claim.validate(); err != nil {
		return nil, err
	}
	return &claim, ctx.Err()
}

func (s SQLImportAdoptionClaimStore) CurrentImportAdoptionClaim(ctx context.Context, claim ImportAdoptionClaim) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || claim.validate() != nil {
		return false, v2input.ErrInvalid
	}
	var current bool
	err := s.DB.QueryRow(ctx, `SELECT photo.v30_import_adoption_claim_current($1,$2,$3,$4,$5,$6,$7,$8)`,
		claim.Tenant, claim.Workflow, claim.Run, claim.Task, claim.Result, claim.Attempt,
		claim.Fence, claim.Token).Scan(&current)
	if err != nil {
		return false, err
	}
	return current, ctx.Err()
}

func (s SQLImportAdoptionClaimStore) RenewImportAdoptionClaim(ctx context.Context, claim ImportAdoptionClaim) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || claim.validate() != nil {
		return false, v2input.ErrInvalid
	}
	var renewed bool
	err := s.DB.QueryRow(ctx, `SELECT photo.v30_renew_import_adoption_claim($1,$2,$3,$4,$5,$6,$7,$8)`,
		claim.Tenant, claim.Workflow, claim.Run, claim.Task, claim.Result, claim.Attempt,
		claim.Fence, claim.Token).Scan(&renewed)
	if err != nil {
		return false, err
	}
	return renewed, ctx.Err()
}

// RootImportAdoption returns the first staged result under the current claim.
// The object version is an IO pin, not authority to accept its JSON content.
func (s SQLImportAdoptionClaimStore) RootImportAdoption(ctx context.Context, claim ImportAdoptionClaim, bucket string) (v2input.Ref, objectstore.Object, error) {
	var ref v2input.Ref
	var object objectstore.Object
	if err := ctx.Err(); err != nil {
		return ref, object, err
	}
	if s.DB == nil || claim.validate() != nil || len(bucket) < 3 || len(bucket) > 63 {
		return ref, object, v2input.ErrInvalid
	}
	var foundBucket string
	err := s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type
 FROM photo.v30_import_adoption_root_output($1,$2,$3,$4,$5,$6,$7,$8)`,
		claim.Tenant, claim.Workflow, claim.Run, claim.Task, claim.Result,
		claim.Attempt, claim.Fence, claim.Token).Scan(&ref.ArtifactID, &ref.SHA256,
		&foundBucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return v2input.Ref{}, objectstore.Object{}, v2input.ErrInvalid
	}
	if err != nil {
		return v2input.Ref{}, objectstore.Object{}, err
	}
	object.SHA256 = ref.SHA256
	if foundBucket != bucket || !validImportRef(ref) || object.Key == "" ||
		object.VersionID == "" || object.VersionID == "null" || object.Bytes < 2 ||
		object.Bytes > 65536 || object.ContentType != "application/json" {
		return v2input.Ref{}, objectstore.Object{}, v2input.ErrInvalid
	}
	return ref, object, ctx.Err()
}

// LookupImportOutput never accepts an unscoped artifact: SQL matches the
// canonical result position, producer attempt/fence, role and current claim.
func (s SQLImportAdoptionClaimStore) LookupImportOutput(ctx context.Context,
	claim ImportAdoptionClaim, ref v2input.Ref, contract, bucket string) (objectstore.Object, error) {
	var object objectstore.Object
	if err := ctx.Err(); err != nil {
		return object, err
	}
	if s.DB == nil || claim.validate() != nil || !validImportRef(ref) ||
		(contract != "InterchangeResult" && contract != "MappingReport" && contract != "MappingShard") ||
		len(bucket) < 3 || len(bucket) > 63 {
		return object, v2input.ErrInvalid
	}
	var foundBucket string
	err := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type
 FROM photo.v30_import_adoption_output($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		claim.Tenant, claim.Workflow, claim.Run, claim.Task, claim.Result,
		claim.Attempt, claim.Fence, claim.Token, ref.ArtifactID, ref.SHA256, contract).
		Scan(&foundBucket, &object.Key, &object.VersionID, &object.SHA256,
			&object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	limit := int64(8 << 20)
	if contract == "InterchangeResult" {
		limit = 65536
	}
	if foundBucket != bucket || object.SHA256 != ref.SHA256 || object.Key == "" ||
		object.VersionID == "" || object.VersionID == "null" || object.Bytes < 2 ||
		object.Bytes > limit || object.ContentType != "application/json" {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return object, ctx.Err()
}
