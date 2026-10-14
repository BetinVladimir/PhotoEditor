package interchange

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Physical array validity never substitutes for consent, model qualification or
// protected-region policy. Each stage receives an independent document copy.
// Repeat physical authority after semantic IO before retaining a parent witness.
type importPreparedMaskQualification struct{ Physical, Semantic ImportMaskQualifier }

func (q importPreparedMaskQualification) QualifyImportedMask(ctx context.Context, mask review.MaskRef, binding v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.Physical == nil || q.Semantic == nil {
		return v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(mask)
	if err != nil {
		return err
	}
	for _, stage := range []ImportMaskQualifier{q.Physical, q.Semantic, q.Physical} {
		if err := ctx.Err(); err != nil {
			return err
		}
		owned, err := review.ParseMaskRef(append([]byte(nil), raw...))
		if err != nil {
			return err
		}
		if err := stage.QualifyImportedMask(ctx, owned, binding); err != nil {
			return err
		}
	}
	return ctx.Err()
}

type PreparedImportResolverDependencies struct {
	Registry        *ImportPreparedNodeRegistry
	Catalog         SQLPreparedImportCatalog
	Objects         ImportDocumentObjects
	Masters         ImportMasterReader
	Working, Assets ImportClosureNodeResolver
	Canonical       *ImportWorkingWitnesses
	Documents       ImportTypedDocumentValidator
	Codec           ImportDependencyCodec
	MaskSemantics   ImportMaskQualifier
	ModelLocks      []string
	Scratch         *ExportArchiveScratch
	Probe           ExportNativeProbe
	ArrayRuntimeSHA string
	MaxBytes        int64
}

// Builds one entry-scoped closure reader. Operator semantic authorities remain
// mandatory; schema-only validation cannot enable imported edits.
func NewSQLPreparedImportResolver(ctx context.Context, d PreparedImportResolverDependencies) (ImportPreparedResolverRouter, error) {
	var zero ImportPreparedResolverRouter
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if d.Objects == nil || d.Masters == nil || d.Working == nil || d.Assets == nil || d.Canonical == nil || d.Documents == nil || d.Codec == nil || d.MaskSemantics == nil {
		return zero, v2input.ErrInvalid
	}
	catalog := d.Catalog
	catalog.Store.Parsed = append([]byte(nil), catalog.Store.Parsed...)
	if catalog.Scope.Warp != nil {
		owned := *catalog.Scope.Warp
		catalog.Scope.Warp = &owned
	}
	if catalog.Scope.Background != nil {
		owned := *catalog.Scope.Background
		catalog.Scope.Background = &owned
	}
	// Validate scope and parsed authority before constructing native readers.
	scope := catalog.Scope
	scope.Contract = "MaskRef"
	scope.Warp = nil
	scope.Background = nil
	if _, _, err := catalog.Store.receipt(scope, scope.Binding.Source, 2, "application/json"); err != nil {
		return zero, err
	}
	physical, err := NewImportPhysicalMaskQualifier(d.Masters, catalog, d.Objects, append([]string(nil), d.ModelLocks...), d.MaxBytes)
	if err != nil {
		return zero, err
	}
	witnesses := &ImportPreparedMaskWitnesses{}
	lutWitnesses := &ImportPreparedLUTWitnesses{}
	defectLabelWitnesses := &ImportPreparedDefectLabelWitnesses{}
	masks, err := NewSQLPreparedImportMaskResolver(ctx, catalog, d.Objects, d.Masters, witnesses, d.Scratch, d.Probe, d.ArrayRuntimeSHA, d.MaxBytes)
	if err != nil {
		return zero, err
	}
	warps, err := NewSQLPreparedImportWarpResolver(ctx, catalog, d.Objects, d.Scratch, d.Probe, d.ArrayRuntimeSHA, d.MaxBytes)
	if err != nil {
		return zero, err
	}
	codecValidator := ImportCodecDocumentValidator{Binding: scope.Binding, Codec: d.Codec, Children: d.Documents}
	validator := ImportPreparedMaskDocumentValidator{Binding: scope.Binding, Qualifier: importPreparedMaskQualification{Physical: physical, Semantic: d.MaskSemantics}, Witnesses: witnesses, LUT: lutWitnesses, DefectLabels: defectLabelWitnesses, Other: codecValidator}
	documents := ImportJSONNodeResolver{Catalog: catalog, Objects: d.Objects, Validator: validator}
	parentDocuments := ImportJSONNodeResolver{Catalog: catalog, Objects: d.Objects, Validator: codecValidator}
	lut, err := NewSQLPreparedImportLUTResolver(ctx, catalog, d.Objects, lutWitnesses, parentDocuments, d.Scratch, d.Probe, d.ArrayRuntimeSHA, d.MaxBytes)
	if err != nil {
		return zero, err
	}
	labels, err := NewSQLPreparedImportDefectLabelResolver(ctx, catalog, d.Objects, defectLabelWitnesses, parentDocuments, d.MaxBytes)
	if err != nil {
		return zero, err
	}
	return ImportPreparedResolverRouter{JSON: documents, Working: d.Working, Masks: masks, Warps: warps, LUT: lut, DefectLabels: labels, Assets: d.Assets, Canonical: d.Canonical}, ctx.Err()
}
