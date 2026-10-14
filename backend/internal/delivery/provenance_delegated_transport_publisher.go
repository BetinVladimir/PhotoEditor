package delivery

import (
	"bytes"
	"context"
	"sync"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// DelegatedSignerInputPublisher publishes only the one exact chunk and its
// manifest. The SQL repository fences each document against the parent lease.
type DelegatedSignerInputPublisher struct {
	mu sync.Mutex
	store SQLDelegatedLineageStore
	lineageRef v2input.Ref
	lineageBody []byte
	params []byte
	task v2input.DraftTask
	entries []v2input.Entry
	objects artifactjson.Objects
	chunk v2input.Ref
}

func NewDelegatedSignerInputPublisher(ctx context.Context,
	store SQLDelegatedLineageStore, lineageRef v2input.Ref, lineageBody []byte,
	intent provenance.FrozenSigningIntent, task provenance.FrozenSigningTask,
	objects artifactjson.Objects) (*DelegatedSignerInputPublisher, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	if objects == nil || store.DB == nil { return nil, v2input.ErrInvalid }
	docs, err := BuildDelegatedSigningDraftDocuments(ctx, store.Scope, intent, task)
	if err != nil { return nil, err }
	if _, err := store.scopePins(lineageRef, lineageBody); err != nil { return nil, err }
	if err := store.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
		return nil, err
	}
	return &DelegatedSignerInputPublisher{store: store, lineageRef: lineageRef,
		lineageBody: append([]byte(nil), lineageBody...),
		params: docs.Params, task: task.Task(), entries: task.Inputs(),
		objects: objects}, nil
}

func (p *DelegatedSignerInputPublisher) Publish(ctx context.Context,
	taskID uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil { return zero, err }
	if p == nil { return zero, v2input.ErrInvalid }
	p.mu.Lock()
	defer p.mu.Unlock()
	if taskID != p.task.ID || len(body) < 2 || len(body) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	var purpose string
	var expected []byte
	var err error
	switch {
	case ordinal == 0 && contract == "InputClosureChunk":
		purpose = "v2_input_chunk_0"
		expected, err = canonical.Marshal(v2input.Chunk{
			SchemaVersion: 1, Entries: p.entries})
	case ordinal == 1 && contract == "InputClosureManifest" &&
		p.chunk.ArtifactID != uuid.Nil:
		purpose = "v2_input_manifest"
		expected, err = canonical.Marshal(v2input.Manifest{
			SchemaVersion: 1, TaskID: p.task.ID, EntryCount: len(p.entries),
			EntriesSHA: p.task.InputDigest, Chunks: []v2input.Ref{p.chunk}})
	default:
		return zero, v2input.ErrInvalid
	}
	if err != nil || !bytes.Equal(expected, body) {
		return zero, v2input.ErrInvalid
	}
	if err := p.store.VerifyReady(ctx, p.lineageRef, p.lineageBody); err != nil {
		return zero, err
	}
	repository := delegatedSignerTransportRepository{publisher: p,
		purpose: purpose, body: append([]byte(nil), body...)}
	scope := artifactjson.Scope{Tenant: p.store.Scope.TenantID,
		Run: uuid.NewSHA1(p.store.Scope.WorkflowID, []byte("run")),
		Owner: p.store.Scope.TaskID, Purpose: purpose}
	result, err := (artifactjson.Publisher{Repository: repository,
		Objects: p.objects}).Publish(ctx, scope, body)
	if err != nil { return zero, err }
	if err := p.store.VerifyReady(ctx, p.lineageRef, p.lineageBody); err != nil {
		return zero, err
	}
	ref := v2input.Ref{ArtifactID: result.ID, SHA256: result.SHA256}
	if purpose == "v2_input_chunk_0" { p.chunk = ref }
	return ref, nil
}

type delegatedSignerTransportRepository struct {
	publisher *DelegatedSignerInputPublisher
	purpose string
	body []byte
}

