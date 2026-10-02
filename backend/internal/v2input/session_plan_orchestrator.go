package v2input

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/google/uuid"
)

// FrozenSessionPlan is read from immutable workflow configuration, never from
// the current mutable session or collection. The source must return the same
// values for a retried expansion.
type FrozenSessionPlan struct {
	ItemCount      int
	Policy         any
	Context        Ref
	SnapshotDigest string
	PolicySHA      string
}

type SessionPlanClosure struct {
	Open        CursorFactory
	Direct      []Ref
	InputDigest string
	Profile     json.RawMessage
}

type SessionPlanSource interface {
	Load(context.Context, SessionPlanLease) (FrozenSessionPlan, error)
	Closure(context.Context, SessionPlanLease, []SessionShardResult, Ref) (SessionPlanClosure, error)
}

type SessionPlanRepository interface {
	Claim(context.Context) (*SessionPlanLease, error)
	Inputs(context.Context, SessionPlanLease) ([]SessionShardResult, error)
	Stage(context.Context, SessionPlanLease, SessionPlanStage) (uuid.UUID, error)
	Renew(context.Context, SessionPlanLease) (bool, error)
	Release(context.Context, SessionPlanLease) (bool, error)
}

type SessionPlanCommitter interface {
	CommitSessionPlan(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, GraphTask) error
}

// SessionPlanOrchestrator performs object publication strictly between DB
// calls. Successful CommitSessionPlan is the only path that closes the barrier;
// failure releases the fenced claim for retry with the same immutable docs.
type SessionPlanOrchestrator struct {
	Store             SessionPlanRepository
	Source            SessionPlanSource
	Documents         DocumentPublisher
	Transport         Publisher
	TransportForLease func(SessionPlanLease) Publisher
	Committer         SessionPlanCommitter
}

// ProcessOne lets the scheduler run this expansion in its independent loop.
func (o SessionPlanOrchestrator) ProcessOne(ctx context.Context) (bool, error) {
	return o.RunOnce(ctx)
}

func (o SessionPlanOrchestrator) RunOnce(ctx context.Context) (bool, error) {
	if o.Store == nil || o.Source == nil || o.Documents == nil ||
		(o.Transport == nil && o.TransportForLease == nil) || o.Committer == nil {
		return false, ErrInvalid
	}
	lease, err := o.Store.Claim(ctx)
	if err != nil || lease == nil {
		return false, err
	}
	completed := false
	defer func() {
		if !completed {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, _ = o.Store.Release(releaseCtx, *lease)
		}
	}()
	if !validSessionPlanLease(*lease) {
		return false, ErrInvalid
	}
	transport := o.Transport
	if o.TransportForLease != nil {
		transport = o.TransportForLease(*lease)
	}
	if transport == nil {
		return false, ErrInvalid
	}
	results, err := o.Store.Inputs(ctx, *lease)
	if err != nil {
		return false, err
	}
	frozen, err := o.Source.Load(ctx, *lease)
	if err != nil {
		return false, err
	}
	descriptors, err := CompileSessionDescriptors(lease.WorkflowID, frozen.ItemCount)
	if err != nil || len(descriptors.Tasks) != len(results) ||
		!validRef(frozen.Context) || frozen.Policy == nil {
		return false, ErrInvalid
	}
	if ok, err := o.Store.Renew(ctx, *lease); err != nil || !ok {
		if err != nil {
			return false, err
		}
		return false, ErrExpired
	}
	params, err := PublishSessionPlanParams(ctx, o.Documents, *lease,
		descriptors, results, frozen.Policy, frozen.Context)
	if err != nil {
		return false, err
	}
	closure, err := o.Source.Closure(ctx, *lease, results, params)
	if err != nil || closure.Open == nil || !digestPattern.MatchString(closure.InputDigest) {
		return false, ErrInvalid
	}
	if ok, err := o.Store.Renew(ctx, *lease); err != nil || !ok {
		if err != nil {
			return false, err
		}
		return false, ErrExpired
	}
	task, err := o.Store.Stage(ctx, *lease, SessionPlanStage{
		Params: params, InputDigest: closure.InputDigest,
		Shards: results, Profile: closure.Profile})
	if err != nil {
		return false, err
	}
	expectedTask, err := TaskID(lease.WorkflowID, "session.plan", 0)
	if err != nil || task != expectedTask {
		return false, ErrInvalid
	}
	cursor, err := closure.Open(ctx)
	if err != nil || cursor == nil {
		return false, ErrInvalid
	}
	built, buildErr := BuildInputClosure(ctx, task, cursor, transport)
	if closer, ok := cursor.(io.Closer); ok {
		_ = closer.Close()
	}
	if buildErr != nil {
		return false, buildErr
	}
	if ok, err := o.Store.Renew(ctx, *lease); err != nil || !ok {
		if err != nil {
			return false, err
		}
		return false, ErrExpired
	}
	if err := o.Committer.CommitSessionPlan(ctx, lease.TenantID, lease.RunID,
		lease.ExpansionID, lease.Token, GraphTask{
			ID: task, Built: built, Open: closure.Open, Direct: closure.Direct,
		}); err != nil {
		return false, err
	}
	completed = true
	return true, nil
}
