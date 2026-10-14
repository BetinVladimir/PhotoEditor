package interchange

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportCorrectionEvidence(ctx context.Context,
	entry v2input.Entry, raw []byte) (ExportCorrectionProducerEvidence, error) {
	var zero ExportCorrectionProducerEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil ||
		entry.Contract != "CorrectionPlan" || !validImportRef(entry.Ref) ||
		len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != entry.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return zero, v2input.ErrInvalid
	}
	before, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	if before.ContentType != "application/json" || before.Bytes != int64(len(raw)) {
		return zero, v2input.ErrInvalid
	}
	var packet []byte
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_correction_evidence(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, entry.Ref.ArtifactID, entry.Ref.SHA256, string(raw),
		s.Bucket, before.Key, before.VersionID, before.Bytes).Scan(&packet)
	if err != nil {
		return zero, err
	}
	normalized, err = canonical.Bytes(packet)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var evidence ExportCorrectionProducerEvidence
	if err := decodeImportDocument(normalized, &evidence); err != nil {
		return zero, err
	}
	if !evidence.valid() || evidence.Bucket != s.Bucket {
		return zero, v2input.ErrInvalid
	}
	after, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	if after != before {
		return zero, v2input.ErrInvalid
	}
	return evidence, ctx.Err()
}

var _ ExportCorrectionProducerSource = SQLSchedulerExportInputCatalog{}
