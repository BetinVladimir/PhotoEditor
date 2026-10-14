package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type RestorationAdoptionCandidate struct {
	Scope      RestorationArrayScope
	Run        uuid.UUID
	Project    uuid.UUID
	Snapshot   uuid.UUID
	Actor      auth.Identity
	Operation  string
	Descriptor []byte
	Params     []byte
}

type RestorationAdoptionQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type SQLRestorationAdoptionQueue struct {
	DB RestorationAdoptionQuery
}

// Next discovers one canonical result. The descriptor is only a locator and
// must be reauthorized by the material loader and receipt transaction.
func (q SQLRestorationAdoptionQueue) Next(ctx context.Context,
	after uuid.UUID) (*RestorationAdoptionCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.DB == nil {
		return nil, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var tenant, task, result, root uuid.UUID
	var run, project, snapshot, actor, session uuid.UUID
	var operation, descriptor, rootSHA string
	var cursor any
	if after != uuid.Nil {
		cursor = after
	}
	err := q.DB.QueryRow(bounded, `SELECT tenant_id,run_id,task_id,project_id,
	 snapshot_id,actor_id,session_id,operation,descriptor_jcs,result_id,root_id,root_sha
	 FROM photo.next_v28_restoration_adoption($1)`, cursor).Scan(&tenant, &run,
		&task, &project, &snapshot, &actor, &session, &operation, &descriptor,
		&result, &root, &rootSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if tenant == uuid.Nil || run == uuid.Nil || task == uuid.Nil ||
		project == uuid.Nil || snapshot == uuid.Nil || actor == uuid.Nil ||
		session == uuid.Nil || result == uuid.Nil ||
		(after != uuid.Nil && bytes.Compare(task[:], after[:]) <= 0) ||
		(operation != "portrait.glare" && operation != "image.reflection") ||
		len(descriptor) < 2 || len(descriptor) > 1048576 {
		return nil, v2input.ErrInvalid
	}
	rootRef := v2input.Ref{ArtifactID: root, SHA256: rootSHA}
	if !isDocumentRef(rootRef) {
		return nil, v2input.ErrInvalid
	}
	canonicalBody, err := canonical.Bytes([]byte(descriptor))
	if err != nil || !bytes.Equal(canonicalBody, []byte(descriptor)) {
		return nil, v2input.ErrInvalid
	}
	var plan restorationTaskDescriptor
	if json.Unmarshal(canonicalBody, &plan) != nil || plan.SchemaVersion != 1 ||
		plan.Task.ID != task || plan.Task.Operation != operation ||
		plan.Task.OperationVersion != "1" || plan.Task.ResourceClass != "gpu" ||
		len(plan.Params) < 2 || len(plan.Params) > 65536 {
		return nil, v2input.ErrInvalid
	}
	params, err := canonical.Bytes(plan.Params)
	if err != nil || !bytes.Equal(params, plan.Params) {
		return nil, v2input.ErrInvalid
	}
	return &RestorationAdoptionCandidate{Scope: RestorationArrayScope{
		Tenant: tenant, Task: task, Result: result, Root: root, RootSHA: rootSHA},
		Run: run, Project: project, Snapshot: snapshot,
		Actor:     auth.Identity{UserID: actor, SessionID: session},
		Operation: operation, Descriptor: append([]byte(nil), canonicalBody...),
		Params: append([]byte(nil), params...)}, nil
}
