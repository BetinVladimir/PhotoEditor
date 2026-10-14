package interchange

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportResolvedOrigin(ctx context.Context,
	entry v2input.Entry, raw []byte) (ExportResolvedOriginEvidence, error) {
	var zero, evidence ExportResolvedOriginEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || entry.Contract != "ResolvedStyle" ||
		!validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 ||
		rawSHA(raw) != entry.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	children, err := exportResolvedChildren(entry, raw)
	if err != nil {
		return zero, err
	}
	var body struct {
		Params struct {
			Image v2input.Ref `json:"image"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &body) != nil || !validImportRef(body.Params.Image) {
		return zero, v2input.ErrInvalid
	}
	inputs := make([]v2input.Ref, 0, len(children))
	for _, child := range children {
		if child.Contract != "AbsoluteLUTArray" {
			inputs = append(inputs, child.Ref)
		}
	}
	inputRaw, err := canonical.Marshal(inputs)
	if err != nil {
		return zero, err
	}
	resolved, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	image := v2input.Entry{Ref: body.Params.Image, Contract: "WorkingImage"}
	imageObject, err := s.LookupExportDocument(ctx, image)
	if err != nil {
		return zero, err
	}
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT params_id,params_sha,params_bucket,params_key,
 params_version,params_sha,params_bytes,params_type
 FROM photo.v30_export_adoption_resolved_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14::jsonb,$15,$16,$17,$18)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, entry.Ref.ArtifactID, entry.Ref.SHA256,
		image.Ref.ArtifactID, image.Ref.SHA256, inputRaw,
		s.Bucket, resolved.Key, resolved.VersionID, resolved.Bytes).Scan(
		&evidence.Params.ArtifactID, &evidence.Params.SHA256, &bucket,
		&evidence.Object.Key, &evidence.Object.VersionID,
		&evidence.Object.SHA256, &evidence.Object.Bytes, &evidence.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if !validImportRef(evidence.Params) || bucket != s.Bucket ||
		evidence.Object.Key == "" || evidence.Object.VersionID == "" ||
		evidence.Object.VersionID == "null" ||
		evidence.Object.SHA256 != evidence.Params.SHA256 ||
		evidence.Object.Bytes < 2 || evidence.Object.Bytes > 1<<20 ||
		evidence.Object.ContentType != "application/json" {
		return zero, v2input.ErrInvalid
	}
	currentResolved, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	currentImage, err := s.LookupExportDocument(ctx, image)
	if err != nil {
		return zero, err
	}
	if currentResolved != resolved || currentImage != imageObject {
		return zero, v2input.ErrInvalid
	}
	return evidence, ctx.Err()
}

var _ ExportResolvedOriginSource = SQLSchedulerExportInputCatalog{}
