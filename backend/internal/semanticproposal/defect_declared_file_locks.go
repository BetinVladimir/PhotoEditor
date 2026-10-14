package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Locks exact declared metadata for an existing verified canonical origin.
// Must be composed with producer, qualification, inventory/auth locks; no queue.
func (s SQLTransferSourceReader) lockDefectDeclaredFilesInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectFaceExecutionPreparation) error {
	if tx == nil {
		return v2input.ErrInvalid
	}
	origin, body, err := s.marshalDefectDeclaredFileAuthority(ctx, who, tenant, binding, prepared)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_execution_files($1,$2,$3,$4,$5,$6,$7,$8,$9)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, string(origin), string(body)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Exact original declared file metadata/parent edges and producer refs for
// reuse by a composed SQL admission function. No object IO or authority grant.
func (s SQLTransferSourceReader) marshalDefectDeclaredFileAuthority(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectFaceExecutionPreparation) (json.RawMessage, json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if prepared.originProof.pins != prepared.origin || !s.validDefectOriginPreparationProof(who, tenant, binding, prepared.originProof) {
		return nil, nil, v2input.ErrInvalid
	}
	files, err := s.decodeDefectExecutionFilePins(ctx, who, tenant, binding, prepared.source)
	if err != nil {
		return nil, nil, err
	}
	var descriptors struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}
	if json.Unmarshal([]byte(prepared.source.descriptors), &descriptors) != nil || descriptors.Published.Ref != prepared.origin.Params {
		return nil, nil, v2input.ErrInvalid
	}
	selected, err := validateDefectProfileDescriptors(descriptors.Published, descriptors.Nodes)
	if err != nil {
		return nil, nil, err
	}
	entries := make([]map[string]any, 0, len(files))
	for _, file := range files {
		node := selected[file.Ref.ArtifactID]
		var parentID, parentSHA any
		if node.ParentRef.ArtifactID != uuid.Nil {
			parentID = node.ParentRef.ArtifactID
			parentSHA = node.ParentRef.SHA256
		}
		entries = append(entries, map[string]any{"contract": file.Contract, "artifact_id": file.Ref.ArtifactID, "sha256": file.Ref.SHA256, "bucket": s.Bucket, "key": file.Object.Key, "version": file.Object.VersionID, "bytes": file.Object.Bytes, "media_type": file.Object.ContentType, "parent_id": parentID, "parent_sha": parentSHA})
	}
	body, err := canonical.Marshal(entries)
	if err != nil || len(body) > 4<<20 {
		return nil, nil, v2input.ErrInvalid
	}
	pins := prepared.origin
	origin, err := canonical.Marshal([]v2input.Ref{pins.Proposal, pins.Report, pins.Labels, pins.Protection, pins.Params})
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return origin, body, nil
}
