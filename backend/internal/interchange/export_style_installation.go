package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

type ExportScopedDocumentValidatorFactory interface {
	BuildScopedExportDocumentValidator(context.Context, CreateIntent, FrozenExportDocuments, ExportDocumentCatalog, ImportDocumentObjects) (ExportTypedDocumentValidator, error)
}
type ExportStyleValidatorInstallation struct {
	Base              ExportDocumentValidatorInstallation
	Scratch           *ExportArchiveScratch
	Probe             ExportNativeProbe
	Models            map[string]string
	RuntimeSHA        string
	PersonPolicy      ExportStatsPersonPolicyFactory
	PromptPolicy      ExportPromptMaskPolicyFactory
	Body              ExportBodyOriginValidatorFactory
	Backgrounds       ExportStyleBackgroundAuthority
	BackgroundFactory ExportStyleBackgroundAuthorityFactory
	ImageProbe        ExportNativeProbe
	ImageRuntimeSHA   string
}

func (s ExportStyleValidatorInstallation) BuildScopedExportBackground(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, catalog ExportDocumentCatalog, objects ImportDocumentObjects) (ExportStyleBackgroundAuthority, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Backgrounds != nil && s.BackgroundFactory != nil {
		return nil, v2input.ErrInvalid
	}
	if s.BackgroundFactory == nil {
		return s.Backgrounds, nil
	}
	if s.Scratch == nil || s.ImageProbe == nil || !validImportRefForSHA(s.ImageRuntimeSHA) {
		return nil, v2input.ErrInvalid
	}
	background, err := s.BackgroundFactory.BuildExportStyleBackgroundAuthority(ctx, intent, frozen, catalog, objects, NativeExportImageQualifier{Objects: objects, Scratch: s.Scratch, Probe: s.ImageProbe, RuntimeSHA: s.ImageRuntimeSHA})
	if err != nil {
		return nil, err
	}
	if background == nil {
		return nil, v2input.ErrInvalid
	}
	return background, ctx.Err()
}

