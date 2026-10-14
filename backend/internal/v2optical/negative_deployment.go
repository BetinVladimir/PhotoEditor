package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type negativeDeploymentEntry struct {
	Tenant    uuid.UUID                       `json:"tenant_id"`
	Project   uuid.UUID                       `json:"project_id"`
	Pin       NegativeQualificationPin     `json:"pin"`
	Locks     v2qualification.DeploymentLocks `json:"policy"`
	Capacity  NegativeResourceEstimate     `json:"capacity"`
}
type negativeDeploymentWire struct {
	SchemaVersion int                          `json:"schema_version"`
	Projects      []negativeDeploymentEntry `json:"projects"`
}
type NegativeDeployment struct{ raw string }

func DecodeNegativeDeployment(raw []byte) (NegativeDeployment, error) {
	var zero NegativeDeployment
	if len(raw) < 2 || len(raw) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var wire negativeDeploymentWire
	if json.Unmarshal(normal, &wire) != nil || wire.SchemaVersion != 1 || len(wire.Projects) < 1 || len(wire.Projects) > 2048 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	seen := map[[2]uuid.UUID]bool{}
	for _, entry := range wire.Projects {
		key := [2]uuid.UUID{entry.Tenant, entry.Project}
		if seen[key] || entry.Tenant == uuid.Nil || entry.Project == uuid.Nil {
			return zero, v2input.ErrInvalid
		}
		seen[key] = true
		if _, err := entry.Pin.Compile(); err != nil {
			return zero, err
		}
		expected, err := entry.Locks.Expected()
		if err != nil {
			return zero, err
		}
		policy, err := NegativePolicySHA(entry.Pin.PolicyID, entry.Locks)
		if err != nil || policy != entry.Pin.PolicySHA || expected.Operation != "optical.negative" || expected.Version != "1" || expected.Backend != "cpu" || expected.ModelLockSHA != entry.Pin.ModelLockSHA || expected.RuntimeLockSHA != entry.Pin.RuntimeLockSHA || expected.DeviceProfileSHA != entry.Pin.DeviceProfileSHA || expected.DatasetSHA != entry.Pin.DatasetSHA || expected.IssuerID != entry.Pin.IssuerID {
			return zero, v2input.ErrInvalid
		}
		if entry.Capacity.PixelCount < 1 || entry.Capacity.PixelCount > 200000000 || entry.Capacity.VRAMBytes != 0 {
			return zero, v2input.ErrInvalid
		}
		for _, n := range []int64{entry.Capacity.RAMBytes,entry.Capacity.MaterialScratchBytes,entry.Capacity.OutputScratchBytes, entry.Capacity.ScratchBytes, entry.Capacity.OutputBytes} {
			if n < 1 || n > 9007199254740991 {
				return zero, v2input.ErrInvalid
			}
		}
	 if entry.Capacity.ScratchBytes<entry.Capacity.MaterialScratchBytes+entry.Capacity.OutputScratchBytes { return zero,v2input.ErrInvalid }
	}
	return NegativeDeployment{raw: string(normal)}, nil
}

type NegativeProjects struct {
	services map[[2]uuid.UUID]*NegativeService
}

func (d NegativeDeployment) Build(db NegativeTransactions, objects *objectstore.Store, bucket string) (*NegativeProjects, error) {
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, v2input.ErrInvalid
	}
	validated, err := DecodeNegativeDeployment([]byte(d.raw))
	if err != nil {
		return nil, err
	}
	var wire negativeDeploymentWire
	if json.Unmarshal([]byte(validated.raw), &wire) != nil {
		return nil, v2input.ErrInvalid
	}
	projects := &NegativeProjects{services: make(map[[2]uuid.UUID]*NegativeService, len(wire.Projects))}
	for _, entry := range wire.Projects {
		service, err := NewNegativeService(NegativeCoordinator{DB: db, Reader: objects, Writer: objects, Bucket: bucket, Pin: entry.Pin, Locks: entry.Locks, Capacity: entry.Capacity}, entry.Tenant, entry.Project)
		if err != nil {
			return nil, err
		}
		projects.services[[2]uuid.UUID{entry.Tenant, entry.Project}] = service
	}
	return projects, nil
}
func (p *NegativeProjects) SubmitProjectIntent(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent NegativeIntent) (NegativeReceipt, error) {
	if err := ctx.Err(); err != nil {
		return NegativeReceipt{}, err
	}
	if p == nil {
		return NegativeReceipt{}, v2qualification.ErrUnqualified
	}
	service := p.services[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return NegativeReceipt{}, v2qualification.ErrUnqualified
	}
	return service.SubmitProjectIntent(ctx, who, tenant, project, key, intent)
}

