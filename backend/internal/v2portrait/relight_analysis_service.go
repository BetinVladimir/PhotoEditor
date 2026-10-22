package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

type RelightAnalysisIntent struct {
	SchemaVersion int                           `json:"schema_version"`
	Snapshot      uuid.UUID                     `json:"snapshot_id"`
	Base          v2input.SessionVersionBinding `json:"base"`
	Operation     string                        `json:"operation"`
}

func DecodeRelightAnalysisIntent(raw []byte) (RelightAnalysisIntent, error) {
	var zero RelightAnalysisIntent
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var request RelightAnalysisIntent
	if json.Unmarshal(normal, &request) != nil || request.SchemaVersion != 1 || (request.Operation != "depth.predict" && request.Operation != "mask.person") {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(normal, exact) {
		return zero, v2input.ErrInvalid
	}
	if err = (RelightCandidateQuery{Snapshot: request.Snapshot, Base: request.Base, Kind: "depth"}).validate(); err != nil {
		return zero, err
	}
	return request, nil
}

// Service pins one tenant/project/operation to deployment-owned qualification
// and measured capacity. Public requests cannot select models or resource pins.
type RelightAnalysisService struct {
	coordinator     RelightAnalysisCoordinator
	tenant, project uuid.UUID
	runtime         RelightAnalysisRuntime
	capacity        LocalResourceEstimate
}

func NewRelightAnalysisService(c RelightAnalysisCoordinator, tenant, project uuid.UUID, runtime RelightAnalysisRuntime, capacity LocalResourceEstimate) (*RelightAnalysisService, error) {
	e := c.Expected
	if c.DB == nil || c.Reader == nil || c.Writer == nil || c.Bucket == "" || len(c.Bucket) > 255 || tenant == uuid.Nil || project == uuid.Nil || (e.Operation != "depth.predict" && e.Operation != "mask.person") || e.Version != "1" || e.Backend != runtime.Backend || e.ModelLockSHA != runtime.ModelLockSHA || e.RuntimeLockSHA != runtime.RuntimeLockSHA || e.DeviceProfileSHA != runtime.DeviceProfileSHA || !e.LicenseCleared || e.IssuerID == uuid.Nil || len(e.RequiredChecks) == 0 || len(e.TestFixtureSHAs) == 0 || len(e.EvidenceSHAs) == 0 || !validRelightTransportRef(runtime.Qualification) {
		return nil, v2input.ErrInvalid
	}
	for _, sha := range []string{e.ModelLockSHA, e.RuntimeLockSHA, e.DeviceProfileSHA, e.DatasetSHA} {
		if _, err := domain.ParseDigest(sha); err != nil {
			return nil, v2input.ErrInvalid
		}
	}
	if runtime.Backend != "cpu" && runtime.Backend != "mps" && runtime.Backend != "cuda" {
		return nil, v2input.ErrInvalid
	}
	need := runtime.Resources
	if need.PixelCount < 1 || need.RAMBytes < 1<<30 || need.ScratchBytes < 32<<20 || need.OutputBytes < 1 || need.VRAMBytes < 0 || (runtime.Backend == "cpu" && need.VRAMBytes != 0) || (runtime.Backend != "cpu" && need.VRAMBytes < 1) {
		return nil, v2input.ErrInvalid
	}
	for i, n := range []int64{need.PixelCount, need.RAMBytes, need.ScratchBytes, need.OutputBytes, need.VRAMBytes} {
		maximum := []int64{capacity.PixelCount, capacity.RAMBytes, capacity.ScratchBytes, capacity.OutputBytes, capacity.VRAMBytes}[i]
		if n > maximum || maximum > 9007199254740991 {
			return nil, v2input.ErrInvalid
		}
	}
	if capacity.PixelCount > 200000000 {
		return nil, v2input.ErrInvalid
	}
	cloneSet := func(src map[string]struct{}) map[string]struct{} {
		dst := make(map[string]struct{}, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}
	e.RequiredChecks = append([]string(nil), e.RequiredChecks...)
	e.TestFixtureSHAs = cloneSet(e.TestFixtureSHAs)
	e.EvidenceSHAs = cloneSet(e.EvidenceSHAs)
	e.CalibrationSHAs = cloneSet(e.CalibrationSHAs)
	c.Expected = e
	return &RelightAnalysisService{coordinator: c, tenant: tenant, project: project, runtime: runtime, capacity: capacity}, nil
}
func (s *RelightAnalysisService) SubmitIntent(ctx context.Context, who auth.Identity, key string, intent RelightAnalysisIntent) (RelightAnalysisReceipt, error) {
	var zero RelightAnalysisReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	intent, err = DecodeRelightAnalysisIntent(raw)
	if err != nil {
		return zero, err
	}
	if intent.Operation != s.coordinator.Expected.Operation {
		return zero, v2input.ErrInvalid
	}
	reader := SQLRelightSourceReader{DB: s.coordinator.DB, Objects: s.coordinator.Reader, Bucket: s.coordinator.Bucket}
	source, err := reader.Load(ctx, who, s.tenant, s.project, intent.Snapshot, intent.Base)
	if err != nil {
		return zero, err
	}
	request := RelightAnalysisPlanRequest{Tenant: s.tenant, Project: s.project, Actor: who.UserID, Session: who.SessionID, Snapshot: intent.Snapshot, Key: key, Operation: intent.Operation, Base: intent.Base, SourceRaw: source, Runtime: s.runtime, Capacity: s.capacity}
	minimum, err := RelightAnalysisMinimumResources(request)
	if err != nil {
		return zero, err
	}
	// Keep deployment-measured reserves, raising only to deterministic source
	// lower bounds. Compiler rejects any resulting reserve above capacity.
	request.Runtime.Resources.PixelCount = minimum.PixelCount
	if request.Runtime.Resources.RAMBytes < minimum.RAMBytes {
		request.Runtime.Resources.RAMBytes = minimum.RAMBytes
	}
	if request.Runtime.Resources.ScratchBytes < minimum.ScratchBytes {
		request.Runtime.Resources.ScratchBytes = minimum.ScratchBytes
	}
	if request.Runtime.Resources.OutputBytes < minimum.OutputBytes {
		request.Runtime.Resources.OutputBytes = minimum.OutputBytes
	}
	return s.coordinator.Submit(ctx, who, request)
}
