package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

// Infrastructure ports are entry-scoped SQL readers. Semantic ports are the
// installed operator authorities, never document-supplied capability claims.
type ImportRecipeCodecDependencies struct {
	// Optional native V28 installation is rebuilt with the actual entry ports.
	LocalPortrait         *ImportLocalPortraitInstallation
	OperationSchema       *ImportNativeOperationInstallation
	StatsQualification    *ImportStatsQualificationInstallation
	DenoiseQualification  *ImportDenoiseQualificationInstallation
	DenoiseModelLock      string
	UpscaleModelLocks     map[int]string
	UpscaleQualifications ImportUpscaleQualifications
	Snapshot              string
	Bindings              []ImportCorrectionBinding
	Sources               ImportRecipeSourceQualifier
	Masters               ImportMasterReader
	Documents             ImportPreparedDocumentReader
	Style                 ImportStyleQualifier
	Resolved              ImportResolvedQualifier
	Backgrounds           ImportStyleBackgroundRemapper
	OverrideBackgrounds   ImportOverrideBackgroundRemapper
	OverrideArchive       ImportOverrideArchiveQualifier
	Masks                 ImportMaskQualifier
	ConfirmedMasks        ImportConfirmedMaskAuthority
	ConfirmedRegions      ImportPortraitRegionConfirmation
	DefectReview          ImportDefectReviewAuthority
	DefectCandidates      ImportDefectCandidatesQualifier
	DustSeries            ImportDustSeriesAuthority
	DustReview            ImportDustReviewAuthority
	Warps                 ImportDenseWarpRemapper
	OverrideContent       ImportOverrideContentQualifier
	// Exact operator/version admission is mandatory in the production graph.
	Operations *ImportOperationRouter
	// Complete operation-specific Params support beyond hair/clothing. Unknown
	// operations must be rejected by this authority, never accepted generically.
	Params ImportDependencyCodec
}

