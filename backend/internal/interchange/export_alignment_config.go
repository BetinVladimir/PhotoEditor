package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Canonical deployment file, provisioned by the operator alongside the SQL
// qualification policy. This is never part of an import/export API payload.
type ExportAlignmentPolicyDeployment struct {
	Expected v2qualification.Expected
	Report   v2input.Ref
}

type ExportAlignmentPolicyConfig struct {
	Schema int                             `json:"schema_version"`
	Report v2input.Ref                     `json:"qualification_report"`
	Locks  v2qualification.DeploymentLocks `json:"locks"`
}

func DecodeExportAlignmentPolicyConfig(ctx context.Context, raw []byte) (ExportAlignmentPolicyDeployment, error) {
	var zero ExportAlignmentPolicyDeployment
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	var config ExportAlignmentPolicyConfig
	if err := decodeImportDocument(raw, &config); err != nil {
		return zero, err
	}
	if config.Schema != 1 || !validImportRef(config.Report) || config.Locks.Operation != "pair.register" || config.Locks.Version != "1" {
		return zero, v2input.ErrInvalid
	}
	expected, err := config.Locks.Expected()
	if err != nil {
		return zero, err
	}
	if !validExportAlignmentProfile(expected) {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ExportAlignmentPolicyDeployment{Expected: expected, Report: config.Report}, nil
}
func LoadExportAlignmentPolicyConfig(ctx context.Context, path string) (ExportAlignmentPolicyDeployment, error) {
	var zero ExportAlignmentPolicyDeployment
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	raw, err := readExportPolicyFile(ctx, path, 1<<20)
	if err != nil {
		return zero, err
	}
	return DecodeExportAlignmentPolicyConfig(ctx, raw)
}
