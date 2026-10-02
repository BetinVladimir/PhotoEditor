package v2region

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
)

// ReadDescriptor expects a tenant-scoped transaction/connection. SQL proves
// live editor access and sealed input closure; bytes are verified independently.
type descriptorEvidence struct {
	Project, Task  uuid.UUID
	Node           []byte
	Source, Recipe RegionSourceMetadata
}

func (s Service) descriptorEvidence(ctx context.Context, tenant, asset, run, actor, session uuid.UUID, targetICC string) (descriptorEvidence, error) {
	var empty descriptorEvidence
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || !validRegionSHA(targetICC) {
		return empty, ErrRegionInvalid
	}
	var project, task uuid.UUID
	var raw []byte
	if err := s.DB.QueryRow(ctx, "SELECT project_id,task_id,node FROM photo.v26_region_descriptor_node($1,$2,$3,$4,$5)", tenant, run, asset, actor, session).Scan(&project, &task, &raw); err != nil {
		return empty, err
	}
	nodeJSON, err := canonical.Bytes(raw)
	if err != nil {
		return empty, ErrRegionInvalid
	}
	var node regionNodeDocument
	var params regionPlanDocument
	if json.Unmarshal(nodeJSON, &node) != nil || json.Unmarshal([]byte(node.ParamsJCS), &params) != nil {
		return empty, ErrRegionInvalid
	}
	source, err := ReadRegionSource(ctx, s.DB, s.Bucket, tenant, asset, params.Request.VersionID, actor, session)
	if err != nil {
		return empty, err
	}
	recipe, err := readRegionMetadata(ctx, s.DB.QueryRow(ctx, "SELECT * FROM photo.v26_region_recipe($1,$2,$3,$4,$5,$6,$7)", tenant, asset, params.Request.VersionID, actor, session, params.Request.Recipe.ArtifactID, params.Request.Recipe.SHA256), s.Bucket)
	if err != nil {
		return empty, err
	}
	return descriptorEvidence{project, task, nodeJSON, source, recipe}, nil
}

func (s Service) ReadDescriptor(ctx context.Context, tenant, asset, run, actor, session uuid.UUID, targetICC string) (RegionDescriptor, error) {
	evidence, err := s.descriptorEvidence(ctx, tenant, asset, run, actor, session, targetICC)
	if err != nil {
		return RegionDescriptor{}, err
	}
	return RestoreRegionDescriptor(ctx, s.Objects, evidence.Node, tenant, asset, evidence.Project, evidence.Task, s.Bucket, targetICC, evidence.Source, evidence.Recipe)
}

// ReadDescriptorScoped keeps object IO outside retryable SQL transactions and
// repeats current access/input proofs after the bounded object reads.
func (s Service) ReadDescriptorScoped(ctx context.Context, pool *pgxpool.Pool, tenant, asset, run, actor, session uuid.UUID, targetICC string) (RegionDescriptor, error) {
	if pool == nil {
		return RegionDescriptor{}, ErrRegionInvalid
	}
	read := func() (descriptorEvidence, error) {
		var out descriptorEvidence
		err := database.TenantTransaction(ctx, pool, tenant, func(tx pgx.Tx) error {
			local := s
			local.DB = tx
			var err error
			out, err = local.descriptorEvidence(ctx, tenant, asset, run, actor, session, targetICC)
			return err
		})
		return out, err
	}
	before, err := read()
	if err != nil {
		return RegionDescriptor{}, err
	}
	result, err := RestoreRegionDescriptor(ctx, s.Objects, before.Node, tenant, asset, before.Project, before.Task, s.Bucket, targetICC, before.Source, before.Recipe)
	if err != nil {
		return RegionDescriptor{}, err
	}
	after, err := read()
	if err != nil {
		return RegionDescriptor{}, err
	}
	a, err := canonical.Marshal(before)
	if err != nil {
		return RegionDescriptor{}, ErrRegionInvalid
	}
	b, err := canonical.Marshal(after)
	if err != nil || !bytes.Equal(a, b) {
		return RegionDescriptor{}, ErrRegionInvalid
	}
	return result, nil
}
