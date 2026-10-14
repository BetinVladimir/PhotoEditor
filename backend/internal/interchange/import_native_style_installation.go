package interchange

import (
	"bytes"
	"context"
	"photoeditor/backend/internal/v2input"
)

// Pins are operator deployment inputs. They are not copied from archived
// quality/provenance fields or inferred from an installed executable path.
type ImportNativeStyleInstallation struct {
	WorkICCSHA, StyleRuntimeSHA, ImageRuntimeSHA string
	Models                                       map[string]string
}

func (i ImportNativeStyleInstallation) Build(ctx context.Context, parsed []byte, bindings []ImportCorrectionBinding, sources ImportRecipeSourceQualifier, masters ImportMasterReader, publisher PreparedImportArtifactPublisher, native ImportResolverDependencies) (ImportStyleQualifier, ImportStyleBackgroundRemapper, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	store, ok := publisher.Store.(SQLPreparedImportArtifactStore)
	if !ok || publisher.SourceObjects == nil || masters == nil || sources == nil || native.WarpScratch == nil || native.WarpProbe == nil || !validImportRefForSHA(i.WorkICCSHA) || !validImportRefForSHA(i.StyleRuntimeSHA) || !validImportRefForSHA(i.ImageRuntimeSHA) || i.Models == nil {
		return nil, nil, v2input.ErrInvalid
	}
	if !bytes.Equal(store.Parsed, parsed) {
		return nil, nil, v2input.ErrInvalid
	}
	scope := publisher.Scope
	scope.Contract = "MaskRef"
	scope.Background = nil
	scope.Warp = nil
	if _, _, err := store.receipt(scope, scope.Binding.Source, 2, "application/json"); err != nil {
		return nil, nil, err
	}
	targets, err := NewBoundImportBackgroundAuthority(append([]ImportCorrectionBinding(nil), bindings...), sources)
	if err != nil {
		return nil, nil, err
	}
	style, err := NewNativeImportStyleQualifier(native.WarpScratch, native.WarpProbe, i.Models, i.StyleRuntimeSHA, BoundImportStyleScope{WorkICCSHA: i.WorkICCSHA, Masters: masters, Targets: targets})
	if err != nil {
		return nil, nil, err
	}
	archive := SQLImportOpaqueCatalog{DB: store.DB, Tenant: store.Tenant, Workflow: publisher.Scope.Workflow, Parsed: append([]byte(nil), parsed...), Bucket: store.Bucket}
	raster := NativeImportBackgroundQualifier{Native: NativeExportImageQualifier{Objects: publisher.SourceObjects, Scratch: native.WarpScratch, Probe: native.WarpProbe, RuntimeSHA: i.ImageRuntimeSHA}}
	backgrounds, err := (ImportBackgroundInstallation{Catalog: archive, Native: raster, Prepared: &publisher}).Build(ctx, publisher.Scope.Workflow, append([]byte(nil), parsed...), append([]ImportCorrectionBinding(nil), bindings...), sources)
	if err != nil {
		return nil, nil, err
	}
	return style, witnessedImportBackgroundRemapper{Base: backgrounds, Witnesses: &importBackgroundClosureWitnesses{}}, ctx.Err()
}
func (i ImportNativeStyleInstallation) owned() ImportNativeStyleInstallation {
	if i.Models != nil {
		models := map[string]string{}
		for key, value := range i.Models {
			models[key] = value
		}
		i.Models = models
	}
	return i
}
func (i ImportNativeStyleInstallation) BuildResolved(ctx context.Context, parsed []byte, bindings []ImportCorrectionBinding, sources ImportRecipeSourceQualifier, masters ImportMasterReader, native ImportResolverDependencies) (ImportResolvedQualifier, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if native.WarpScratch == nil || native.WarpProbe == nil || i.Models == nil || !validImportRefForSHA(i.StyleRuntimeSHA) {
		return nil, v2input.ErrInvalid
	}
	scope, err := NewBoundImportResolvedScope(parsed, append([]ImportCorrectionBinding(nil), bindings...), sources, masters, i.WorkICCSHA)
	if err != nil {
		return nil, err
	}
	return NewNativeImportResolvedQualifier(native.WarpScratch, native.WarpProbe, i.Models, i.StyleRuntimeSHA, scope)
}
