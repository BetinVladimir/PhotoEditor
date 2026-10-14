package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"time"
)

type SemanticAssemblyMaterial struct {
	IntentRef    Ref
	Intent       SemanticEditIntent
	Calibration  Ref
	ModelLockSHA string
	Snapshot     Ref
	SnapshotBody json.RawMessage
	Candidates   []SemanticCandidateDocument
}

// Load must resolve exact versioned documents under the lease's project/run,
// adopted producer and model/calibration authority. Compilation repeats pins.
type SemanticAssemblySource interface {
	Load(context.Context, SemanticAssemblyLease, []SemanticTransferResult) (SemanticAssemblyMaterial, error)
}
type SemanticAssemblyRepository interface {
	Claim(context.Context) (*SemanticAssemblyLease, error)
	Inputs(context.Context, SemanticAssemblyLease) ([]SemanticTransferResult, error)
	Stage(context.Context, SemanticAssemblyLease, SemanticAssemblyStage) (uuid.UUID, error)
	Renew(context.Context, SemanticAssemblyLease) (bool, error)
	Release(context.Context, SemanticAssemblyLease) (bool, error)
}
type SemanticAssemblyCommitter interface {
	CommitSemanticAssembly(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, GraphTask) error
}
type SemanticAssemblyOrchestrator struct {
	Store             SemanticAssemblyRepository
	Source            SemanticAssemblySource
	Documents         DocumentPublisher
	TransportForLease func(SemanticAssemblyLease) Publisher
	Committer         SemanticAssemblyCommitter
	RenewEvery        time.Duration
}

func (o SemanticAssemblyOrchestrator) ProcessOne(ctx context.Context) (bool, error) {
	return o.RunOnce(ctx)
}

func (o SemanticAssemblyOrchestrator) RunOnce(ctx context.Context) (processed bool, runErr error) {
	if o.Store == nil || o.Source == nil || o.Documents == nil || o.TransportForLease == nil || o.Committer == nil {
		return false, ErrInvalid
	}
	lease, err := o.Store.Claim(ctx)
	if err != nil || lease == nil {
		return false, err
	}
	complete := false
	defer func() {
		if !complete {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, _ = o.Store.Release(cleanup, *lease)
		}
	}()
	if !validSemanticAssemblyLease(*lease) {
		return false, ErrInvalid
	}
	// Long document resolution/publication needs renewal throughout IO, not
	// only between phases. Losing authority cancels ongoing source/object work.
	workCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	lost := make(chan error, 1)
	every := o.RenewEvery
	if every <= 0 || every > 20*time.Second {
		every = 20 * time.Second
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				renewalCtx, cancel := context.WithTimeout(workCtx, 5*time.Second)
				ok, err := o.Store.Renew(renewalCtx, *lease)
				cancel()
				if err != nil || !ok {
					if err == nil {
						err = ErrExpired
					}
					lost <- err
					stop()
					return
				}
			}
		}
	}()
	defer func() {
		stop()
		<-done
		select {
		case err := <-lost:
			if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				runErr = err
			}
		default:
		}
	}()
	ctx = workCtx
	renew := func() error {
		ok, err := o.Store.Renew(ctx, *lease)
		if err != nil {
			return err
		}
		if !ok {
			return ErrExpired
		}
		return nil
	}
	results, err := o.Store.Inputs(ctx, *lease)
	if err != nil {
		return false, err
	}
	if len(results) < 1 || len(results) > 5000 {
		return false, ErrInvalid
	}
	results = append([]SemanticTransferResult(nil), results...)
	refs := make([]Ref, len(results))
	seen := map[uuid.UUID]bool{}
	for i, item := range results {
		task, e := TaskID(lease.WorkflowID, "semantic.transfer", i)
		if e != nil || item.Ordinal != i || item.TaskID != task || !validRef(item.Candidate) || seen[item.Candidate.ArtifactID] {
			return false, ErrInvalid
		}
		seen[item.Candidate.ArtifactID] = true
		refs[i] = item.Candidate
	}
	material, err := o.Source.Load(ctx, *lease, append([]SemanticTransferResult(nil), results...))
	if err != nil {
		return false, err
	}
	params := SemanticAssembleParams{SchemaVersion: 1, Intent: material.IntentRef, Candidates: refs, ModelLockSHA: material.ModelLockSHA, Calibration: material.Calibration}
	profile, _ := CompileSemanticAssemblyProfile(256<<20, 32<<20, 8<<20)
	sha, err := canonical.Hash(params)
	if err != nil {
		return false, err
	}
	// Preflight all typed candidate/snapshot bytes before document publication.
	provisional := Ref{ArtifactID: uuid.NewSHA1(lease.ExpansionID, []byte("v27.assembly.params.preflight")), SHA256: sha}
	if _, err := CompileSemanticAssemblyTask(lease.WorkflowID, params, provisional, material.Intent, material.Snapshot, material.SnapshotBody, material.Candidates, profile); err != nil {
		return false, err
	}
	if err := renew(); err != nil {
		return false, err
	}
	paramsRef, err := PublishSemanticAssemblyParams(ctx, o.Documents, *lease, params)
	if err != nil {
		return false, err
	}
	frozen, err := CompileSemanticAssemblyTask(lease.WorkflowID, params, paramsRef, material.Intent, material.Snapshot, material.SnapshotBody, material.Candidates, profile)
	if err != nil {
		return false, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return false, err
	}
	if err := renew(); err != nil {
		return false, err
	}
	task, err := o.Store.Stage(ctx, *lease, SemanticAssemblyStage{Params: paramsRef, InputDigest: draft.InputDigest, Transfers: results, Profile: profile})
	if err != nil {
		return false, err
	}
	if task != draft.ID {
		return false, ErrInvalid
	}
	transport := o.TransportForLease(*lease)
	if transport == nil {
		return false, ErrInvalid
	}
	prepared, err := PublishSemanticAssemblyTask(ctx, frozen, transport)
	if err != nil {
		return false, err
	}
	if err := renew(); err != nil {
		return false, err
	}
	if err := o.Committer.CommitSemanticAssembly(ctx, lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token, prepared.Graph); err != nil {
		return false, err
	}
	complete = true
	return true, nil
}
