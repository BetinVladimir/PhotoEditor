package interchange

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type ExportBodyDependencyQualificationReader interface {
	ReadExportBodyDependencyQualification(context.Context, ImportDocumentObjects, v2input.Ref, v2qualification.Expected) (ExportBodyDependencyQualification, error)
	MatchExportBodyDependencyProfile(context.Context, ExportBodyDependencyOrigin, v2input.Entry, []byte, v2input.Entry, v2qualification.Expected) error
}
type ExportBodyDependencyPolicyConfig struct {
	Report   v2input.Ref
	Expected v2qualification.Expected
}
type exportBodyDependencyFrozenPolicy struct {
	report   v2input.Ref
	expected []byte
}
type ExportBodyDependencyPolicy struct {
	reader   ExportBodyDependencyQualificationReader
	objects  ImportDocumentObjects
	policies map[string]exportBodyDependencyFrozenPolicy
}

func NewExportBodyDependencyPolicy(reader ExportBodyDependencyQualificationReader, objects ImportDocumentObjects, configs []ExportBodyDependencyPolicyConfig) (*ExportBodyDependencyPolicy, error) {
	if reader == nil || objects == nil || len(configs) < 1 || len(configs) > 3 {
		return nil, v2input.ErrInvalid
	}
	result := &ExportBodyDependencyPolicy{reader: reader, objects: objects, policies: map[string]exportBodyDependencyFrozenPolicy{}}
	for _, config := range configs {
		expected := config.Expected
		if !exportBodyDependencyQualificationOperation(expected.Operation) || expected.Version != "1" || !expected.LicenseCleared || !validImportRef(config.Report) {
			return nil, v2input.ErrInvalid
		}
		if expected.Backend != "cpu" && expected.Backend != "cuda" && expected.Backend != "mps" {
			return nil, v2input.ErrInvalid
		}
		for _, sha := range []string{expected.ModelLockSHA, expected.RuntimeLockSHA, expected.DeviceProfileSHA, expected.DatasetSHA} {
			if !validImportRef(v2input.Ref{ArtifactID: config.Report.ArtifactID, SHA256: sha}) {
				return nil, v2input.ErrInvalid
			}
		}
		if _, exists := result.policies[expected.Operation]; exists {
			return nil, v2input.ErrInvalid
		}
		frozen, err := canonical.Marshal(expected)
		if err != nil {
			return nil, err
		}
		result.policies[expected.Operation] = exportBodyDependencyFrozenPolicy{report: config.Report, expected: frozen}
	}
	return result, nil
}
func (s *ExportBodyDependencyPolicy) QualifyExportBodyDependencyProfile(ctx context.Context, origin ExportBodyDependencyOrigin, parent v2input.Entry, raw []byte, child v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.reader == nil || s.objects == nil || len(raw) < 2 || len(raw) > 1<<20 {
		return v2input.ErrInvalid
	}
	configured, ok := s.policies[origin.Producer.Operation]
	if !ok {
		return v2qualification.ErrUnqualified
	}
	body := append([]byte(nil), raw...)
	fresh := func() (v2qualification.Expected, error) {
		var expected v2qualification.Expected
		err := json.Unmarshal(configured.expected, &expected)
		return expected, err
	}
	first, err := fresh()
	if err != nil {
		return err
	}
	before, err := s.reader.ReadExportBodyDependencyQualification(ctx, s.objects, configured.report, first)
	if err != nil {
		return err
	}
	if before.Report != configured.report {
		return v2input.ErrInvalid
	}
	expected, err := fresh()
	if err != nil {
		return err
	}
	if err := s.reader.MatchExportBodyDependencyProfile(ctx, origin, parent, append([]byte(nil), body...), child, expected); err != nil {
		return err
	}
	last, err := fresh()
	if err != nil {
		return err
	}
	after, err := s.reader.ReadExportBodyDependencyQualification(ctx, s.objects, configured.report, last)
	if err != nil {
		return err
	}
	if after != before {
		return v2input.ErrInvalid
	}
	final, err := fresh()
	if err != nil {
		return err
	}
	if err := s.reader.MatchExportBodyDependencyProfile(ctx, origin, parent, append([]byte(nil), body...), child, final); err != nil {
		return err
	}
	return ctx.Err()
}

var _ ExportBodyDependencyQualificationReader = SQLExportDocumentCatalog{}
var _ ExportBodyDependencyQualificationReader = SQLExportResultInputCatalog{}
var _ ExportBodyDependencyProfileAuthority = (*ExportBodyDependencyPolicy)(nil)
