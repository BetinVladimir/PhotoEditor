package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

type SQLExportAlignmentInstallation struct {
	Policy                                                            ExportAlignmentPolicyDeployment
	Scratch                                                           *ExportArchiveScratch
	StyleProbe, MaskProbe                                             ExportNativeProbe
	RegistrationRuntimeSHA, MaskOriginRuntimeSHA, MaskArrayRuntimeSHA string
}

// Images/warps/mask policy must be built for this same frozen export scope by
// the trusted composition root; no request-provided policy or resolver is used.
func (s SQLExportAlignmentInstallation) BuildExportAlignmentQualifier(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, catalog ExportDocumentCatalog, objects ImportDocumentObjects, images ExportClosureNodeResolver, warps ExportDenseWarpOccurrenceAuthority, masks ExportSeriesMaskPolicyAuthority) (*NativeExportBodyFitAlignmentQualifier, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scheduler, ok := catalog.(SQLSchedulerExportInputCatalog); ok {
		if objects == nil || images == nil || warps == nil || masks == nil ||
			s.Scratch == nil || s.StyleProbe == nil || s.MaskProbe == nil ||
			!validImportRef(s.Policy.Report) ||
			!validExportAlignmentProfile(s.Policy.Expected) {
			return nil, v2input.ErrInvalid
		}
		for _, sha := range []string{s.RegistrationRuntimeSHA, s.MaskOriginRuntimeSHA, s.MaskArrayRuntimeSHA} {
			if !validImportRef(v2input.Ref{ArtifactID: s.Policy.Report.ArtifactID, SHA256: sha}) {
				return nil, v2input.ErrInvalid
			}
		}
		if _, err := schedulerExportPolicyBindings(ctx, intent, frozen, scheduler); err != nil {
			return nil, err
		}
		if err := SchedulerExportAdoptionSQLReady(ctx, scheduler.Claims.DB); err != nil {
			return nil, err
		}
		qualified := QualifiedExportAlignmentBackgroundSource{
			Reader: ExportAlignmentBackgroundOriginReader{Source: scheduler, Objects: objects},
			Policy: masks, Scratch: s.Scratch, Probe: s.StyleProbe,
			RuntimeSHA: s.MaskOriginRuntimeSHA,
		}
		background := NativeExportAlignmentBackgroundQualifier{
			Source: qualified, Objects: objects, Scratch: s.Scratch,
			Probe: s.MaskProbe, RuntimeSHA: s.MaskArrayRuntimeSHA,
		}
		inputs := BoundExportAlignmentOriginalInputs{
			Source: scheduler, Images: images, Warps: warps, Background: background,
		}
		policy, err := NewExportAlignmentPolicy(scheduler, objects, inputs,
			s.Policy.Report, s.Policy.Expected)
		if err != nil {
			return nil, err
		}
		return NewNativeExportBodyFitAlignmentQualifier(
			ExportBodyFitAlignmentParamsReader{Source: scheduler, Objects: objects},
			policy, s.Scratch, s.StyleProbe, map[string]string{},
			s.RegistrationRuntimeSHA)
	}
	scoped, err := exportPolicyCatalogConfig(catalog)
	if err != nil || scoped.DB == nil || objects == nil || images == nil || warps == nil || masks == nil || s.Scratch == nil || s.StyleProbe == nil || s.MaskProbe == nil {
		return nil, v2input.ErrInvalid
	}
	origin, ok := catalog.(ExportBodyFitAlignmentOriginSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	backgroundOrigin, ok := catalog.(ExportAlignmentBackgroundOriginSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	policyReader, ok := catalog.(ExportAlignmentQualificationReader)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	actual, err := FreezeCreateIntent(scoped.Tenant, scoped.Who, scoped.Request, scoped.Key)
	if err != nil {
		return nil, err
	}
	if actual.RequestSHA != intent.RequestSHA || actual.Identity != intent.Identity {
		return nil, v2input.ErrInvalid
	}
	checked, _, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if err != nil {
		return nil, err
	}
	if string(scoped.Frozen.Body()) != string(checked.Body()) {
		return nil, v2input.ErrInvalid
	}
	if !validImportRef(s.Policy.Report) || !validExportAlignmentProfile(s.Policy.Expected) {
		return nil, v2input.ErrInvalid
	}
	for _, sha := range []string{s.RegistrationRuntimeSHA, s.MaskOriginRuntimeSHA, s.MaskArrayRuntimeSHA} {
		if !validImportRef(v2input.Ref{ArtifactID: s.Policy.Report.ArtifactID, SHA256: sha}) {
			return nil, v2input.ErrInvalid
		}
	}
	qualified := QualifiedExportAlignmentBackgroundSource{Reader: ExportAlignmentBackgroundOriginReader{Source: backgroundOrigin, Objects: objects}, Policy: masks, Scratch: s.Scratch, Probe: s.StyleProbe, RuntimeSHA: s.MaskOriginRuntimeSHA}
	background := NativeExportAlignmentBackgroundQualifier{Source: qualified, Objects: objects, Scratch: s.Scratch, Probe: s.MaskProbe, RuntimeSHA: s.MaskArrayRuntimeSHA}
	inputs := BoundExportAlignmentOriginalInputs{Source: origin, Images: images, Warps: warps, Background: background}
	policy, err := NewExportAlignmentPolicy(policyReader, objects, inputs, s.Policy.Report, s.Policy.Expected)
	if err != nil {
		return nil, err
	}
	qualifier, err := NewNativeExportBodyFitAlignmentQualifier(ExportBodyFitAlignmentParamsReader{Source: origin, Objects: objects}, policy, s.Scratch, s.StyleProbe, map[string]string{}, s.RegistrationRuntimeSHA)
	if err != nil {
		return nil, err
	}
	if err := validateExportAuthorityFunctions(ctx, scoped.DB, []string{"photo.v30_registration_qualification_object(uuid,uuid,uuid,uuid,jsonb)"}); err != nil {
		return nil, err
	}
	if err := validateExportMaskAuthorityInstallation(ctx, scoped.DB); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return qualifier, nil
}
