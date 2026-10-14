package interchange

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// RejectUnsupportedImportDependency is the terminal codec for schemas without
// an installed typed remapper. It never copies archive JSON into a new asset.
// Operators may install more typed codecs ahead of it; unknown semantics stay
// visible as unsupported instead of becoming a partial successful import.
type RejectUnsupportedImportDependency struct{}

func (RejectUnsupportedImportDependency) RemapImportDocument(ctx context.Context, kind string, _ []byte, _, _ v2input.SessionVersionBinding, _ func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if kind == "Params" {
		return nil, &ImportRecipeUnsupported{Reason: "params_contract_not_installed"}
	}
	return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
}

func (RejectUnsupportedImportDependency) QualifyRemappedImportDocument(ctx context.Context, kind string, _ []byte, _ v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if kind == "Params" {
		return &ImportRecipeUnsupported{Reason: "params_contract_not_installed"}
	}
	return &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
}

var _ ImportDependencyCodec = RejectUnsupportedImportDependency{}
