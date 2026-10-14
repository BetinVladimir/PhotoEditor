package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// LookupExportParamsOperation binds original bytes to a completed current
// producer. Shape selects a supported schema but never grants authority.
func (s SQLExportDocumentCatalog) LookupExportParamsOperation(ctx context.Context, entry v2input.Entry, raw []byte) (ExportParamsOperationEvidence, error) {
	return lookupExportParamsOperation(ctx, s, s.DB, s.Tenant, s.Who, s.Request.ProjectID, s.Bucket, entry, raw)
}
func (s SQLExportResultInputCatalog) LookupExportParamsOperation(ctx context.Context, entry v2input.Entry, raw []byte) (ExportParamsOperationEvidence, error) {
	config := s.Export
	return lookupExportParamsOperation(ctx, s, config.DB, config.Tenant, config.Who, config.Request.ProjectID, config.Bucket, entry, raw)
}
func lookupExportParamsOperation(ctx context.Context, catalog ExportDocumentCatalog, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, project uuid.UUID, bucketName string, entry v2input.Entry, raw []byte) (ExportParamsOperationEvidence, error) {
	var zero, out ExportParamsOperationEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || db == nil || tenantID == uuid.Nil || project == uuid.Nil || len(bucketName) < 3 || entry.Contract != "Params" || !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != entry.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return zero, v2input.ErrInvalid
	}
	operation, err := exportParamsOperationShape(raw)
	if err != nil {
		return zero, err
	}
	if operation == "" {
		return zero, v2input.ErrInvalid
	}
	var fields struct {
		Image v2input.Ref `json:"image"`
		Base  struct {
			Source v2input.Ref `json:"source"`
		} `json:"base"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return zero, v2input.ErrInvalid
	}
	if operation == "portrait.hair" || operation == "portrait.clothing" {
		fields.Image = fields.Base.Source
	}
	if !validImportRef(fields.Image) {
		return zero, v2input.ErrInvalid
	}
	requiredInputs := []v2input.Ref{}
	if operation == "geometry.body" {
		requiredInputs, err = exportBodyRequiredInputs(entry, raw)
		if err != nil {
			return zero, err
		}
	}
	requiredBody, err := canonical.Marshal(requiredInputs)
	if err != nil {
		return zero, err
	}
	object, err := catalog.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	if object.Bytes != int64(len(raw)) {
		return zero, v2input.ErrInvalid
	}
	qualify := func() error {
		return tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
			rows, err := tx.Query(ctx, `SELECT producer.id,producer.operation,producer.operation_version,producer.attempt,producer.fencing_token
   FROM photo.tasks producer
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id AND workflow.run_id=origin.id
   WHERE producer.tenant_id=$1 AND params.id=$2 AND params.sha256=$3 AND producer.operation=$4 AND producer.operation_version='1' AND producer.state='succeeded'
   AND attempt.finished_at IS NOT NULL AND attempt.backend=producer.execution_profile->>'backend' AND attempt.backend=attempt.profile->>'backend'
   AND NOT origin.cancel_requested AND (workflow.project_id=$5 OR origin.config_snapshot->>'project_id'=$5::text)
   AND photo.v2_editor_authorized($1,$5,$6,$7)
   AND `+exportPromptWorkflowConsentSQL+`
   AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$8::uuid,'sha256',$9::text))
   AND ($4::text<>'geometry.body' OR producer.inputs @> $14::jsonb)
   AND params.bucket=$10 AND params.object_key=$11 AND params.object_version=$12 AND params.bytes=$13 AND params.media_type='application/json'
   AND params.state='ready' AND params.verified_at IS NOT NULL AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null')
   AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
   AND EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
    WHERE v.tenant_id=$1 AND a.project_id=$5 AND v.master_artifact_id=$8 AND v.content_hash=$9 AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified')
   ORDER BY producer.id LIMIT 2`, tenantID, entry.Ref.ArtifactID, entry.Ref.SHA256, operation, project, who.UserID, who.SessionID, fields.Image.ArtifactID, fields.Image.SHA256, bucketName, object.Key, object.VersionID, object.Bytes, requiredBody)
			if err != nil {
				return err
			}
			defer rows.Close()
			if !rows.Next() {
				if err := rows.Err(); err != nil {
					return err
				}
				return v2input.ErrInvalid
			}
			if err := rows.Scan(&out.Task, &out.Operation, &out.Version, &out.Attempt, &out.Fence); err != nil {
				return err
			}
			// Shared Params with multiple completed producers has no unique fence proof.
			if rows.Next() {
				return v2input.ErrInvalid
			}
			if err := rows.Err(); err != nil {
				return err
			}
			return ctx.Err()
		})
	}
	if err := qualify(); err != nil {
		return zero, err
	}
	before := out
	current, err := catalog.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	if current != object {
		return zero, v2input.ErrInvalid
	}
	// Metadata IO may race with a producer transition or consumed grant revocation.
	if err := qualify(); err != nil {
		return zero, err
	}
	if out != before {
		return zero, v2input.ErrInvalid
	}
	return out, ctx.Err()
}

var _ ExportParamsOperationSource = SQLExportDocumentCatalog{}
var _ ExportParamsOperationSource = SQLExportResultInputCatalog{}

// Worker geometry.body requires these exact pins, in addition to independent image
// pixels/alpha and transform-chain qualification. Do not treat membership as
// PoseSet/BodyFit semantic or model provenance proof.
func exportBodyRequiredInputs(entry v2input.Entry, raw []byte) ([]v2input.Ref, error) {
	if entry.Contract != "Params" || !validImportRef(entry.Ref) || rawSHA(raw) != entry.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	operation, err := exportParamsOperationShape(raw)
	if err != nil || operation != "geometry.body" {
		return nil, v2input.ErrInvalid
	}
	children, err := v2draft.DocumentChildren(raw, "Params", entry.Ref)
	if err != nil {
		return nil, err
	}
	refs := []v2input.Ref{}
	for _, child := range children {
		switch child.Path {
		case "Document.image", "Document.poses", "Document.person.artifact", "Document.fit", "Document.faces":
			refs = append(refs, child.Ref)
		}
	}
	if len(refs) < 3 || len(refs) > 5 {
		return nil, v2input.ErrInvalid
	}
	return refs, nil
}
