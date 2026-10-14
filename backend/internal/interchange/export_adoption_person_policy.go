package interchange

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// SchedulerExportStatsPersonPolicy keeps the operator's immutable model policy
// and frozen source set, while all live proofs run under the export claim.
type SchedulerExportStatsPersonPolicy struct {
	catalog  SQLSchedulerExportInputCatalog
	objects  ImportDocumentObjects
	bindings map[v2input.Ref]v2input.SessionVersionBinding
	report   v2input.Ref
	expected []byte
}

func NewSchedulerExportStatsPersonPolicy(catalog SQLSchedulerExportInputCatalog,
	objects ImportDocumentObjects, bindings []v2input.SessionVersionBinding,
	report v2input.Ref, expected v2qualification.Expected) (*SchedulerExportStatsPersonPolicy, error) {
	if catalog.Claims.DB == nil || catalog.Claim.validate() != nil || objects == nil ||
		!validImportRef(report) || expected.Operation != "mask.person" ||
		expected.Version != "1" || !expected.LicenseCleared ||
		len(bindings) == 0 || len(bindings) > 30000 ||
		(expected.Backend != "cpu" && expected.Backend != "cuda" && expected.Backend != "mps") {
		return nil, v2input.ErrInvalid
	}
	for _, digest := range []string{expected.ModelLockSHA, expected.RuntimeLockSHA,
		expected.DeviceProfileSHA, expected.DatasetSHA} {
		if !validImportRefForSHA(digest) {
			return nil, v2input.ErrInvalid
		}
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return nil, err
	}
	policy := &SchedulerExportStatsPersonPolicy{catalog: catalog, objects: objects,
		bindings: make(map[v2input.Ref]v2input.SessionVersionBinding, len(bindings)),
		report:   report, expected: body}
	for _, binding := range bindings {
		if binding.AssetVersionID == uuid.Nil || !validImportRef(binding.Source) ||
			!validImportRefForSHA(binding.CanvasHash) {
			return nil, v2input.ErrInvalid
		}
		if _, exists := policy.bindings[binding.Source]; exists {
			return nil, v2input.ErrInvalid
		}
		policy.bindings[binding.Source] = binding
	}
	return policy, nil
}

func (s *SchedulerExportStatsPersonPolicy) ResolveExportStatsPersonPolicy(ctx context.Context,
	origin ExportStatsPersonOrigin, source v2input.Ref) (ExportStatsPersonPolicyEvidence, error) {
	var zero ExportStatsPersonPolicyEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || s.objects == nil || !validImportRef(source) ||
		!validImportRef(origin.Root) || !validImportRef(origin.Params) ||
		!validImportRef(origin.Report) {
		return zero, v2input.ErrInvalid
	}
	binding, ok := s.bindings[source]
	if !ok {
		return zero, v2input.ErrInvalid
	}
	readExpected := func() (v2qualification.Expected, error) {
		var value v2qualification.Expected
		err := json.Unmarshal(s.expected, &value)
		return value, err
	}
	first, err := readExpected()
	if err != nil {
		return zero, err
	}
	before, err := s.catalog.ReadExportPersonQualification(ctx, s.objects, s.report, first)
	if err != nil {
		return zero, err
	}
	profile, err := readExpected()
	if err != nil {
		return zero, err
	}
	if err := s.catalog.QualifyExportPersonProfile(ctx, origin, binding, profile); err != nil {
		return zero, err
	}
	last, err := readExpected()
	if err != nil {
		return zero, err
	}
	after, err := s.catalog.ReadExportPersonQualification(ctx, s.objects, s.report, last)
	if err != nil {
		return zero, err
	}
	if after != before {
		return zero, v2input.ErrInvalid
	}
	final, err := readExpected()
	if err != nil {
		return zero, err
	}
	if err := s.catalog.QualifyExportPersonProfile(ctx, origin, binding, final); err != nil {
		return zero, err
	}
	return ExportStatsPersonPolicyEvidence{Binding: binding,
		ModelLock: final.ModelLockSHA, Backend: final.Backend,
		QualificationSHA: s.report.SHA256}, ctx.Err()
}

var _ ExportStatsPersonPolicyAuthority = (*SchedulerExportStatsPersonPolicy)(nil)
