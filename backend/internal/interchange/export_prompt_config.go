package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Canonical deployment file, provisioned by the operator alongside the SQL
// qualification policy. This is never part of an import/export API payload.
type ExportPromptMaskPolicyConfig struct {
	Schema int                             `json:"schema_version"`
	Report v2input.Ref                     `json:"qualification_report"`
	Locks  v2qualification.DeploymentLocks `json:"locks"`
}

func DecodeExportPromptMaskPolicyConfig(ctx context.Context, raw []byte) (SQLExportPromptMaskPolicyInstallation, error) {
	var zero SQLExportPromptMaskPolicyInstallation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	var config ExportPromptMaskPolicyConfig
	if err := decodeImportDocument(raw, &config); err != nil {
		return zero, err
	}
	if config.Schema != 1 || !validImportRef(config.Report) || config.Locks.Operation != "mask.prompt" || config.Locks.Version != "1" {
		return zero, v2input.ErrInvalid
	}
	expected, err := config.Locks.Expected()
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return SQLExportPromptMaskPolicyInstallation{Expected: expected, Report: config.Report}, nil
}
func LoadExportPromptMaskPolicyConfig(ctx context.Context, path string) (SQLExportPromptMaskPolicyInstallation, error) {
	var zero SQLExportPromptMaskPolicyInstallation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	raw, err := readExportPolicyFile(ctx, path, 1<<20)
	if err != nil {
		return zero, err
	}
	return DecodeExportPromptMaskPolicyConfig(ctx, raw)
}