// Cannot build without a catalogue tied to this exact request. SQL installer
// supplies it through the scoped factory interface; there is no global reader.
func (s ExportStyleValidatorInstallation) BuildExportDocumentValidator(context.Context, CreateIntent, FrozenExportDocuments) (ExportTypedDocumentValidator, error) {
	return nil, v2input.ErrInvalid
}
func (s ExportStyleValidatorInstallation) BuildScopedExportDocumentValidator(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, catalog ExportDocumentCatalog, objects ImportDocumentObjects) (ExportTypedDocumentValidator, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if catalog == nil || objects == nil || s.Base.Other == nil || s.Body == nil || s.BackgroundFactory != nil && s.Backgrounds == nil {
		return nil, v2input.ErrInvalid
	}
	// This parent reader validates only FrozenRenderRecipe and returns its typed
	// edges without visiting them. It cannot recurse through StyleV2 validation.
	parentValidator, err := NewExportFrozenDocumentValidator(ctx, intent, frozen, s.Base.ICCSHA, s.Base.ModelSHA, nil)
	if err != nil {
		return nil, err
	}
	parentReader := ExportJSONNodeResolver{Catalog: catalog, Objects: objects, Validator: parentValidator}
	authority, err := NewExportFrozenStyleScope(ctx, intent, frozen, parentReader, s.Base.Other)
	if err != nil {
		return nil, err
	}
	authority.backgrounds = s.Backgrounds
	native, err := NewNativeExportStyleValidator(s.Scratch, s.Probe, s.Models, s.RuntimeSHA, authority)
	if err != nil {
		return nil, err
	}
	producerSource, ok := catalog.(ExportCorrectionProducerSource)
	if !ok || (s.Base.Semantics == nil) == (s.Base.SemanticsFactory == nil) {
		return nil, v2input.ErrInvalid
	}
	baseSemantics := s.Base.Semantics
	if s.Base.SemanticsFactory != nil {
		baseSemantics, err = s.Base.SemanticsFactory.BuildExportDependencySemantics(ctx, catalog)
		if err != nil || baseSemantics == nil {
			return nil, v2input.ErrInvalid
		}
	}
	learningAuthority, ok := catalog.(ExportLearningOccurrenceAuthority)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	learning, err := NewExportFrozenLearningSemantics(ctx, intent, frozen, parentReader, learningAuthority, baseSemantics)
	if err != nil {
		return nil, err
	}
	producer := ExportCorrectionProducerSemantics{Source: producerSource, Objects: objects, Fallback: learning}
	corrections, err := NewExportFrozenSourceScope(ctx, intent, frozen, parentReader, producer)
	if err != nil {
		return nil, err
	}
	sourceValidator, err := NewExportWorkingDocumentValidator(ctx, intent, frozen, s.Base.ICCSHA, nil)
	if err != nil {
		return nil, err
	}
	sourceReader := ExportJSONNodeResolver{Catalog: catalog, Objects: objects, Validator: sourceValidator}
	bindings := make([]v2input.SessionVersionBinding, 0, len(corrections.recipes))
	for _, recipe := range corrections.recipes {
		bindings = append(bindings, recipe.binding)
	}
	warpAuthority, ok := catalog.(ExportDenseWarpOccurrenceAuthority)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	resolvedScope := ExportResolvedSourceScope{Warps: warpAuthority, Sources: sourceReader, Bindings: bindings, Other: s.Base.Other}
	authority.policy = resolvedScope
	overrides := &ExportFrozenOverrideScope{warps: warpAuthority, parents: parentReader, sources: sourceReader, recipes: append([]exportRecipeBinding(nil), corrections.recipes...), policy: native}
	overrideSchema, err := NewNativeExportOverrideValidator(s.Scratch, s.Probe, s.Models, s.RuntimeSHA, overrides)
	if err != nil {
		return nil, err
	}
	statsOrigin, ok := catalog.(ExportStatsOriginSource)
	if !ok || s.PersonPolicy == nil || s.PromptPolicy == nil {
		return nil, v2input.ErrInvalid
	}
	personOrigin, ok := catalog.(ExportStatsPersonOriginSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	personDocuments, ok := catalog.(ExportStatsPersonDocumentCatalog)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	personPolicy, err := s.PersonPolicy.BuildExportStatsPersonPolicy(ctx, intent, frozen, catalog, objects)
	if err != nil {
		return nil, err
	}
	if personPolicy == nil {
		return nil, v2input.ErrInvalid
	}
	promptPolicy, err := s.PromptPolicy.BuildExportPromptMaskPolicy(ctx, intent, frozen, catalog, objects)
	if err != nil {
		return nil, err
	}
	if promptPolicy == nil {
		return nil, v2input.ErrInvalid
	}
	personSource := QualifiedExportStatsPersonSource{Origin: personOrigin, Documents: ExportStatsPersonDocumentReader{Catalog: personDocuments, Objects: objects}, Policy: personPolicy}
	personNative := NativeExportStatsPersonQualifier{Source: personSource, Objects: objects, Scratch: s.Scratch, Probe: s.Probe, RuntimeSHA: s.RuntimeSHA}
	statsAuthority := ExportStatsOriginQualifier{Origin: statsOrigin, Objects: objects, Images: sourceReader, Person: personNative}
	semantics := ExportStatsSemantics{Authority: statsAuthority, Other: corrections}
	base := s.Base
	base.Other = overrideSchema
	base.Semantics = semantics
	base.SemanticsFactory = nil
	complete, err := base.BuildExportDocumentValidator(ctx, intent, frozen)
	if err != nil {
		return nil, err
	}
	memberAuthority, ok := catalog.(ExportCorrectionMemberAuthority)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	working, ok := complete.(ExportWorkingDocumentValidator)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	resolvedOrigin, ok := catalog.(ExportResolvedOriginSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	paramsOperation, ok := catalog.(ExportParamsOperationSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	seriesMetadata, ok := catalog.(ExportSeriesAuthority)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	seriesSource, ok := catalog.(ExportSeriesOriginSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	seriesImages, ok := catalog.(ExportSeriesStatImageAuthority)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	measureSource, ok := catalog.(ExportSeriesMeasureOriginSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	measureInputs, ok := catalog.(ExportSeriesMeasureInputAuthority)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	maskOrigin, ok := catalog.(ExportSeriesMaskOriginSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	maskPolicies := ExportSeriesMaskPolicyRouter{Person: ExportSeriesPersonMaskPolicy{Person: personPolicy}, Prompt: promptPolicy}
	bodyDocuments := ExportJSONNodeResolver{Catalog: catalog, Objects: objects, Validator: exportCompositionStagingValidator{}}
	measureMasks := QualifiedExportSeriesMeasureMaskSource{Reader: ExportSeriesMaskOriginReader{Source: maskOrigin, Objects: objects}, Policy: maskPolicies, Scratch: s.Scratch, Probe: s.Probe, RuntimeSHA: s.RuntimeSHA}
	physicalMeasureInputs := NativeExportSeriesMeasureMasks{Inputs: measureInputs, Source: measureMasks, Objects: objects, Scratch: s.Scratch, Probe: s.Probe, RuntimeSHA: s.RuntimeSHA}
	measureAuthority, err := NewNativeExportSeriesMeasureQualifier(ExportSeriesMeasureParamsReader{Source: measureSource, Objects: objects}, physicalMeasureInputs, s.Scratch, s.Probe, s.Models, s.RuntimeSHA)
	if err != nil {
		return nil, err
	}
	seriesAuthority, err := NewNativeExportSeriesOriginQualifier(ExportSeriesOriginReader{Source: seriesSource, Objects: objects}, seriesMetadata, ExportSeriesStatInputs{Authority: seriesImages, Images: sourceReader, Warps: warpAuthority, Measure: measureAuthority}, s.Scratch, s.Probe, s.Models, s.RuntimeSHA)
	if err != nil {
		return nil, err
	}
	// Auxiliary correction/descriptor reads must use the same full resolved
	// schema/original Params proof as the outer graph, without recursing into it.
	planSchema, err := NewNativeExportResolvedValidator(s.Scratch, s.Probe, s.Models, s.RuntimeSHA, ExportDependencyDocumentValidator{Semantics: semantics, Other: resolvedScope})
	if err != nil {
		return nil, err
	}
	planOrigin, err := NewExportResolvedOriginValidator(planSchema, resolvedOrigin, objects, s.Scratch, s.Probe, s.Models, s.RuntimeSHA)
	if err != nil {
		return nil, err
	}
	planParams, err := NewExportOperationParamsValidator(planOrigin, paramsOperation, s.Scratch, s.Probe, s.Models, s.RuntimeSHA)
	if err != nil {
		return nil, err
	}
	planSeries, err := NewNativeExportSeriesValidator(s.Scratch, s.Probe, s.Models, s.RuntimeSHA, ExportSeriesProducerValidator{Authority: seriesAuthority, Other: planParams})
	if err != nil {
		return nil, err
	}
	planBody, err := s.Body.BuildExportBodyOriginValidator(ctx, intent, frozen, catalog, objects, bodyDocuments, sourceReader, warpAuthority, maskPolicies, planSeries)
	if err != nil {
		return nil, err
	}
	if planBody == nil {
		return nil, v2input.ErrInvalid
	}
	plans := ExportJSONNodeResolver{Catalog: catalog, Objects: objects, Validator: planBody}
	frameAuthority, ok := catalog.(ExportRenderFrameAuthority)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	if _, ok := catalog.(ExportRenderFrameArrayAuthority); !ok {
		return nil, v2input.ErrInvalid
	}
	descriptorWorking := ExportDescriptorWorkingAuthority{Parents: parentReader, Descriptors: plans, Recipes: append([]exportRecipeBinding(nil), corrections.recipes...), Authority: frameAuthority}
	working.Additional = ExportCorrectionWorkingAuthority{Parents: parentReader, Plans: plans, Recipes: append([]exportRecipeBinding(nil), corrections.recipes...), Authority: memberAuthority, Fallback: descriptorWorking}
	complete = ExportDescriptorFaceValidator{Frames: descriptorWorking, Other: working}
	localImages := ExportJSONNodeResolver{Catalog: catalog, Objects: objects, Validator: working}
	complete = ExportImageLocalSourceScope{Images: localImages, Resolved: plans, Warps: warpAuthority, Other: complete}
	// Resolved documents also occur below repair/detail Params, not just the
	// frozen style root. Probe every occurrence before the scoped policy chain.
	complete, err = NewNativeExportResolvedValidator(s.Scratch, s.Probe, s.Models, s.RuntimeSHA, complete)
	if err != nil {
		return nil, err
	}
	complete, err = NewExportResolvedOriginValidator(complete, resolvedOrigin, objects, s.Scratch, s.Probe, s.Models, s.RuntimeSHA)
	if err != nil {
		return nil, err
	}
	complete, err = NewExportOperationParamsValidator(complete, paramsOperation, s.Scratch, s.Probe, s.Models, s.RuntimeSHA)
	if err != nil {
		return nil, err
	}
	complete, err = NewNativeExportSeriesValidator(s.Scratch, s.Probe, s.Models, s.RuntimeSHA, ExportSeriesProducerValidator{Authority: seriesAuthority, Other: complete})
	if err != nil {
		return nil, err
	}
	complete, err = s.Body.BuildExportBodyOriginValidator(ctx, intent, frozen, catalog, objects, bodyDocuments, sourceReader, warpAuthority, maskPolicies, complete)
	if err != nil {
		return nil, err
	}
	if complete == nil {
		return nil, v2input.ErrInvalid
	}
	staging := ExportJSONNodeResolver{Catalog: catalog, Objects: objects, Validator: exportCompositionStagingValidator{}}
	return newExportFrozenCompositionValidator(complete, staging, s.Scratch, s.Probe, s.Models, s.RuntimeSHA)
}

var _ ExportDocumentValidatorFactory = ExportStyleValidatorInstallation{}
var _ ExportScopedDocumentValidatorFactory = ExportStyleValidatorInstallation{}
