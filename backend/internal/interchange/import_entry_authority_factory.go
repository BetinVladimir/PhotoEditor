package interchange

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

// Operator dependencies are installed per workflow. All SQL/object readers and
// canonical source witnesses below are rebuilt for the concrete entry.
type SQLImportEntryRecipeAuthorityFactory struct {
	StyleInstallation       *ImportNativeStyleInstallation
	Registry                *ImportPreparedNodeRegistry
	Native                  ImportResolverDependencies
	Codecs                  ImportRecipeCodecDependencies
	Documents               ImportTypedDocumentValidator
	MaskSemantics           ImportMaskQualifier
	MaskInstallation        *ImportMaskPolicyInstallation
	PersonMaskQualification *ImportPersonMaskQualificationInstallation
	ModelLocks              []string
	Assets                  ImportClosureNodeResolver
	BackgroundMedia         string
	ImageRuntimeSHA         string
	DustSensors             ImportDustSensorMetadataReader
}

func (f SQLImportEntryRecipeAuthorityFactory) BuildImportEntryRecipeAuthority(ctx context.Context, scope ImportPreparedScope, parsedRaw []byte, archive ImportArchiveDocumentReader, masters ImportMasterReader, publisher PreparedImportArtifactPublisher, warps ImportDenseWarpRemapper) (ImportDependencyCodec, ImportPreparedRecipeQualifier, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	store, ok := publisher.Store.(SQLPreparedImportArtifactStore)
	objects, objectsOK := publisher.Objects.(ImportDocumentObjects)
	if !ok || !objectsOK || objects == nil || archive == nil || masters == nil || warps == nil || publisher.Scope != scope || scope.Warp != nil || scope.Background != nil || scope.Contract != "" || (f.MaskSemantics == nil && f.MaskInstallation == nil && f.PersonMaskQualification == nil) || (f.Assets == nil && f.ImageRuntimeSHA == "" && f.StyleInstallation == nil) {
		return nil, nil, v2input.ErrInvalid
	}
	parsed := append([]byte(nil), parsedRaw...)
	if !bytes.Equal(store.Parsed, parsed) {
		return nil, nil, v2input.ErrInvalid
	}
	store.Parsed = append([]byte(nil), parsed...)
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return nil, nil, err
	}
	probeScope := scope
	probeScope.Contract = "MaskRef"
	if _, _, err := store.receipt(probeScope, scope.Binding.Source, 2, "application/json"); err != nil {
		return nil, nil, err
	}
	if f.Native.WarpScratch == nil || f.Native.WarpProbe == nil || !validImportRefForSHA(f.Native.WarpRuntimeSHA) || f.Native.MaxFileBytes < 1 || f.Native.MaxFileBytes > 128<<30 {
		return nil, nil, v2input.ErrInvalid
	}
	var document importParsedDocument
	if err := decodeImportDocument(parsed, &document); err != nil {
		return nil, nil, err
	}
	sources, err := NewParsedImportRecipeSourceQualifier(parsed, archive, masters)
	if err != nil {
		return nil, nil, err
	}
	evidence, err := qualifyImportEntrySources(ctx, scope, document, f.Codecs.Bindings, sources)
	if err != nil {
		return nil, nil, err
	}
	targets, err := NewBoundImportBackgroundAuthority(f.Codecs.Bindings, sources)
	if err != nil {
		return nil, nil, err
	}
	if f.PersonMaskQualification == nil && f.Codecs.ConfirmedMasks != nil {
		return nil, nil, v2input.ErrInvalid
	}
	if f.PersonMaskQualification != nil {
		if f.MaskSemantics != nil || f.Codecs.ConfirmedMasks != nil {
			return nil, nil, v2input.ErrInvalid
		}
		currentConsent := SQLImportConfirmedMaskAuthority{Store: store}
		f.Codecs.ConfirmedMasks = currentConsent
		installation := ImportMaskPolicyInstallation{Models: map[string]ImportModelMaskPolicy{}}
		if f.MaskInstallation != nil {
			owned, err := f.MaskInstallation.owned()
			if err != nil {
				return nil, nil, err
			}
			installation = owned
		}
		if _, exists := installation.Models["person"]; exists {
			return nil, nil, v2input.ErrInvalid
		}
		person := f.PersonMaskQualification.owned()
		if person.Consent == nil {
			return nil, nil, v2input.ErrInvalid
		}
		person.Consent = importPersonMaskConsentChain{current: currentConsent, other: person.Consent}
		policy, lock, err := person.Build(ctx, store, objects)
		if err != nil {
			return nil, nil, err
		}
		installation.Models["person"] = ImportModelMaskPolicy{ModelLock: lock, Qualifier: policy}
		f.MaskInstallation = &installation
	}
	if f.MaskInstallation != nil {
		if f.MaskSemantics != nil {
			return nil, nil, v2input.ErrInvalid
		}
		installed, locks, err := f.MaskInstallation.Build(ctx, masters, targets)
		if err != nil {
			return nil, nil, err
		}
		f.MaskSemantics = installed
		seen := map[string]bool{}
		combined := []string{}
		for _, lock := range append(append([]string(nil), f.ModelLocks...), locks...) {
			if !validImportRefForSHA(lock) {
				return nil, nil, v2input.ErrInvalid
			}
			if !seen[lock] {
				seen[lock] = true
				combined = append(combined, lock)
			}
		}
		f.ModelLocks = combined
	}
	maskSemantics := f.MaskSemantics
	if f.MaskInstallation == nil {
		maskSemantics = BoundImportMaskSemantics{Masters: masters, Targets: targets, Policy: f.MaskSemantics}
	}
	// Final execution rechecks these sources against the complete frozen plan.
	witnesses := &ImportWorkingWitnesses{}
	validator := ImportScopedDocumentValidator{masters: evidence, witnesses: witnesses}
	jsonCatalog := SQLImportDocumentCatalog{DB: store.DB, Tenant: scope.Tenant, Workflow: scope.Workflow, Parsed: parsed, Bucket: store.Bucket}
	pixels := SQLImportPixelCatalog{DB: store.DB, Tenant: scope.Tenant, Workflow: scope.Workflow, Parsed: parsed, Bucket: store.Bucket, Witnesses: witnesses}
	warpCatalog := SQLImportWarpCatalog{DB: store.DB, Tenant: scope.Tenant, Workflow: scope.Workflow, Parsed: parsed, Bucket: store.Bucket, Witnesses: witnesses}
	native := NativeExportArrayQualifier{Objects: objects, Scratch: f.Native.WarpScratch, Probe: f.Native.WarpProbe, RuntimeSHA: f.Native.WarpRuntimeSHA, Geometry: ImportCanonicalWarpGeometry{Witnesses: witnesses, Catalog: warpCatalog}}
	working := ImportNodeResolverRouter{JSON: ImportJSONNodeResolver{Catalog: jsonCatalog, Objects: objects, Validator: validator}, Pixels: ImportCanonicalPixelNodeResolver{Catalog: pixels, Objects: objects, MaxBytes: f.Native.MaxFileBytes}, Warps: ImportCanonicalWarpNodeResolver{Catalog: warpCatalog, Objects: objects, MaxBytes: f.Native.MaxFileBytes, Qualifier: native}}
	if f.Documents == nil {
		f.Documents = ImportPreparedSchemaChildren{}
	}
	codecs := f.Codecs
	codecs.Sources = sources
	codecs.Warps = warps
	if f.DustSensors != nil {
		if codecs.DustSeries != nil {
			return nil, nil, v2input.ErrInvalid
		}
		codecs.DustSeries = MetadataImportDustSeriesAuthority{Bindings: append([]ImportCorrectionBinding(nil), codecs.Bindings...), Sensors: f.DustSensors}
	}
	if codecs.DefectCandidates != nil {
		var matched *ImportCorrectionBinding
		for i := range codecs.Bindings {
			if codecs.Bindings[i].Entry == scope.Entry {
				matched = &codecs.Bindings[i]
				break
			}
		}
		if matched == nil || matched.New != scope.Binding {
			return nil, nil, v2input.ErrInvalid
		}
		codecs.DefectCandidates = BoundImportDefectCandidatesQualifier{Entry: scope.Entry, Old: matched.Old, Target: matched.New, Parsed: parsed, Reader: archive, Sources: sources, Semantic: codecs.DefectCandidates}
	}
	if codecs.DustSeries != nil {
		var matched *ImportCorrectionBinding
		for i := range codecs.Bindings {
			if codecs.Bindings[i].Entry == scope.Entry {
				if matched != nil {
					return nil, nil, v2input.ErrInvalid
				}
				matched = &codecs.Bindings[i]
			}
		}
		if matched == nil || matched.New != scope.Binding {
			return nil, nil, v2input.ErrInvalid
		}
		codecs.DustSeries = BoundImportDustSeriesAuthority{Entry: scope.Entry, Old: matched.Old, Target: matched.New, Bindings: append([]ImportCorrectionBinding(nil), codecs.Bindings...), Parsed: parsed, Reader: archive, Sources: sources, Semantic: codecs.DustSeries}
	}
	// A newly authored tenant decision, read by the scheduler against the exact
	// prepared target, replaces every archived defect/dust review selection.
	currentReviews := SQLImportLiveReviewReader{DB: store.DB, Tenant: scope.Tenant, Workflow: scope.Workflow, Parsed: append([]byte(nil), parsed...), Witnesses: f.Native.Reviews}
	if codecs.DefectCandidates != nil {
		codecs.DefectReview = SQLImportDefectReviewIssuer{Entry: scope.Entry, Target: scope.Binding, Decisions: currentReviews, Additional: codecs.DefectReview}
	}
	if codecs.DustSeries != nil {
		codecs.DustReview = SQLImportDustReviewIssuer{Entry: scope.Entry, Target: scope.Binding, Decisions: currentReviews, Additional: codecs.DustReview}
	}
	if f.StyleInstallation != nil {
		installation := f.StyleInstallation.owned()
		style, backgrounds, err := installation.Build(ctx, parsed, codecs.Bindings, sources, masters, publisher, f.Native)
		if err != nil {
			return nil, nil, err
		}
		codecs.Style = style
		codecs.Backgrounds = backgrounds
		resolved, err := installation.BuildResolved(ctx, parsed, codecs.Bindings, sources, masters, f.Native)
		if err != nil {
			return nil, nil, err
		}
		codecs.Resolved = resolved
		f.ImageRuntimeSHA = installation.ImageRuntimeSHA
		codecs.OperationSchema = &ImportNativeOperationInstallation{Scratch: f.Native.WarpScratch, Probe: f.Native.WarpProbe, Models: installation.Models, RuntimeSHA: installation.StyleRuntimeSHA}
		archiveOverride, err := NewNativeImportOverrideArchiveQualifier(f.Native.WarpScratch, f.Native.WarpProbe, installation.Models, installation.StyleRuntimeSHA)
		if err != nil {
			return nil, nil, err
		}
		codecs.OverrideArchive = archiveOverride
		override, err := NewNativeImportOverrideContent(f.Native.WarpScratch, f.Native.WarpProbe, installation.Models, installation.StyleRuntimeSHA, codecs.OverrideContent)
		if err != nil {
			return nil, nil, err
		}
		codecs.OverrideContent = override
		witnessed, ok := backgrounds.(witnessedImportBackgroundRemapper)
		if !ok {
			return nil, nil, v2input.ErrInvalid
		}
		var matched *ImportCorrectionBinding
		for i := range codecs.Bindings {
			if codecs.Bindings[i].Entry == scope.Entry {
				if matched != nil || codecs.Bindings[i].New != scope.Binding {
					return nil, nil, v2input.ErrInvalid
				}
				matched = &codecs.Bindings[i]
			}
		}
		if matched == nil {
			return nil, nil, v2input.ErrInvalid
		}
		codecs.OverrideBackgrounds = BoundImportOverrideBackgroundRemapper{
			Source:     ImportOverrideStyleSource{Parsed: append([]byte(nil), parsed...), Entry: scope.Entry, Old: matched.Old, Reader: archive},
			OldVersion: matched.Old.AssetVersionID, Target: scope.Binding, Style: backgrounds,
		}
		f.Documents = importStyleBackgroundChildren{Other: f.Documents, Witnesses: witnessed.Witnesses}
	}
	assets := f.Assets
	if f.ImageRuntimeSHA != "" {
		if !validImportRefForSHA(f.ImageRuntimeSHA) {
			return nil, nil, v2input.ErrInvalid
		}
		assets = ImportPreparedBackgroundNodeResolver{Catalog: SQLPreparedImportBackgroundCatalog{Catalog: SQLPreparedImportCatalog{Store: store, Scope: scope}}, Native: NativeExportImageQualifier{Objects: objects, Scratch: f.Native.WarpScratch, Probe: f.Native.WarpProbe, RuntimeSHA: f.ImageRuntimeSHA}}
	}
	deps := PreparedImportResolverDependencies{Registry: f.Registry, Catalog: SQLPreparedImportCatalog{Store: store, Scope: scope, BackgroundMedia: f.BackgroundMedia}, Objects: objects, Masters: masters, Working: working, Assets: assets, Canonical: witnesses, Documents: f.Documents, MaskSemantics: maskSemantics, ModelLocks: append([]string(nil), f.ModelLocks...), Scratch: f.Native.WarpScratch, Probe: f.Native.WarpProbe, ArrayRuntimeSHA: f.Native.WarpRuntimeSHA, MaxBytes: f.Native.MaxFileBytes}
	if f.StyleInstallation != nil {
		installation := f.StyleInstallation.owned()
		composition, err := NewNativeExportStyleValidator(f.Native.WarpScratch, f.Native.WarpProbe, installation.Models, installation.StyleRuntimeSHA, exportCompositionStagingValidator{})
		if err != nil {
			return nil, nil, err
		}
		composition.contract = "RenderComposition"
		deps.Composition = composition
	}
	return NewSQLImportEntryRecipeAuthority(ctx, deps, codecs)
}

