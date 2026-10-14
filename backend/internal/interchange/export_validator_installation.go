package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

type ExportDocumentValidatorFactory interface {
	BuildExportDocumentValidator(context.Context, CreateIntent, FrozenExportDocuments) (ExportTypedDocumentValidator, error)
}

// Locks come from independently qualified deployment assets. Other must install
// StyleV2/Override and every remaining supported schema; Semantics supplies the
// source/model/consent/operation checks for dependency documents.
type ExportDocumentValidatorInstallation struct {
	ICCSHA, ModelSHA string
	ModelWeights     map[string]string
	MaskModelLocks   []string
	Semantics        ExportDependencySemantics
	SemanticsFactory ExportDependencySemanticsFactory
	Other            ExportTypedDocumentValidator
}

func (s ExportDocumentValidatorInstallation) BuildExportDocumentValidator(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments) (ExportTypedDocumentValidator, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Semantics == nil || s.SemanticsFactory != nil || s.Other == nil {
		return nil, v2input.ErrInvalid
	}
	source, err := NewExportSourceSemantics(ctx, intent, frozen, s.Semantics)
	if err != nil {
		return nil, err
	}
	semantics, err := NewExportCanonicalMaskSemantics(ctx, intent, frozen, append([]string(nil), s.MaskModelLocks...), source)
	if err != nil {
		return nil, err
	}
	dependencies := ExportDependencyDocumentValidator{Semantics: semantics, Other: s.Other}
	portable, err := NewExportRecipeDocumentValidator(ctx, intent, frozen, dependencies)
	if err != nil {
		return nil, err
	}
	render, err := NewExportFrozenDocumentValidator(ctx, intent, frozen, s.ICCSHA, s.ModelSHA, portable)
	if err != nil {
		return nil, err
	}
	working, err := NewExportWorkingDocumentValidator(ctx, intent, frozen, s.ICCSHA, render)
	if err != nil {
		return nil, err
	}
	return working, ctx.Err()
}

var _ ExportDocumentValidatorFactory = ExportDocumentValidatorInstallation{}
