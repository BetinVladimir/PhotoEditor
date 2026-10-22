package gateway

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type TaskContext struct {
	Identity         TaskIdentity
	LeaseUntil       time.Time
	Deadline         time.Time
	Operation        string
	ExecutionProfile json.RawMessage
}

type Authorizer struct{ DB *pgxpool.Pool }

// Authorize is required both when minting a capability and for every object
// request. A token's cryptographic validity alone is never enough to access data.
func (a Authorizer) Authorize(ctx context.Context, expected TaskIdentity, certificate *x509.Certificate) (TaskContext, error) {
	if !expected.valid() {
		return TaskContext{}, ErrCapability
	}
	task, err := a.Resolve(ctx, expected.TaskID, expected.Attempt, expected.Fence, expected.WorkerID, certificate)
	if err != nil {
		return TaskContext{}, err
	}
	if task.Identity != expected {
		return TaskContext{}, ErrCapability
	}
	return task, nil
}

// Resolve derives tenant/run from the canonical DB task during token issuance;
// clients cannot choose those fields in a credentials request.
func (a Authorizer) Resolve(ctx context.Context, taskID uuid.UUID, attempt int, fence int64, workerID uuid.UUID, certificate *x509.Certificate) (TaskContext, error) {
	var task TaskContext
	if err := ctx.Err(); err != nil {
		return task, err
	}
	if a.DB == nil {
		return task, ErrCapability
	}
	if taskID == uuid.Nil || workerID == uuid.Nil || attempt < 1 || attempt > 6 || fence < 1 || certificate == nil || certificate.IsCA || len(certificate.Raw) == 0 {
		return task, ErrCapability
	}
	task, err := retryGatewayAuthorization(ctx, func(queryCtx context.Context) (TaskContext, error) {
		var current TaskContext
		err := a.DB.QueryRow(queryCtx, `SELECT tenant_id,run_id,task_id,worker_id,attempt,fence,lease_until,deadline,operation,execution_profile
 FROM photo.authorize_gateway_task($1,$2,$3,$4,$5)`, taskID, attempt, fence, workerID, certificateHash(certificate)).Scan(
			&current.Identity.TenantID, &current.Identity.RunID, &current.Identity.TaskID, &current.Identity.WorkerID, &current.Identity.Attempt, &current.Identity.Fence, &current.LeaseUntil, &current.Deadline, &current.Operation, &current.ExecutionProfile)
		return current, err
	})
	if cancellation := ctx.Err(); cancellation != nil {
		return TaskContext{}, cancellation
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskContext{}, ErrCapability
	}
	if err != nil {
		return TaskContext{}, err
	}
	if task.Identity.TaskID != taskID || task.Identity.WorkerID != workerID || task.Identity.Attempt != attempt || task.Identity.Fence != fence {
		return TaskContext{}, ErrCapability
	}
	if !time.Now().Before(task.LeaseUntil) || !time.Now().Before(task.Deadline) {
		return TaskContext{}, ErrCapability
	}
	return task, nil
}
