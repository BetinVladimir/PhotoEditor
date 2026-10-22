package v2portrait

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type analysisDeploymentEntry struct {
	Tenant         uuid.UUID              `json:"tenant_id"`
	Project        uuid.UUID              `json:"project_id"`
	Operation      string                 `json:"operation"`
	Runtime        RelightAnalysisRuntime `json:"runtime"`
	DatasetSHA     string                 `json:"dataset_sha"`
	Issuer         uuid.UUID              `json:"issuer_id"`
	Checks         []string               `json:"required_checks"`
	Fixtures       []string               `json:"test_fixture_shas"`
	Evidence       []string               `json:"evidence_shas"`
	Calibrations   []string               `json:"calibration_shas"`
	LicenseCleared bool                   `json:"license_cleared"`
	Capacity       LocalResourceEstimate  `json:"capacity"`
}
type analysisDeploymentWire struct {
	SchemaVersion int                       `json:"schema_version"`
	Projects      []analysisDeploymentEntry `json:"projects"`
}
type RelightAnalysisDeployment struct{ projects []analysisDeploymentEntry }

func (p analysisDeploymentEntry) expected() (v2qualification.Expected, error) {
	var zero v2qualification.Expected
	if p.Tenant == uuid.Nil || p.Project == uuid.Nil || (p.Operation != "depth.predict" && p.Operation != "mask.person") || p.Issuer == uuid.Nil || !p.LicenseCleared || len(p.Checks) < 1 || len(p.Checks) > 256 || len(p.Fixtures) < 1 || len(p.Evidence) < 1 || !validRelightTransportRef(p.Runtime.Qualification) {
		return zero, v2input.ErrInvalid
	}
	seen := map[string]bool{}
	for _, check := range p.Checks {
		if len(check) < 1 || len(check) > 100 || seen[check] {
			return zero, v2input.ErrInvalid
		}
		seen[check] = true
	}
	for _, sha := range []string{p.DatasetSHA, p.Runtime.ModelLockSHA, p.Runtime.RuntimeLockSHA, p.Runtime.DeviceProfileSHA} {
		if _, err := domain.ParseDigest(sha); err != nil {
			return zero, err
		}
	}
	fixtures, err := relightDigestSet(p.Fixtures)
	if err != nil {
		return zero, err
	}
	evidence, err := relightDigestSet(p.Evidence)
	if err != nil {
		return zero, err
	}
	calibrations, err := relightDigestSet(p.Calibrations)
	if err != nil {
		return zero, err
	}
	for sha := range fixtures {
		if _, overlap := calibrations[sha]; overlap {
			return zero, v2input.ErrInvalid
		}
	}
	if p.Runtime.Backend != "cpu" && p.Runtime.Backend != "mps" && p.Runtime.Backend != "cuda" {
		return zero, v2input.ErrInvalid
	}
	if p.Capacity.PixelCount < 1 || p.Capacity.PixelCount > 200000000 || p.Capacity.RAMBytes < 1<<30 || p.Capacity.ScratchBytes < 32<<20 || p.Capacity.OutputBytes < 1 || p.Capacity.VRAMBytes < 0 {
		return zero, v2input.ErrInvalid
	}
	return v2qualification.Expected{Operation: p.Operation, Version: "1", Backend: p.Runtime.Backend, ModelLockSHA: p.Runtime.ModelLockSHA, RuntimeLockSHA: p.Runtime.RuntimeLockSHA, DeviceProfileSHA: p.Runtime.DeviceProfileSHA, DatasetSHA: p.DatasetSHA, IssuerID: p.Issuer, RequiredChecks: append([]string(nil), p.Checks...), TestFixtureSHAs: fixtures, EvidenceSHAs: evidence, CalibrationSHAs: calibrations, LicenseCleared: true}, nil
}
func DecodeRelightAnalysisDeployment(raw []byte) (RelightAnalysisDeployment, error) {
	var zero RelightAnalysisDeployment
	if len(raw) < 2 || len(raw) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var wire analysisDeploymentWire
	if json.Unmarshal(normal, &wire) != nil || wire.SchemaVersion != 1 || len(wire.Projects) < 1 || len(wire.Projects) > 2048 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(normal, exact) {
		return zero, v2input.ErrInvalid
	}
	seen := map[analysisProjectKey]bool{}
	for _, p := range wire.Projects {
		key := analysisProjectKey{p.Tenant, p.Project, p.Operation}
		if seen[key] {
			return zero, v2input.ErrInvalid
		}
		seen[key] = true
		if _, err := p.expected(); err != nil {
			return zero, err
		}
	}
	return RelightAnalysisDeployment{projects: wire.Projects}, nil
}
func (d RelightAnalysisDeployment) Build(db DonorConfirmationTransactions, objects *objectstore.Store, bucket string) (*RelightAnalysisProjects, error) {
	if db == nil || objects == nil || bucket == "" || len(d.projects) == 0 {
		return nil, v2input.ErrInvalid
	}
	services := make([]*RelightAnalysisService, 0, len(d.projects))
	for _, p := range d.projects {
		expected, err := p.expected()
		if err != nil {
			return nil, err
		}
		service, err := NewRelightAnalysisService(RelightAnalysisCoordinator{DB: db, Reader: objects, Writer: objects, Bucket: bucket, Expected: expected}, p.Tenant, p.Project, p.Runtime, p.Capacity)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	return NewRelightAnalysisProjects(services...)
}