// Constructs the actual six-contract codec graph with shared mask authority
// for direct masks, inline overrides and layer masks. It does not publish or
// activate anything; final prepared closure qualification remains mandatory.
func NewImportRecipeCodecGraph(ctx context.Context, d ImportRecipeCodecDependencies) (*ImportRecipeCodecRouter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.Operations == nil || len(d.Operations.operations) == 0 {
		return nil, v2input.ErrInvalid
	}
	if d.Sources == nil || d.Masters == nil || d.Documents == nil || d.Style == nil || d.Backgrounds == nil || d.Masks == nil || d.Warps == nil || d.OverrideContent == nil || d.Operations == nil || d.Params == nil {
		return nil, v2input.ErrInvalid
	}
	corrections, err := NewImportCorrectionPlanCodec(d.Snapshot, append([]ImportCorrectionBinding(nil), d.Bindings...), d.Sources, nil)
	if err != nil {
		return nil, err
	}
	masks := ImportMaskCodec{Qualifier: d.Masks, Warps: d.Warps}
	overrides := ImportOverrideCodec{Masters: d.Masters, Masks: masks, Confirmed: d.ConfirmedMasks, Archive: d.OverrideArchive, Backgrounds: d.OverrideBackgrounds, Qualifier: ImportPreparedOverrideQualifier{Masters: d.Masters, Masks: d.Masks, Content: d.OverrideContent, Confirmed: d.ConfirmedMasks}}
	layers := ImportLayerStackCodec{Qualifier: ImportPreparedLayerQualifier{Documents: d.Documents, Masks: d.Masks, Operations: d.Operations}}
	params := ImportLocalPortraitParamsCodec{Qualifier: d.Operations, Fallback: ImportDenoiseParamsCodec{Qualifier: d.Operations, Fallback: ImportUpscaleParamsCodec{Qualifier: d.Operations, Fallback: ImportStatsParamsCodec{Qualifier: d.Operations, Masks: masks, Fallback: ImportPortraitDetailParamsCodec{Qualifier: d.Operations, Masks: masks, Confirmed: d.ConfirmedRegions, Fallback: ImportDefectRepairParamsCodec{Qualifier: d.Operations, Masks: masks, Fallback: ImportDustRepairParamsCodec{Qualifier: d.Operations, Authority: d.DustReview, Fallback: d.Params}}}}}}}
	router, err := NewImportRecipeCodecRouter(ImportStyleCodec{Qualifier: d.Style, Backgrounds: d.Backgrounds}, corrections, overrides, layers, masks, params)
	if err != nil {
		return nil, err
	}
	if d.Resolved != nil {
		router, err = router.WithResolved(ImportResolvedCodec{Qualifier: d.Resolved, Override: overrides})
		if err != nil {
			return nil, err
		}
	}
	router, err = router.WithStats(ImportStatsDocumentCodec{})
	if err != nil {
		return nil, err
	}
	if d.DefectReview != nil {
		router, err = router.WithDefectReview(ImportDefectReviewCodec{Authority: d.DefectReview})
		if err != nil {
			return nil, err
		}
	}
	if d.DefectCandidates != nil {
		router, err = router.WithDefectCandidates(ImportDefectCandidatesCodec{Masters: d.Masters, Qualifier: d.DefectCandidates})
		if err != nil {
			return nil, err
		}
	}
	if d.DustSeries != nil {
		router, err = router.WithDustCandidates(ImportDustCandidatesCodec{Authority: d.DustSeries})
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return router, nil
}

// Joins the codec graph and the final physical closure verifier over the same
// entry-scoped catalogue, immutable object store, masters and mask authority.
// Caller-supplied document/master readers in codec options are replaced with
// these scoped infrastructure readers before any callback is invoked.
func NewSQLImportEntryRecipeAuthority(ctx context.Context, resolverDeps PreparedImportResolverDependencies, codecDeps ImportRecipeCodecDependencies) (ImportDependencyCodec, ImportPreparedRecipeQualifier, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if resolverDeps.Objects == nil || resolverDeps.Masters == nil || resolverDeps.MaskSemantics == nil {
		return nil, nil, v2input.ErrInvalid
	}
	catalog := resolverDeps.Catalog
	catalog.Store.Parsed = append([]byte(nil), catalog.Store.Parsed...)
	scope := catalog.Scope
	scope.Contract = "MaskRef"
	scope.Warp = nil
	scope.Background = nil
	if _, _, err := catalog.Store.receipt(scope, scope.Binding.Source, 2, "application/json"); err != nil {
		return nil, nil, err
	}
	physical, err := NewImportPhysicalMaskQualifier(resolverDeps.Masters, catalog, resolverDeps.Objects, append([]string(nil), resolverDeps.ModelLocks...), resolverDeps.MaxBytes)
	if err != nil {
		return nil, nil, err
	}
	codecDeps.Masters = resolverDeps.Masters
	codecDeps.Documents = BoundedPreparedImportReader{Catalog: catalog, Objects: resolverDeps.Objects}
	codecDeps.Masks = importPreparedMaskQualification{Physical: physical, Semantic: resolverDeps.MaskSemantics}
	if codecDeps.DefectReview != nil {
		if codecDeps.DefectCandidates == nil {
			return nil, nil, v2input.ErrInvalid
		}
		codecDeps.DefectReview = BoundImportDefectReviewAuthority{Documents: codecDeps.Documents, Candidates: codecDeps.DefectCandidates, Current: codecDeps.DefectReview, Target: scope.Binding}
	}
	if codecDeps.DustReview != nil {
		if codecDeps.DustSeries == nil {
			return nil, nil, v2input.ErrInvalid
		}
		codecDeps.DustReview = BoundImportDustReviewAuthority{Documents: codecDeps.Documents, Series: codecDeps.DustSeries, Current: codecDeps.DustReview, Target: scope.Binding}
	}
	if codecDeps.LocalPortrait != nil {
		installed, err := codecDeps.LocalPortrait.Build(ctx, codecDeps.Operations, codecDeps.Masters, codecDeps.Documents, codecDeps.Masks, SQLImportLocalRuntimeQualification{DB: catalog.Store.DB, Tenant: scope.Tenant, Project: codecDeps.LocalPortrait.Project})
		if err != nil {
			return nil, nil, err
		}
		codecDeps.Operations = installed
	}
	if codecDeps.DenoiseQualification != nil {
		policy, lock, err := codecDeps.DenoiseQualification.Build(ctx, catalog.Store, resolverDeps.Objects)
		if err != nil {
			return nil, nil, err
		}
		operations := map[ImportOperationVersion]ImportLayerOperationQualifier{}
		if codecDeps.Operations != nil {
			for key, value := range codecDeps.Operations.operations {
				operations[key] = value
			}
		}
		key := ImportOperationVersion{"image.denoise", "1"}
		if _, exists := operations[key]; exists {
			return nil, nil, v2input.ErrInvalid
		}
		operations[key] = policy
		installed, err := NewImportOperationRouter(ctx, operations)
		if err != nil {
			return nil, nil, err
		}
		codecDeps.Operations = installed
		codecDeps.DenoiseModelLock = lock
	}
	if codecDeps.Operations != nil {
		installed, err := bindImportDenoiseOperation(ctx, codecDeps)
		if err != nil {
			return nil, nil, err
		}
		codecDeps.Operations = installed
	}
	if codecDeps.UpscaleQualifications != nil {
		policy, locks, err := codecDeps.UpscaleQualifications.Build(ctx, catalog.Store, resolverDeps.Objects)
		if err != nil {
			return nil, nil, err
		}
		operations := map[ImportOperationVersion]ImportLayerOperationQualifier{}
		if codecDeps.Operations != nil {
			for key, value := range codecDeps.Operations.operations {
				operations[key] = value
			}
		}
		key := ImportOperationVersion{"image.upscale", "1"}
		if _, exists := operations[key]; exists {
			return nil, nil, v2input.ErrInvalid
		}
		operations[key] = policy
		for scale, lock := range codecDeps.UpscaleModelLocks {
			if locks[scale] != lock {
				return nil, nil, v2input.ErrInvalid
			}
		}
		installed, err := NewImportOperationRouter(ctx, operations)
		if err != nil {
			return nil, nil, err
		}
		codecDeps.Operations = installed
		codecDeps.UpscaleModelLocks = locks
	}
	if codecDeps.Operations != nil {
		installed, err := bindImportUpscaleOperation(ctx, codecDeps)
		if err != nil {
			return nil, nil, err
		}
		codecDeps.Operations = installed
	}
	if codecDeps.StatsQualification != nil {
		policy, _, err := codecDeps.StatsQualification.Build(ctx, catalog.Store, resolverDeps.Objects)
		if err != nil {
			return nil, nil, err
		}
		operations := map[ImportOperationVersion]ImportLayerOperationQualifier{}
		if codecDeps.Operations != nil {
			for key, value := range codecDeps.Operations.operations {
				operations[key] = value
			}
		}
		key := ImportOperationVersion{"color.stats", "1"}
		if _, exists := operations[key]; exists {
			return nil, nil, v2input.ErrInvalid
		}
		operations[key] = policy
		installed, err := NewImportOperationRouter(ctx, operations)
		if err != nil {
			return nil, nil, err
		}
		codecDeps.Operations = installed
	}
	if codecDeps.Operations != nil {
		installed, err := bindImportStatsOperation(ctx, codecDeps)
		if err != nil {
			return nil, nil, err
		}
		codecDeps.Operations = installed
	}
	if codecDeps.OperationSchema != nil {
		installed, err := codecDeps.OperationSchema.Build(ctx, codecDeps.Operations)
		if err != nil {
			return nil, nil, err
		}
		codecDeps.Operations = installed
	}
	codecs, err := NewImportRecipeCodecGraph(ctx, codecDeps)
	if err != nil {
		return nil, nil, err
	}
	resolverDeps.Codec = codecs
	resolver, err := NewSQLPreparedImportResolver(ctx, resolverDeps)
	if err != nil {
		return nil, nil, err
	}
	if resolverDeps.Registry != nil {
		return codecs, registeredPreparedImportRecipeQualifier{Scope: scope, Resolver: resolver, Catalog: catalog, Objects: resolverDeps.Objects, Registry: resolverDeps.Registry}, ctx.Err()
	}
	return codecs, BoundImportPreparedRecipeQualifier{Scope: scope, Resolver: resolver}, ctx.Err()
}
