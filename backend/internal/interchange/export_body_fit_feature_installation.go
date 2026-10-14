package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

type SQLExportBodyFitFeatureInstallation struct {
	Configs    []ExportBodyDependencyPolicyConfig
	Detector   SQLExportDetectorInstallation
	Scratch    *ExportArchiveScratch
	Probe      ExportNativeProbe
	RuntimeSHA string
}

func (s SQLExportBodyFitFeatureInstallation) BuildExportBodyFitFeatureQualifier(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, catalog ExportDocumentCatalog, objects ImportDocumentObjects, documents, images ExportClosureNodeResolver, warps ExportDenseWarpOccurrenceAuthority) (*NativeExportBodyFitFeatureQualifier, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scheduler, ok := catalog.(SQLSchedulerExportInputCatalog); ok {
		if objects == nil || documents == nil || images == nil || warps == nil ||
			s.Scratch == nil || s.Probe == nil {
			return nil, v2input.ErrInvalid
		}
		if _, err := schedulerExportPolicyBindings(ctx, intent, frozen, scheduler); err != nil {
			return nil, err
		}
		if err := SchedulerExportAdoptionSQLReady(ctx, scheduler.Claims.DB); err != nil {
			return nil, err
		}
		detector, err := s.Detector.BuildExportDetectorQualifier(ctx, intent, frozen,
			scheduler, objects, documents, images, warps)
		if err != nil {
			return nil, err
		}
		inputs := BoundExportBodyFitFeatureOriginal{
			Source: scheduler, Images: images, Warps: warps, Faces: detector,
		}
		policy, err := NewExportBodyFitFeaturePolicy(scheduler, objects, inputs, s.Configs)
		if err != nil {
			return nil, err
		}
		models := map[string]string{}
		for _, config := range s.Configs {
			switch config.Expected.Operation {
			case "pose.detect":
				models["keypointrcnn_resnet50_fpn"] = config.Expected.ModelLockSHA
			case "face.landmarks":
				models["fan2d"] = config.Expected.ModelLockSHA
			default:
				return nil, v2input.ErrInvalid
			}
		}
		return NewNativeExportBodyFitFeatureQualifier(
			ExportBodyFitFeatureParamsReader{Source: scheduler, Objects: objects},
			policy, s.Scratch, s.Probe, models, s.RuntimeSHA)
	}
	scoped, err := exportPolicyCatalogConfig(catalog)
	if err != nil || scoped.DB == nil || objects == nil || documents == nil || images == nil || warps == nil || s.Scratch == nil || s.Probe == nil {
		return nil, v2input.ErrInvalid
	}
	origin, ok := catalog.(ExportBodyFitFeatureOriginSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	policyReader, ok := catalog.(ExportBodyFitFeatureQualificationReader)
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
	detector, err := s.Detector.BuildExportDetectorQualifier(ctx, intent, checked, catalog, objects, documents, images, warps)
	if err != nil {
		return nil, err
	}
	inputs := BoundExportBodyFitFeatureOriginal{Source: origin, Images: images, Warps: warps, Faces: detector}
	policy, err := NewExportBodyFitFeaturePolicy(policyReader, objects, inputs, s.Configs)
	if err != nil {
		return nil, err
	}
	models := map[string]string{}
	for _, config := range s.Configs {
		switch config.Expected.Operation {
		case "pose.detect":
			models["keypointrcnn_resnet50_fpn"] = config.Expected.ModelLockSHA
		case "face.landmarks":
			models["fan2d"] = config.Expected.ModelLockSHA
		default:
			return nil, v2input.ErrInvalid
		}
	}
	qualifier, err := NewNativeExportBodyFitFeatureQualifier(ExportBodyFitFeatureParamsReader{Source: origin, Objects: objects}, policy, s.Scratch, s.Probe, models, s.RuntimeSHA)
	if err != nil {
		return nil, err
	}
	if err := validateExportAuthorityFunctions(ctx, scoped.DB, []string{"photo.v30_body_dependency_qualification_object(uuid,uuid,uuid,uuid,jsonb)"}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return qualifier, nil
}