var _ ImportEntryRecipeAuthorityFactory = SQLImportEntryRecipeAuthorityFactory{}

// Freeze bindings from current SQL ingest authority, then re-read the complete
// source snapshot after archive/master IO. No configured binding list survives.
func (f SQLImportEntryRecipeAuthorityFactory) BindImportWorkflowRecipeAuthority(ctx context.Context, c workflow.InterchangeImportCandidate, parsedRaw []byte, deps ImportResolverDependencies) (ImportEntryRecipeAuthorityFactory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parsed := append([]byte(nil), parsedRaw...)
	if rawSHA(parsed) != c.SHA || !validImportRefForSHA(c.Snapshot) || deps.Objects == nil || deps.DB == nil || c.Bucket != deps.Bucket {
		return nil, v2input.ErrInvalid
	}
	ref := v2input.Ref{ArtifactID: c.Artifact, SHA256: c.SHA}
	sourceReader := SQLImportSourceReader{DB: deps.DB, Tenant: c.Tenant, Workflow: c.Workflow, Project: c.Project}
	sources, err := sourceReader.Read(ctx, ref, parsed)
	if err != nil {
		return nil, err
	}
	masters, err := NewSQLImportMasterReader(c.Tenant, c.Workflow, parsed, deps)
	if err != nil {
		return nil, err
	}
	archive := BoundedImportArchiveReader{Catalog: SQLImportOpaqueCatalog{DB: deps.DB, Tenant: c.Tenant, Workflow: c.Workflow, Parsed: parsed, Bucket: deps.Bucket}, Objects: deps.Objects}
	bindings, err := deriveImportCorrectionBindings(ctx, sources, parsed, archive, masters)
	if err != nil {
		return nil, err
	}
	current, err := sourceReader.Read(ctx, ref, parsed)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(sources.Body(), current.Body()) {
		return nil, artifactjson.ErrStale
	}
	if f.StyleInstallation != nil {
		owned := f.StyleInstallation.owned()
		f.StyleInstallation = &owned
	}
	if f.PersonMaskQualification != nil {
		owned := f.PersonMaskQualification.owned()
		f.PersonMaskQualification = &owned
	}
	if f.MaskInstallation != nil {
		owned, err := f.MaskInstallation.owned()
		if err != nil {
			return nil, err
		}
		f.MaskInstallation = &owned
	}
	if f.Codecs.UpscaleQualifications != nil {
		owned, err := f.Codecs.UpscaleQualifications.owned()
		if err != nil {
			return nil, err
		}
		f.Codecs.UpscaleQualifications = owned
	}
	if f.Codecs.UpscaleModelLocks != nil {
		locks, err := ownedImportUpscaleLocks(f.Codecs.UpscaleModelLocks)
		if err != nil {
			return nil, err
		}
		f.Codecs.UpscaleModelLocks = locks
	}
	if f.Codecs.StatsQualification != nil {
		owned := f.Codecs.StatsQualification.owned()
		f.Codecs.StatsQualification = &owned
	}
	if f.Codecs.DenoiseQualification != nil {
		owned := f.Codecs.DenoiseQualification.owned()
		f.Codecs.DenoiseQualification = &owned
	}
	if f.Codecs.LocalPortrait != nil {
		owned := *f.Codecs.LocalPortrait
		owned.Project = c.Project
		f.Codecs.LocalPortrait = &owned
	}
	f.Registry = &ImportPreparedNodeRegistry{}
	f.Native = deps
	f.Codecs.Bindings = append([]ImportCorrectionBinding(nil), bindings...)
	f.Codecs.Snapshot = c.Snapshot
	if f.DustSensors == nil && f.Codecs.DustSeries == nil {
		f.DustSensors = SQLSchedulerImportDustSensorReader{DB: deps.DB, Tenant: c.Tenant, Workflow: c.Workflow, Parsed: append([]byte(nil), parsed...)}
	}
	f.ModelLocks = append([]string(nil), f.ModelLocks...)
	return f, ctx.Err()
}

