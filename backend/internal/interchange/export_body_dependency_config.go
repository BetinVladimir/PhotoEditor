package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Operator-only complete policy inventory. No partial set or inferred model
// identifiers; reports must independently qualify each operation.
type ExportBodyDependencyPolicyFile struct {
	Schema   int                                   `json:"schema_version"`
	Policies []ExportBodyDependencyPolicyFileEntry `json:"policies"`
}
type ExportBodyDependencyPolicyFileEntry struct {
	Report v2input.Ref                     `json:"qualification_report"`
	Locks  v2qualification.DeploymentLocks `json:"locks"`
}

func DecodeExportBodyDependencyPolicyConfig(ctx context.Context, raw []byte) ([]ExportBodyDependencyPolicyConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	var file ExportBodyDependencyPolicyFile
	if err := decodeImportDocument(raw, &file); err != nil {
		return nil, err
	}
	if file.Schema != 1 || len(file.Policies) != 3 {
		return nil, v2input.ErrInvalid
	}
	seen := map[string]bool{}
	reports := map[[16]byte]bool{}
	configs := []ExportBodyDependencyPolicyConfig{}
	for _, entry := range file.Policies {
		operation := entry.Locks.Operation
		if !exportBodyDependencyQualificationOperation(operation) || seen[operation] || entry.Locks.Version != "1" || !validImportRef(entry.Report) || reports[entry.Report.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		seen[operation] = true
		reports[entry.Report.ArtifactID] = true
		expected, err := entry.Locks.Expected()
		if err != nil {
			return nil, err
		}
		if operation == "pair.fit.body" && expected.Backend != "cpu" {
			return nil, v2input.ErrInvalid
		}
		configs = append(configs, ExportBodyDependencyPolicyConfig{Report: entry.Report, Expected: expected})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return configs, nil
}
func LoadExportBodyDependencyPolicyConfig(ctx context.Context, path string) ([]ExportBodyDependencyPolicyConfig, error) {
	var zero []ExportBodyDependencyPolicyConfig
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	raw, err := readExportPolicyFile(ctx, path, 1<<20)
	if err != nil {
		return zero, err
	}
	return DecodeExportBodyDependencyPolicyConfig(ctx, raw)
}
