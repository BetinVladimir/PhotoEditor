package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

type SQLExportBodyDependencyInstallation struct {
	Configs    []ExportBodyDependencyPolicyConfig
	Scratch    *ExportArchiveScratch
	Probe      ExportNativeProbe
	RuntimeSHA string
}

// Exact model identifiers come from production contracts, never exported JSON.
func exportBodyDependencyNativeModels(configs []ExportBodyDependencyPolicyConfig) (map[string]string, error) {
	if len(configs) != 3 {
		return nil, v2input.ErrInvalid
	}
	models := map[string]string{}
	seen := map[string]bool{}
	for _, config := range configs {
		expected := config.Expected
		if !exportBodyDependencyQualificationOperation(expected.Operation) || seen[expected.Operation] || expected.Version != "1" || !expected.LicenseCleared || !validImportRef(config.Report) || !validImportRef(v2input.Ref{ArtifactID: config.Report.ArtifactID, SHA256: expected.ModelLockSHA}) {
			return nil, v2input.ErrInvalid
		}
		seen[expected.Operation] = true
		switch expected.Operation {
		case "pose.detect":
			models["keypointrcnn_resnet50_fpn"] = expected.ModelLockSHA
		case "face.landmarks":
			models["fan2d"] = expected.ModelLockSHA
		case "pair.fit.body":
			if expected.Backend != "cpu" {
				return nil, v2input.ErrInvalid
			}
		}
	}
	return models, nil
}
func (s SQLExportBodyDependencyInstallation) BuildExportBodyDependencyQualifier(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, catalog ExportDocumentCatalog, objects ImportDocumentObjects) (*NativeExportBodyDependencyQualifier, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scheduler, ok := catalog.(SQLSchedulerExportInputCatalog); ok {
		if objects == nil || s.Scratch == nil || s.Probe == nil {
			return nil, v2input.ErrInvalid
		}
		if _, err := schedulerExportPolicyBindings(ctx, intent, frozen, scheduler); err != nil {
			return nil, err
		}
		if err := SchedulerExportAdoptionSQLReady(ctx, scheduler.Claims.DB); err != nil {
			return nil, err
		}
		models, err := exportBodyDependencyNativeModels(s.Configs)
		if err != nil {
			return nil, err
		}
		profile, err := NewExportBodyDependencyPolicy(scheduler, objects, s.Configs)
		if err != nil {
			return nil, err
		}
		return NewNativeExportBodyDependencyQualifier(
			ExportBodyDependencyParamsReader{Source: scheduler, Objects: objects},
			profile, s.Scratch, s.Probe, models, s.RuntimeSHA)
	}
	scoped, err := exportPolicyCatalogConfig(catalog)
	if err != nil || scoped.DB == nil || objects == nil || s.Scratch == nil || s.Probe == nil {
		return nil, v2input.ErrInvalid
	}
	origin, ok := catalog.(ExportBodyDependencyOriginSource)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	policyReader, ok := catalog.(ExportBodyDependencyQualificationReader)
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
	models, err := exportBodyDependencyNativeModels(s.Configs)
	if err != nil {
		return nil, err
	}
	profile, err := NewExportBodyDependencyPolicy(policyReader, objects, s.Configs)
	if err != nil {
		return nil, err
	}
	qualifier, err := NewNativeExportBodyDependencyQualifier(ExportBodyDependencyParamsReader{Source: origin, Objects: objects}, profile, s.Scratch, s.Probe, models, s.RuntimeSHA)
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
