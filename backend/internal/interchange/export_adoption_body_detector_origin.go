package interchange

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportBodyFitDetectorOrigin(ctx context.Context,
	landmarkOrigin ExportBodyDependencyOrigin, parent v2input.Entry, raw []byte,
	fit ExportClosureNode, landmark v2input.Entry, landmarkParams []byte,
	child v2input.Entry) (ExportBodyDependencyOrigin, error) {
	var zero, out ExportBodyDependencyOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil {
		return zero, v2input.ErrInvalid
	}
	fit = ExportClosureNode{Entry: fit.Entry, Body: append([]byte(nil), fit.Body...)}
	feature, err := exportBodyFitDetectorEdge(landmarkOrigin, fit, landmark, landmarkParams, child)
	if err != nil {
		return zero, err
	}
	body := append([]byte(nil), raw...)
	if err := s.QualifyExportBodyFitInputInventory(ctx, parent, body, fit); err != nil {
		return zero, err
	}
	consumer, err := s.LookupExportBodyFitFeatureOrigin(ctx, parent, body, fit, landmark)
	if err != nil {
		return zero, err
	}
	if consumer != landmarkOrigin {
		return zero, v2input.ErrInvalid
	}
	output, err := s.LookupExportDocument(ctx, child)
	if err != nil {
		return zero, err
	}
	rows, err := s.Claims.DB.Query(ctx, `SELECT producer_task,producer_operation,producer_version,
 producer_attempt,producer_fence,params_id,params_sha,params_bucket,params_key,
 params_version,params_bytes,params_type
 FROM photo.v30_export_adoption_body_detector_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, landmark.Ref.ArtifactID, landmark.Ref.SHA256,
		landmarkOrigin.Producer.Task, landmarkOrigin.Producer.Attempt,
		landmarkOrigin.Producer.Fence, landmarkOrigin.Params.ArtifactID,
		child.Ref.ArtifactID, child.Ref.SHA256,
		feature.Image.ArtifactID, feature.Image.SHA256,
		s.Bucket, output.Key, output.VersionID, output.Bytes)
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return zero, err
		}
		return zero, v2input.ErrInvalid
	}
	var bucket string
	if err := rows.Scan(&out.Producer.Task, &out.Producer.Operation,
		&out.Producer.Version, &out.Producer.Attempt, &out.Producer.Fence,
		&out.Params.ArtifactID, &out.Params.SHA256, &bucket,
		&out.ParamsObject.Key, &out.ParamsObject.VersionID,
		&out.ParamsObject.Bytes, &out.ParamsObject.ContentType); err != nil {
		return zero, err
	}
	if rows.Next() {
		return zero, v2input.ErrInvalid
	}
	if err := rows.Err(); err != nil {
		return zero, err
	}
	out.ParamsObject.SHA256 = out.Params.SHA256
	if out.Producer.Operation != "face.detect" || out.Producer.Version != "1" ||
		out.Producer.Attempt < 1 || out.Producer.Fence < 1 ||
		!validImportRef(out.Params) || bucket != s.Bucket ||
		out.ParamsObject.Key == "" || out.ParamsObject.VersionID == "" ||
		out.ParamsObject.VersionID == "null" ||
		out.ParamsObject.ContentType != "application/json" ||
		out.ParamsObject.Bytes < 2 || out.ParamsObject.Bytes > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	current, err := s.LookupExportBodyFitFeatureOrigin(ctx, parent, body, fit, landmark)
	if err != nil || current != consumer {
		return zero, v2input.ErrInvalid
	}
	currentOutput, err := s.LookupExportDocument(ctx, child)
	if err != nil || currentOutput != output {
		return zero, v2input.ErrInvalid
	}
	if err := s.QualifyExportBodyFitInputInventory(ctx, parent, body, fit); err != nil {
		return zero, err
	}
	out.Output = output
	return out, ctx.Err()
}

var _ ExportBodyFitDetectorOriginSource = SQLSchedulerExportInputCatalog{}
