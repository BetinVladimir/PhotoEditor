package interchange

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type ExportBodyDependencyOrigin struct {
	Producer     ExportParamsOperationEvidence
	Params       v2input.Ref
	ParamsObject objectstore.Object
	Output       objectstore.Object
}

func exportBodyDependencyProducer(parent v2input.Entry, raw []byte, child v2input.Entry) (string, string, string, error) {
	if len(raw) < 2 || len(raw) > 1<<20 || parent.Contract != "Params" || !validImportRef(parent.Ref) || rawSHA(raw) != parent.Ref.SHA256 || !validImportRef(child.Ref) {
		return "", "", "", v2input.ErrInvalid
	}
	operation, err := exportParamsOperationShape(raw)
	if err != nil || operation != "geometry.body" {
		return "", "", "", v2input.ErrInvalid
	}
	refs, err := v2draft.DocumentChildren(raw, "Params", parent.Ref)
	if err != nil {
		return "", "", "", err
	}
	for _, ref := range refs {
		if ref.Ref != child.Ref || ref.Contract != child.Contract {
			continue
		}
		switch ref.Path {
		case "Document.poses":
			return "pose.detect", "pose_set", "gpu", nil
		case "Document.faces":
			return "face.landmarks", "face_landmarks", "gpu", nil
		case "Document.fit":
			return "pair.fit.body", "body_fit", "cpu", nil
		}
	}
	return "", "", "", v2input.ErrInvalid
}

// Metadata/original Params discovery only. Full original schema, model policy
// and source geometry must be qualified independently before export admission.
func (s SQLExportDocumentCatalog) LookupExportBodyDependencyOrigin(ctx context.Context, parent v2input.Entry, raw []byte, child v2input.Entry) (ExportBodyDependencyOrigin, error) {
	return lookupExportBodyDependencyOrigin(ctx, s, s, parent, raw, child)
}
func (s SQLExportResultInputCatalog) LookupExportBodyDependencyOrigin(ctx context.Context, parent v2input.Entry, raw []byte, child v2input.Entry) (ExportBodyDependencyOrigin, error) {
	config, err := exportPolicyCatalogConfig(s)
	if err != nil {
		return ExportBodyDependencyOrigin{}, err
	}
	return lookupExportBodyDependencyOrigin(ctx, s, config, parent, raw, child)
}

type exportBodyDependencyOriginCatalog interface {
	ExportDocumentCatalog
	ExportParamsOperationSource
}

func lookupExportBodyDependencyOrigin(ctx context.Context, documents exportBodyDependencyOriginCatalog, s SQLExportDocumentCatalog, parent v2input.Entry, raw []byte, child v2input.Entry) (ExportBodyDependencyOrigin, error) {
	var zero, out ExportBodyDependencyOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	operation, role, resource, err := exportBodyDependencyProducer(parent, body, child)
	if err != nil {
		return zero, err
	}
	consumer, err := documents.LookupExportParamsOperation(ctx, parent, body)
	if err != nil {
		return zero, err
	}
	var params struct {
		Image v2input.Ref `json:"image"`
	}
	if json.Unmarshal(body, &params) != nil || !validImportRef(params.Image) {
		return zero, v2input.ErrInvalid
	}
	output, err := documents.LookupExportDocument(ctx, child)
	if err != nil {
		return zero, err
	}
	err = tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT producer.id,producer.operation,producer.operation_version,producer.attempt,producer.fencing_token,
   original.id,original.sha256,original.bucket,original.object_key,original.object_version,original.sha256,original.bytes,original.media_type
   FROM photo.artifacts artifact
   JOIN photo.tasks producer ON producer.tenant_id=artifact.tenant_id AND producer.id=artifact.created_by_task_id
   JOIN photo.artifacts original ON original.tenant_id=producer.tenant_id AND original.id=producer.params_artifact_id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   JOIN photo.runs run ON run.tenant_id=producer.tenant_id AND run.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run.tenant_id AND workflow.run_id=run.id
   WHERE artifact.tenant_id=$1 AND artifact.id=$2 AND artifact.sha256=$3
   AND producer.operation=$4 AND producer.operation_version='1' AND producer.state='succeeded' AND producer.resource_class=$5
   AND artifact.output_role=$6 AND artifact.produced_attempt=producer.attempt AND artifact.produced_fence=producer.fencing_token
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text))
   AND attempt.finished_at IS NOT NULL AND attempt.backend=producer.execution_profile->>'backend' AND attempt.backend=attempt.profile->>'backend'
   AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$7::uuid,'sha256',$8::text))
   AND NOT run.cancel_requested AND (workflow.project_id=$9 OR run.config_snapshot->>'project_id'=$9::text)
   AND photo.v2_editor_authorized($1,$9,$10,$11)
   AND `+exportPromptWorkflowConsentSQL+`
   AND artifact.bucket=$12 AND artifact.object_key=$13 AND artifact.object_version=$14 AND artifact.bytes=$15 AND artifact.media_type='application/json'
   AND original.bucket=$12 AND original.media_type='application/json' AND original.bytes BETWEEN 2 AND 1048576
   AND original.id<>artifact.id
   AND NOT EXISTS(SELECT 1 FROM (VALUES(artifact.id),(original.id)) required(id) JOIN photo.artifacts a ON a.tenant_id=$1 AND a.id=required.id
    WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))`, s.Tenant, child.Ref.ArtifactID, child.Ref.SHA256, operation, resource, role, params.Image.ArtifactID, params.Image.SHA256, s.Request.ProjectID, s.Who.UserID, s.Who.SessionID, s.Bucket, output.Key, output.VersionID, output.Bytes).Scan(&out.Producer.Task, &out.Producer.Operation, &out.Producer.Version, &out.Producer.Attempt, &out.Producer.Fence, &out.Params.ArtifactID, &out.Params.SHA256, &bucket, &out.ParamsObject.Key, &out.ParamsObject.VersionID, &out.ParamsObject.SHA256, &out.ParamsObject.Bytes, &out.ParamsObject.ContentType)
		if err != nil {
			return err
		}
		if bucket != s.Bucket || !validImportRef(out.Params) || out.ParamsObject.SHA256 != out.Params.SHA256 || out.ParamsObject.Key == "" {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	final, err := documents.LookupExportParamsOperation(ctx, parent, body)
	if err != nil {
		return zero, err
	}
	if final != consumer {
		return zero, v2input.ErrInvalid
	}
	current, err := documents.LookupExportDocument(ctx, child)
	if err != nil {
		return zero, err
	}
	if current != output {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	out.Output = output
	return out, nil
}
