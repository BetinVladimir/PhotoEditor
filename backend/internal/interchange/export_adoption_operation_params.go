package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportParamsOperation(ctx context.Context,
	entry v2input.Entry, raw []byte) (ExportParamsOperationEvidence, error) {
	var zero, out ExportParamsOperationEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || entry.Contract != "Params" ||
		!validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 1<<20 ||
		rawSHA(raw) != entry.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return zero, v2input.ErrInvalid
	}
	operation, err := exportParamsOperationShape(raw)
	if err != nil || operation == "" {
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
	required := []v2input.Ref{}
	if operation == "geometry.body" {
		required, err = exportBodyRequiredInputs(entry, raw)
		if err != nil {
			return zero, err
		}
	}
	requiredRaw, err := canonical.Marshal(required)
	if err != nil {
		return zero, err
	}
	paramsObject, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	image := v2input.Entry{Ref: fields.Image, Contract: "WorkingImage"}
	imageObject, err := s.LookupExportDocument(ctx, image)
	if err != nil {
		return zero, err
	}
	if paramsObject.Bytes != int64(len(raw)) {
		return zero, v2input.ErrInvalid
	}
	qualify := func() (ExportParamsOperationEvidence, error) {
		var evidence ExportParamsOperationEvidence
		rows, err := s.Claims.DB.Query(ctx, `SELECT producer_task,producer_operation,
 producer_version,producer_attempt,producer_fence
 FROM photo.v30_export_adoption_operation_params($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14,$15,$16,$17,$18,$19::jsonb)`,
			s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
			s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
			s.Claim.Token, entry.Ref.ArtifactID, entry.Ref.SHA256, operation,
			fields.Image.ArtifactID, fields.Image.SHA256,
			s.Bucket, paramsObject.Key, paramsObject.VersionID, paramsObject.Bytes,
			requiredRaw)
		if err != nil {
			return evidence, err
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return evidence, err
			}
			return evidence, v2input.ErrInvalid
		}
		if err := rows.Scan(&evidence.Task, &evidence.Operation, &evidence.Version,
			&evidence.Attempt, &evidence.Fence); err != nil {
			return evidence, err
		}
		if rows.Next() {
			return evidence, v2input.ErrInvalid
		}
		if err := rows.Err(); err != nil {
			return evidence, err
		}
		return evidence, ctx.Err()
	}
	out, err = qualify()
	if err != nil {
		return zero, err
	}
	if out.Operation != operation || out.Version != "1" || out.Attempt < 1 || out.Fence < 1 {
		return zero, v2input.ErrInvalid
	}
	currentParams, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	currentImage, err := s.LookupExportDocument(ctx, image)
	if err != nil {
		return zero, err
	}
	if currentParams != paramsObject || currentImage != imageObject {
		return zero, v2input.ErrInvalid
	}
	current, err := qualify()
	if err != nil {
		return zero, err
	}
	if current != out {
		return zero, v2input.ErrInvalid
	}
	return out, ctx.Err()
}

var _ ExportParamsOperationSource = SQLSchedulerExportInputCatalog{}
