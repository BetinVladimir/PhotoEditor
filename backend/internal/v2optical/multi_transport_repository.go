package v2optical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
	"time"
)

type multiTransportRepository struct {
	db     PerspectiveTransactions
	bucket string
	who    auth.Identity
	draft  MultiDraft
	body   string
}

func (r multiTransportRepository) proof(scope artifactjson.Scope, sha string, size int64) (string, error) {
	s, err := r.draft.Snapshot()
	if err != nil {
		return "", err
	}

	if scope.Tenant != s.Tenant || scope.Run != s.Run || scope.Owner != s.Task || scope.AssemblyLease != uuid.Nil || scope.LocalPortrait || scope.DonorPortrait || size != int64(len(r.body)) || size < 2 || size > 1048576 || !validMultiTransportPurpose(scope.Purpose) {
		return "", v2input.ErrInvalid
	}
	sum := sha256.Sum256([]byte(r.body))
	if sha != hex.EncodeToString(sum[:]) {
		return "", v2input.ErrInvalid
	}
	paramsScope := artifactjson.Scope{Tenant: scope.Tenant, Run: scope.Run, Owner: scope.Owner, Purpose: "task_params"}
	return (multiParamsRepository{db: r.db, bucket: r.bucket, tenant: scope.Tenant, who: r.who, plan: r.draft}).proof(paramsScope, s.ParamsSHA, int64(len(s.ParamsJCS)))
}

func (r multiTransportRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var zero artifactjson.Reservation
	proof, err := r.proof(scope, sha, size)
	if err != nil {
		return zero, err
	}
	if r.db == nil {
		return zero, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = (multiParamsRepository{}).authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_multi_transport_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.bucket, proof, r.body).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return zero, err
	}
	switch outcome {
	case "quota":
		return zero, artifactjson.ErrQuota
	case "conflict":
		return zero, artifactjson.ErrConflict
	case "forbidden", "stale":
		return zero, artifactjson.ErrStale
	case "created", "resumed", "replayed":
	default:
		return zero, objectstore.ErrIntegrity
	}
	if id == nil || *id == uuid.Nil || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return zero, objectstore.ErrIntegrity
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

func (r multiTransportRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	proof, err := r.proof(scope, object.SHA256, object.Bytes)
	if err != nil {
		return zero, err
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key || ticket.Token == uuid.Nil || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || r.db == nil {
		return zero, objectstore.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = (multiParamsRepository{}).authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var done bool
	err = tx.QueryRow(ctx, "SELECT photo.complete_v29_multi_transport_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.bucket, object.VersionID, proof, r.body).Scan(&done)
	if err != nil {
		return zero, err
	}
	if !done {
		return zero, artifactjson.ErrStale
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}

type multiTransportDocuments struct {
	db      PerspectiveTransactions
	bucket  string
	who     auth.Identity
	draft   MultiDraft
	objects artifactjson.Objects
}

func (p multiTransportDocuments) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	repository := multiTransportRepository{db: p.db, bucket: p.bucket, who: p.who, draft: p.draft, body: string(body)}
	return (artifactjson.Publisher{Repository: repository, Objects: p.objects}).Publish(ctx, scope, body)
}

// At most 3933 entries produce eight bounded chunks. Reject noncanonical ordinals.
func validMultiTransportPurpose(purpose string) bool {
	if purpose == "v2_input_manifest" {
		return true
	}
	value, ok := strings.CutPrefix(purpose, "v2_input_chunk_")
	if !ok {
		return false
	}
	ordinal, err := strconv.Atoi(value)
	return err == nil && ordinal >= 0 && ordinal < 8 && value == strconv.Itoa(ordinal)
}

// PublishMultiTransport stages a blocked task before any transport object IO.
// Every reserve/complete repeats the current SQL draft and source authority.
func PublishMultiTransport(ctx context.Context, db PerspectiveTransactions, objects artifactjson.Objects,
	bucket string, who auth.Identity, tenant uuid.UUID, draft MultiDraft,
	qualified VerifiedMultiQualification, params v2input.Ref) (v2input.GraphTask, error) {
	if objects == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	if err := StageMultiTask(ctx, db, bucket, who, tenant, draft, qualified, params); err != nil {
		return v2input.GraphTask{}, err
	}
	snapshot, err := draft.Snapshot()
	if err != nil {
		return v2input.GraphTask{}, err
	}
	originals := make([][]byte, len(snapshot.SourcesJCS))
	for i, source := range snapshot.SourcesJCS {
		originals[i] = []byte(source)
	}
	var profile struct {
		Resources MultiResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(snapshot.Profile, &profile) != nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	plan, err := CompileMultiTaskPlan(ctx, snapshot.Operation, []byte(snapshot.ParamsJCS), originals, qualified, profile.Resources)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	documents := multiTransportDocuments{db: db, bucket: bucket, who: who, draft: draft, objects: objects}
	publisher := v2input.ScopedPublisher{Documents: documents, Tenant: tenant, Run: snapshot.Run}
	return BuildMultiTaskTransport(ctx, snapshot.Task, plan, originals, qualified, params, publisher)
}
