package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Canonical deployment file, provisioned by the operator alongside the SQL
// qualification policy. This is never part of an import/export API payload.
type ExportDetectorPolicyDeployment struct {
	Expected v2qualification.Expected
	Report   v2input.Ref
}

type ExportDetectorPolicyConfig struct {
	Schema int                             `json:"schema_version"`
	Report v2input.Ref                     `json:"qualification_report"`
	Locks  v2qualification.DeploymentLocks `json:"locks"`
}

func DecodeExportDetectorPolicyConfig(ctx context.Context, raw []byte) (ExportDetectorPolicyDeployment, error) {
	var zero ExportDetectorPolicyDeployment
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	var config ExportDetectorPolicyConfig
	if err := decodeImportDocument(raw, &config); err != nil {
		return zero, err
	}
	if config.Schema != 1 || !validImportRef(config.Report) || config.Locks.Operation != "face.detect" || config.Locks.Version != "1" {
		return zero, v2input.ErrInvalid
	}
	expected, err := config.Locks.Expected()
	if err != nil {
		return zero, err
	}
	if !validExportDetectorProfile(expected) {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ExportDetectorPolicyDeployment{Expected: expected, Report: config.Report}, nil
}
func LoadExportDetectorPolicyConfig(ctx context.Context, path string) (ExportDetectorPolicyDeployment, error) {
	var zero ExportDetectorPolicyDeployment
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	raw, err := readExportPolicyFile(ctx, path, 1<<20)
	if err != nil {
		return zero, err
	}
	return DecodeExportDetectorPolicyConfig(ctx, raw)
}