var _ ImportWorkflowRecipeAuthorityBinder = SQLImportEntryRecipeAuthorityFactory{}

func (f SQLImportEntryRecipeAuthorityFactory) BuildImportEntryBinaryRemapper(ctx context.Context, workflowID uuid.UUID, parsed []byte, reader ImportArchiveDocumentReader, sources ImportRecipeSourceQualifier, masters ImportMasterReader, catalog ImportDocumentCatalog, objects ImportDocumentObjects, publisher ImportBinaryPublisher) (ImportBinaryRemapper, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if workflowID == uuid.Nil || reader == nil || sources == nil || masters == nil || catalog == nil || objects == nil || publisher == nil {
		return nil, v2input.ErrInvalid
	}
	masks := ImportMaskBinaryRemapper{Workflow: workflowID, Catalog: catalog, Objects: objects, Masters: masters, Publisher: publisher}
	router := ImportBinaryCodecRouter{Masks: masks}
	if f.Codecs.DefectCandidates != nil {
		labelsPublisher, ok := publisher.(ImportDefectLabelOccurrencePublisher)
		if !ok {
			return nil, &ImportRecipeUnsupported{Reason: "defect_label_occurrence_publisher_not_installed"}
		}
		router.DefectLabels = ImportDefectLabelBinaryRemapper{Workflow: workflowID, Parsed: append([]byte(nil), parsed...), Reader: reader, Qualifier: f.Codecs.DefectCandidates, Sources: sources, Bindings: append([]ImportCorrectionBinding(nil), f.Codecs.Bindings...), Catalog: catalog, Objects: objects, Masters: masters, Publisher: labelsPublisher}
	}
	if f.StyleInstallation == nil {
		return router, nil
	}
	installed := f.StyleInstallation.owned()
	resolved, err := installed.BuildResolved(ctx, parsed, f.Codecs.Bindings, sources, masters, f.Native)
	if err != nil {
		return nil, err
	}
	lut := ImportLUTBinaryRemapper{Workflow: workflowID, Parsed: append([]byte(nil), parsed...), Reader: reader, Resolved: resolved, Catalog: catalog, Objects: objects, Scratch: f.Native.WarpScratch, Probe: f.Native.WarpProbe, RuntimeSHA: f.Native.WarpRuntimeSHA, Publisher: publisher}
	router.LUT = lut
	return router, ctx.Err()
}

var _ ImportEntryBinaryRemapperFactory = SQLImportEntryRecipeAuthorityFactory{}

func (f SQLImportEntryRecipeAuthorityFactory) PreparedImportRegistry() *ImportPreparedNodeRegistry {
	return f.Registry
}
