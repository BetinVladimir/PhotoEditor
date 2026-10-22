package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
 "errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type relightLimitWire struct {
	PointChecks     int64 `json:"point_checks"`
	EdgeEvaluations int64 `json:"edge_evaluations"`
	PayloadBytes    int64 `json:"payload_bytes"`
}

func (l relightLimitWire) limits() (RelightManualRasterLimits, error) {
	if l.PointChecks < 1 || l.EdgeEvaluations < 1 || l.PayloadBytes < 1 || l.PointChecks > 1<<40 || l.EdgeEvaluations > 1<<40 || l.PayloadBytes > 1<<30 {
		return RelightManualRasterLimits{}, v2input.ErrInvalid
	}
	return RelightManualRasterLimits{PointChecks: l.PointChecks, EdgeEvaluations: l.EdgeEvaluations, PayloadBytes: l.PayloadBytes}, nil
}

type relightProjectWire struct {
	Tenant         uuid.UUID               `json:"tenant_id"`
	Project        uuid.UUID               `json:"project_id"`
	Pin            RelightQualificationPin `json:"qualification"`
	Checks         []string                `json:"required_checks"`
	Fixtures       []string                `json:"test_fixture_shas"`
	Evidence       []string                `json:"evidence_shas"`
	Calibrations   []string                `json:"calibration_shas"`
	LicenseCleared bool                    `json:"license_cleared"`
	Capacity       LocalResourceEstimate   `json:"capacity"`
	Raster         relightLimitWire        `json:"raster_limits"`
	Batch          relightLimitWire        `json:"batch_limits"`
}
type relightDeploymentWire struct {
	SchemaVersion int                  `json:"schema_version"`
	Projects      []relightProjectWire `json:"projects"`
}

// RelightDeployment is owned operator configuration, not activation authority.
type RelightDeployment struct{ projects []relightProjectWire }

func relightDigestSet(values []string) (map[string]struct{}, error) {
	if len(values) > 256 {
		return nil, v2input.ErrInvalid
	}
	result := map[string]struct{}{}
	for _, value := range values {
		if _, err := domain.ParseDigest(value); err != nil {
			return nil, err
		}
		if _, exists := result[value]; exists {
			return nil, v2input.ErrInvalid
		}
		result[value] = struct{}{}
	}
	return result, nil
}
func (p relightProjectWire) expected() (v2qualification.Expected, error) {
	var zero v2qualification.Expected
	if p.Tenant == uuid.Nil || p.Project == uuid.Nil || !p.LicenseCleared || len(p.Checks) < 1 || len(p.Checks) > 256 || len(p.Fixtures) < 1 || len(p.Evidence) < 1 {
		return zero, v2input.ErrInvalid
	}
	if _, err := p.Pin.Compile(); err != nil {
		return zero, err
	}
	seen := map[string]bool{}
	for _, check := range p.Checks {
		if check == "" || len(check) > 128 || seen[check] {
			return zero, v2input.ErrInvalid
		}
		seen[check] = true
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
	if _, err = p.Raster.limits(); err != nil {
		return zero, err
	}
	if _, err = p.Batch.limits(); err != nil {
		return zero, err
	}
	if p.Batch.PointChecks < p.Raster.PointChecks || p.Batch.EdgeEvaluations < p.Raster.EdgeEvaluations || p.Batch.PayloadBytes < p.Raster.PayloadBytes {
		return zero, v2input.ErrInvalid
	}
	c := p.Capacity
	if c.PixelCount < 1 || c.PixelCount > 200000000 || c.RAMBytes < 1 || c.ScratchBytes < 1 || c.OutputBytes < 1 || c.VRAMBytes != 0 || c.RAMBytes > 1<<50 || c.ScratchBytes > 1<<50 || c.OutputBytes > 1<<50 {
		return zero, v2input.ErrInvalid
	}
	return v2qualification.Expected{Operation: "image.relight", Version: "1", Backend: "cpu", ModelLockSHA: p.Pin.ModelLockSHA, RuntimeLockSHA: p.Pin.RuntimeLockSHA, DeviceProfileSHA: p.Pin.DeviceProfileSHA, DatasetSHA: p.Pin.DatasetSHA, IssuerID: p.Pin.IssuerID, RequiredChecks: append([]string(nil), p.Checks...), TestFixtureSHAs: fixtures, EvidenceSHAs: evidence, CalibrationSHAs: calibrations, LicenseCleared: true}, nil
}

func DecodeRelightDeployment(raw []byte) (RelightDeployment, error) {
	var zero RelightDeployment
	if len(raw) < 2 || len(raw) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
	}
	var wire relightDeploymentWire
	if json.Unmarshal(normal, &wire) != nil || wire.SchemaVersion != 1 || len(wire.Projects) < 1 || len(wire.Projects) > 1024 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	seen := map[[2]uuid.UUID]bool{}
	for _, p := range wire.Projects {
		key := [2]uuid.UUID{p.Tenant, p.Project}
		if seen[key] {
			return zero, v2input.ErrInvalid
		}
		seen[key] = true
		if _, err := p.expected(); err != nil {
			return zero, err
		}
	}
	return RelightDeployment{projects: wire.Projects}, nil
}

type RelightProjectServices struct {
 db DonorConfirmationTransactions
	services map[[2]uuid.UUID]*RelightSubmissionService
}

func (d RelightDeployment) Build(db DonorConfirmationTransactions, objects *objectstore.Store, bucket string) (*RelightProjectServices, error) {
	if db == nil || objects == nil || bucket == "" || len(d.projects) == 0 {
		return nil, v2input.ErrInvalid
	}
	result := &RelightProjectServices{db: db, services: map[[2]uuid.UUID]*RelightSubmissionService{}}
	for _, p := range d.projects {
		expected, err := p.expected()
		if err != nil {
			return nil, err
		}
		raster, _ := p.Raster.limits()
		batch, _ := p.Batch.limits()
		service, err := NewRelightSubmissionService(RelightBootstrapCoordinator{DB: db, Objects: objects, Bucket: bucket, Expected: expected, RasterLimits: raster, BatchLimits: batch}, objects, p.Tenant, p.Project, p.Pin, p.Capacity)
		if err != nil {
			return nil, err
		}
		result.services[[2]uuid.UUID{p.Tenant, p.Project}] = service
	}
	return result, nil
}
func (s *RelightProjectServices) SubmitProjectKeyed(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent RelightDraftIntent) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, v2qualification.ErrUnqualified
	}
 // Receipt recovery uses persisted pins before current provider lookup.
 // The minimal service derives/validates IDs only; it cannot publish work.
 if s.db != nil {
  request, err := (&RelightSubmissionService{tenant: tenant, project: project}).keyedDraftRequest(ctx, who, key, intent)
  if err != nil { return nil, err }
  body, err := canonical.Marshal(relightSubmissionWire{SchemaVersion:1, Snapshot:request.Snapshot, Run:request.Run, Workflow:request.Workflow, Ordinal:request.Ordinal, SubjectKind:request.SubjectKind, Params:request.ParamsRaw})
  if err != nil { return nil, err }
  receipt, err := (RelightBootstrapCoordinator{DB:s.db}).readRelightQueuedReceipt(ctx,who,tenant,request.Run,&relightReceiptBinding{project:project,request:body})
  if err == nil { return receipt, nil }
  if !errors.Is(err,v2input.ErrExpired) || IsRelightReplayOnly(ctx) { return nil, err }
 }
	service := s.services[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return nil, v2qualification.ErrUnqualified
	}
	return service.SubmitProjectKeyed(ctx, who, tenant, project, key, intent)
}
