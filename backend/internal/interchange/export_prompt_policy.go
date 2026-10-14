package interchange

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type ExportPromptPolicyReader interface {
	ReadExportPromptQualification(context.Context, ImportDocumentObjects, v2input.Ref, v2qualification.Expected) (ExportPromptQualification, error)
	QualifyExportPromptProfile(context.Context, ExportSeriesMaskOrigin, v2input.SessionVersionBinding, v2qualification.Expected) error
}
type ExportPromptMaskPolicy struct {
	reader   ExportPromptPolicyReader
	objects  ImportDocumentObjects
	bindings map[v2input.Ref]v2input.SessionVersionBinding
	report   v2input.Ref
	expected []byte
}

func NewExportPromptMaskPolicy(reader ExportPromptPolicyReader, objects ImportDocumentObjects, bindings []v2input.SessionVersionBinding, report v2input.Ref, expected v2qualification.Expected) (*ExportPromptMaskPolicy, error) {
	if reader == nil || objects == nil || !validImportRef(report) || expected.Operation != "mask.prompt" || expected.Version != "1" || !expected.LicenseCleared || len(bindings) == 0 || len(bindings) > 30000 {
		return nil, v2input.ErrInvalid
	}
	if expected.Backend != "cpu" && expected.Backend != "cuda" && expected.Backend != "mps" {
		return nil, v2input.ErrInvalid
	}
	for _, sha := range []string{expected.ModelLockSHA, expected.RuntimeLockSHA, expected.DeviceProfileSHA, expected.DatasetSHA} {
		if !validImportRef(v2input.Ref{ArtifactID: report.ArtifactID, SHA256: sha}) {
			return nil, v2input.ErrInvalid
		}
	}
	frozen, err := canonical.Marshal(expected)
	if err != nil {
		return nil, err
	}
	result := &ExportPromptMaskPolicy{reader: reader, objects: objects, report: report, expected: frozen, bindings: map[v2input.Ref]v2input.SessionVersionBinding{}}
	for _, binding := range bindings {
		if binding.AssetVersionID == ([16]byte{}) || !validImportRef(binding.Source) || !validImportRef(v2input.Ref{ArtifactID: binding.Source.ArtifactID, SHA256: binding.CanvasHash}) {
			return nil, v2input.ErrInvalid
		}
		if _, ok := result.bindings[binding.Source]; ok {
			return nil, v2input.ErrInvalid
		}
		result.bindings[binding.Source] = binding
	}
	return result, nil
}
func (s *ExportPromptMaskPolicy) ResolveExportSeriesMaskPolicy(ctx context.Context, origin ExportSeriesMaskOrigin, source v2input.Ref) (ExportSeriesMaskPolicyEvidence, error) {
	var zero ExportSeriesMaskPolicyEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || s.reader == nil || s.objects == nil || origin.Operation != "mask.prompt" || origin.Version != "1" {
		return zero, v2input.ErrInvalid
	}
	binding, ok := s.bindings[source]
	if !ok {
		return zero, v2input.ErrInvalid
	}
	expected := func() (v2qualification.Expected, error) {
		var out v2qualification.Expected
		err := json.Unmarshal(s.expected, &out)
		return out, err
	}
	first, err := expected()
	if err != nil {
		return zero, err
	}
	before, err := s.reader.ReadExportPromptQualification(ctx, s.objects, s.report, first)
	if err != nil {
		return zero, err
	}
	if before.Report != s.report {
		return zero, v2input.ErrInvalid
	}
	profile, err := expected()
	if err != nil {
		return zero, err
	}
	if err := s.reader.QualifyExportPromptProfile(ctx, origin, binding, profile); err != nil {
		return zero, err
	}
	last, err := expected()
	if err != nil {
		return zero, err
	}
	after, err := s.reader.ReadExportPromptQualification(ctx, s.objects, s.report, last)
	if err != nil {
		return zero, err
	}
	if after != before {
		return zero, v2input.ErrInvalid
	}
	// Qualification verification performs object IO. Recheck the producer and
	// its consent epochs afterwards rather than returning the earlier permission.
	finalProfile, err := expected()
	if err != nil {
		return zero, err
	}
	if err := s.reader.QualifyExportPromptProfile(ctx, origin, binding, finalProfile); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	configured, err := expected()
	if err != nil {
		return zero, err
	}
	return ExportSeriesMaskPolicyEvidence{Binding: binding, ModelID: "mobilesam", ModelLock: configured.ModelLockSHA, Backend: configured.Backend, QualificationSHA: s.report.SHA256}, ctx.Err()
}

var _ ExportPromptPolicyReader = SQLExportDocumentCatalog{}
var _ ExportPromptPolicyReader = SQLExportResultInputCatalog{}
var _ ExportSeriesMaskPolicyAuthority = (*ExportPromptMaskPolicy)(nil)
