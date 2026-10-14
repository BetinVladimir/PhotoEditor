package v2optical

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Deployment owns operation/pins/resource ceilings; requests cannot choose them.
type MultiCoordinator struct {
	DB        PerspectiveTransactions
	Reader    v2input.SessionAnalysisObjectReader
	Writer    artifactjson.Objects
	Bucket    string
	Operation string
	Pin       MultiQualificationPin
	Locks     v2qualification.DeploymentLocks
	MaxPixels int64
	Capacity  MultiResourceEstimate
}
type MultiPrepareRequest struct {
	Tenant, Project, Snapshot uuid.UUID
	Key                       string
	Params                    []byte
	Sources                   [][]byte
}

func (r MultiPrepareRequest) owned() MultiPrepareRequest {
	r.Params = append([]byte(nil), r.Params...)
	sources := make([][]byte, len(r.Sources))
	for i, source := range r.Sources {
		sources[i] = append([]byte(nil), source...)
	}
	r.Sources = sources
	return r
}

type PreparedMulti struct {
	draft         MultiDraft
	params        artifactjson.Ref
	qualification LocatedMultiQualification
}

func (p PreparedMulti) Draft() MultiDraft        { return MultiDraft{raw: p.draft.raw} }
func (p PreparedMulti) Params() artifactjson.Ref { return p.params }

// rebuild performs original evidence and array IO before any durable mutation.
func (c MultiCoordinator) rebuild(ctx context.Context, who auth.Identity, request MultiPrepareRequest) (PreparedMulti, error) {
	var zero PreparedMulti
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Reader == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || c.MaxPixels < 1 || c.MaxPixels > 200000000 || request.Tenant == uuid.Nil || request.Project == uuid.Nil || request.Snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(request.Key) {
		return zero, v2input.ErrInvalid
	}
	request = request.owned()
	var pixels int64
	switch c.Operation {
	case "optical.panorama":
		p, err := ParsePanoramaParams(request.Params)
		if err != nil {
			return zero, err
		}
		pixels = int64(p.MaxPixels)
	case "optical.focus_stack":
		p, err := ParseFocusStackParams(request.Params)
		if err != nil {
			return zero, err
		}
		pixels = int64(p.MaxPixels)
	default:
		return zero, v2input.ErrInvalid
	}
	if pixels > c.MaxPixels {
		return zero, v2input.ErrInvalid
	}
	required, err := MultiResources(c.Operation, request.Params, request.Sources)
	if err != nil {
		return zero, err
	}
	capacity := c.Capacity
	if capacity.PixelCount < required.PixelCount {
		return zero, v2input.ErrInvalid
	}
	capacity.PixelCount = required.PixelCount
	if err = ValidateMultiCapacity(required, capacity); err != nil {
		return zero, err
	}
	located, err := LoadMultiQualificationEvidence(ctx, SQLMultiQualificationCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, who, request.Tenant, request.Project, c.Pin, c.Locks, c.Operation, request.Params, request.Sources)
	if err != nil {
		return zero, err
	}
	qualified, err := located.Qualification()
	if err != nil {
		return zero, err
	}
	if err = VerifyMultiSourceObjects(ctx, SQLMultiSourceArrayCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, who, request.Tenant, request.Project, request.Snapshot, c.Operation, request.Params, request.Sources); err != nil {
		return zero, err
	}
	draft, err := CompileMultiDraft(ctx, who, request.Tenant, request.Project, request.Snapshot, request.Key, c.Operation, request.Params, request.Sources, qualified, capacity)
	if err != nil {
		return zero, err
	}
	snapshot, err := draft.Snapshot()
	if err != nil {
		return zero, err
	}
	return PreparedMulti{draft: draft, params: artifactjson.Ref{ID: snapshot.ParamsDocument, SHA256: snapshot.ParamsSHA}, qualification: located}, nil
}
func (c MultiCoordinator) revalidate(ctx context.Context, who auth.Identity, request MultiPrepareRequest, prepared PreparedMulti) (PreparedMulti, error) {
	current, err := c.rebuild(ctx, who, request)
	if err != nil {
		return PreparedMulti{}, err
	}
	oldObjects, err := prepared.qualification.Objects()
	if err != nil {
		return PreparedMulti{}, err
	}
	newObjects, err := current.qualification.Objects()
	if err != nil {
		return PreparedMulti{}, err
	}
	if current.draft.raw != prepared.draft.raw || current.params != prepared.params || !reflect.DeepEqual(oldObjects, newObjects) {
		return PreparedMulti{}, v2input.ErrExpired
	}
	return current, nil
}
func (c MultiCoordinator) PrepareParams(ctx context.Context, who auth.Identity, request MultiPrepareRequest) (PreparedMulti, error) {
	if c.Writer == nil {
		return PreparedMulti{}, v2input.ErrInvalid
	}
	prepared, err := c.rebuild(ctx, who, request)
	if err != nil {
		return PreparedMulti{}, err
	}
	ref, err := PublishMultiParams(ctx, c.DB, c.Writer, c.Bucket, who, request.Tenant, prepared.draft)
	if err != nil {
		return PreparedMulti{}, err
	}
	if ref != prepared.params {
		return PreparedMulti{}, v2input.ErrInvalid
	}
	return prepared, nil
}
func (c MultiCoordinator) PublishTransport(ctx context.Context, who auth.Identity, request MultiPrepareRequest, prepared PreparedMulti) (v2input.GraphTask, error) {
	current, err := c.revalidate(ctx, who, request, prepared)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	qualified, err := current.qualification.Qualification()
	if err != nil {
		return v2input.GraphTask{}, err
	}
	return PublishMultiTransport(ctx, c.DB, c.Writer, c.Bucket, who, request.Tenant, current.draft, qualified, v2input.Ref{ArtifactID: current.params.ID, SHA256: current.params.SHA256})
}
func (c MultiCoordinator) Admit(ctx context.Context, who auth.Identity, request MultiPrepareRequest, prepared PreparedMulti) error {
	current, err := c.revalidate(ctx, who, request, prepared)
	if err != nil {
		return err
	}
	qualified, err := current.qualification.Qualification()
	if err != nil {
		return err
	}
	return AdmitMultiTask(ctx, c.DB, c.Bucket, who, request.Tenant, current.draft, qualified, v2input.Ref{ArtifactID: current.params.ID, SHA256: current.params.SHA256}, current.qualification)
}
func (c MultiCoordinator) ReadQueuedReceipt(ctx context.Context, who auth.Identity, request MultiPrepareRequest) (MultiReceipt, error) {
	current, err := c.rebuild(ctx, who, request)
	if err != nil {
		return MultiReceipt{}, err
	}
	qualified, err := current.qualification.Qualification()
	if err != nil {
		return MultiReceipt{}, err
	}
	return ReadMultiQueuedReceipt(ctx, c.DB, c.Bucket, who, request.Tenant, current.draft, qualified, v2input.Ref{ArtifactID: current.params.ID, SHA256: current.params.SHA256}, current.qualification)
}