func (r delegatedSignerTransportRepository) inputs(ctx context.Context,
	scope artifactjson.Scope) ([]byte, []byte, error) {
	p := r.publisher
	if err := ctx.Err(); err != nil { return nil, nil, err }
	if p == nil || p.store.DB == nil || scope.Tenant != p.store.Scope.TenantID ||
		scope.Run != uuid.NewSHA1(p.store.Scope.WorkflowID, []byte("run")) ||
		scope.Owner != p.store.Scope.TaskID || scope.Purpose != r.purpose ||
		scope.AssemblyLease != uuid.Nil || scope.LocalPortrait || scope.DonorPortrait ||
		(r.purpose != "v2_input_chunk_0" && r.purpose != "v2_input_manifest") ||
		len(r.body) < 2 || len(r.body) > 1048576 || len(p.store.Bucket) < 3 ||
		len(p.store.Bucket) > 63 {
		return nil, nil, v2input.ErrInvalid
	}
	pins, err := p.store.scopePins(p.lineageRef, p.lineageBody)
	if err != nil { return nil, nil, err }
	taskBody, err := canonical.Marshal(p.task)
	if err != nil { return nil, nil, err }
	return pins, taskBody, nil
}

func (r delegatedSignerTransportRepository) Reserve(ctx context.Context,
	scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var zero artifactjson.Reservation
	pins, taskBody, err := r.inputs(ctx, scope)
	if err != nil { return zero, err }
	if sha != digest(r.body) || size != int64(len(r.body)) {
		return zero, v2input.ErrInvalid
	}
	p := r.publisher
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = p.store.DB.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready
	 FROM photo.reserve_v31_delivery_signer_transport(
	 $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb,$10,$11,$12)`,
		p.store.Lease.Expansion, p.store.Lease.Token, p.store.Slot.Ordinal,
		p.store.Slot.PresetID, string(pins), int64(len(p.lineageBody)),
		string(p.params), p.store.Bucket, string(taskBody), r.purpose,
		sha, size).Scan(&outcome, &id, &token, &ready)
	if err != nil { return zero, err }
	switch outcome {
	case "quota": return zero, artifactjson.ErrQuota
	case "conflict": return zero, artifactjson.ErrConflict
	case "created", "resumed", "replayed":
	default: return zero, artifactjson.ErrStale
	}
	if id == nil || *id == uuid.Nil || token == nil || *token == uuid.Nil ||
		ready != (outcome == "replayed") { return zero, objectstore.ErrIntegrity }
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready { result.Ready = &artifactjson.Ref{ID: *id, SHA256: sha} }
	return result, nil
}

func (r delegatedSignerTransportRepository) Complete(ctx context.Context,
	scope artifactjson.Scope, reservation artifactjson.Reservation,
	object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	pins, taskBody, err := r.inputs(ctx, scope)
	if err != nil { return zero, err }
	key, err := artifactjson.DocumentKey(scope, reservation.ArtifactID)
	if err != nil || reservation.Token == uuid.Nil || reservation.Ready != nil ||
		object.Key != key || object.SHA256 != digest(r.body) ||
		object.Bytes != int64(len(r.body)) ||
		object.ContentType != "application/json" || object.VersionID == "" ||
		object.VersionID == "null" || len(object.VersionID) > 1024 {
		return zero, objectstore.ErrIntegrity
	}
	p := r.publisher
	var accepted bool
	err = p.store.DB.QueryRow(ctx,`SELECT photo.complete_v31_delivery_signer_transport(
	 $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14,$15)`,
		p.store.Lease.Expansion, p.store.Lease.Token, p.store.Slot.Ordinal,
		p.store.Slot.PresetID, string(pins), int64(len(p.lineageBody)),
		string(p.params), p.store.Bucket, string(taskBody), r.purpose,
		reservation.ArtifactID, reservation.Token, object.SHA256,
		object.Bytes, object.VersionID).Scan(&accepted)
	if err != nil { return zero, err }
	if !accepted { return zero, artifactjson.ErrStale }
	return artifactjson.Ref{ID: reservation.ArtifactID,
		SHA256: object.SHA256}, nil
}

var _ v2input.Publisher = (*DelegatedSignerInputPublisher)(nil)
var _ artifactjson.Repository = delegatedSignerTransportRepository{}

// PublishCurrentDelegatedSignerInputs installs the lease-fenced document
// publisher and builds the exact transport for later transactional registration.
func PublishCurrentDelegatedSignerInputs(ctx context.Context,
	store SQLDelegatedLineageStore, lineageRef v2input.Ref, lineageBody []byte,
	intent provenance.FrozenSigningIntent, task provenance.FrozenSigningTask,
	objects artifactjson.Objects) (DelegatedSignerTransport, error) {
	publisher, err := NewDelegatedSignerInputPublisher(ctx, store, lineageRef,
		lineageBody, intent, task, objects)
	if err != nil { return DelegatedSignerTransport{}, err }
	return PublishDelegatedSignerInputs(ctx, store, lineageRef, lineageBody,
		intent, task, publisher)
}
