package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// RelightSubmissionRequest contains authored intent and existing draft IDs only.
// Identity is supplied separately by authenticated transport middleware.
type RelightSubmissionRequest struct {
	Snapshot, Run, Workflow uuid.UUID
	Ordinal                 int
	SubjectKind             string
	ParamsRaw               []byte
}

type RelightSubmissionService struct {
	coordinator     RelightBootstrapCoordinator
	objects         artifactjson.Objects
	tenant, project uuid.UUID
	pin             RelightQualificationPin
	capacity        LocalResourceEstimate
}

// NewRelightSubmissionService binds one tenant/project to deployment-selected
// qualification and measured capacity. Construction alone never enables SQL
// registry execution or qualifies reports; submission repeats all live gates.
func NewRelightSubmissionService(coordinator RelightBootstrapCoordinator, objects artifactjson.Objects, tenant, project uuid.UUID, pin RelightQualificationPin, capacity LocalResourceEstimate) (*RelightSubmissionService, error) {
	e := coordinator.Expected
	if coordinator.DB == nil || coordinator.Objects == nil || coordinator.Bucket == "" || objects == nil || tenant == uuid.Nil || project == uuid.Nil || e.Operation != "image.relight" || e.Version != "1" || e.Backend != "cpu" || !e.LicenseCleared || e.RuntimeLockSHA != pin.RuntimeLockSHA || e.ModelLockSHA != pin.ModelLockSHA || e.DeviceProfileSHA != pin.DeviceProfileSHA || e.DatasetSHA != pin.DatasetSHA || e.IssuerID != pin.IssuerID || capacity.PixelCount < 1 || capacity.PixelCount > 200000000 || capacity.RAMBytes < 1 || capacity.ScratchBytes < 1 || capacity.OutputBytes < 1 || capacity.VRAMBytes != 0 {
		return nil, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return nil, err
	}
	e.RequiredChecks = append([]string(nil), e.RequiredChecks...)
	copyMap := func(src map[string]struct{}) map[string]struct{} {
		if src == nil {
			return nil
		}
		dst := make(map[string]struct{}, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}
	e.CalibrationSHAs = copyMap(e.CalibrationSHAs)
	e.TestFixtureSHAs = copyMap(e.TestFixtureSHAs)
	e.EvidenceSHAs = copyMap(e.EvidenceSHAs)
	coordinator.Expected = e
	return &RelightSubmissionService{coordinator: coordinator, objects: objects, tenant: tenant, project: project, pin: pin, capacity: capacity}, nil
}

func (s *RelightSubmissionService) Submit(ctx context.Context, who auth.Identity, request RelightSubmissionRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || request.Snapshot == uuid.Nil || request.Run == uuid.Nil || request.Workflow == uuid.Nil || request.Ordinal < 0 || request.Ordinal >= 5000 || (request.SubjectKind != "manual" && request.SubjectKind != "person") {
		return nil, v2input.ErrInvalid
	}
	return s.coordinator.SubmitResolvedRelight(ctx, s.objects, RelightBootstrapRequest{Who: who, Tenant: s.tenant, Project: s.project, Snapshot: request.Snapshot, Run: request.Run, Workflow: request.Workflow, Ordinal: request.Ordinal, SubjectKind: request.SubjectKind, ParamsRaw: append([]byte(nil), request.ParamsRaw...), RuntimeSHA: s.pin.RuntimeLockSHA, Capacity: s.capacity, Pin: s.pin})
}

// SubmitJSON validates the authored service envelope before any catalog IO.
func (s *RelightSubmissionService) SubmitJSON(ctx context.Context, who auth.Identity, raw []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request, err := DecodeRelightSubmissionRequest(raw)
	if err != nil {
		return nil, err
	}
	return s.Submit(ctx, who, request)
}
