package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// LocalPortraitOwnerStore owns database transactions; publishing always occurs
// between transactions. Its production implementation is SQLLocalPortraitOwnerScope.
type LocalPortraitOwnerStore interface {
	Check(context.Context, []byte) error
	Begin(context.Context, []byte, string, int64) error
	LocalPortraitTaskStager
	Admit(context.Context, uuid.UUID, uuid.UUID, int, FrozenLocalPortraitTask, v2input.GraphTask) error
}

// LocalPortraitCoordinator composes durable intent, scoped publication and
// transactional admission. Failure preserves the draft for retry or cleanup.
// Physical raster verification remains mandatory in the worker execution path.
type LocalPortraitCoordinator struct {
	Resolver  ClosureResolver
	Owners    LocalPortraitOwnerStore
	Documents v2input.DocumentPublisher
}

type LocalPortraitAdmission struct {
	Run      uuid.UUID
	Workflow uuid.UUID
	Task     uuid.UUID
}

// Submit revalidates a canonical owner request with current caller-scoped
// evidence before any mutation. Retrying uses the same run and immutable intent;
// store/publisher implementations enforce exact replay and live authority.
func (c LocalPortraitCoordinator) Submit(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	requestRaw, sourceRaw []byte, proof LocalPortraitRecoveryProof, runtimeSHA string,
	capacity v2portrait.LocalResourceEstimate, policySHA string, reservedBytes int64) (LocalPortraitAdmission, error) {
	return c.execute(ctx, who, tenant, requestRaw, nil, sourceRaw, proof, runtimeSHA, capacity, policySHA, reservedBytes)
}

// Recover reuses the published Params in a staged document plan. It repeats
// current scoped checks and never treats saved inventory as authorization.
func (c LocalPortraitCoordinator) Recover(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	requestRaw, savedPlan, sourceRaw []byte, proof LocalPortraitRecoveryProof, runtimeSHA string,
	capacity v2portrait.LocalResourceEstimate, policySHA string, reservedBytes int64) (LocalPortraitAdmission, error) {
	if len(savedPlan) < 2 || len(savedPlan) > 1048576 {
		return LocalPortraitAdmission{}, v2input.ErrInvalid
	}
	return c.execute(ctx, who, tenant, requestRaw, append([]byte(nil), savedPlan...), sourceRaw, proof, runtimeSHA, capacity, policySHA, reservedBytes)
}

func (c LocalPortraitCoordinator) execute(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	requestRaw, savedPlan, sourceRaw []byte, proof LocalPortraitRecoveryProof, runtimeSHA string,
	capacity v2portrait.LocalResourceEstimate, policySHA string, reservedBytes int64) (LocalPortraitAdmission, error) {
	var zero LocalPortraitAdmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.Owners == nil || c.Documents == nil || c.Resolver.Catalog == nil || c.Resolver.Objects == nil || tenant == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	requestRaw = append([]byte(nil), requestRaw...)
	sourceRaw = append([]byte(nil), sourceRaw...)
	proof.RootRaw = append([]byte(nil), proof.RootRaw...)
	proof.OriginalRaw = append([]byte(nil), proof.OriginalRaw...)
	proof.VectorsRaw = append([]byte(nil), proof.VectorsRaw...)
	// Validate the full reservation before the first owner transaction.
	if _, err := localPortraitOwnerConfig(requestRaw, policySHA, reservedBytes); err != nil {
		return zero, err
	}
	var request LocalPortraitOwnerRequest
	if json.Unmarshal(requestRaw, &request) != nil || request.Tenant != tenant || request.Actor != who.UserID || request.Session != who.SessionID {
		return zero, v2input.ErrInvalid
	}
	fresh, _, err := c.Resolver.CompileLocalPortraitOwnerRequest(ctx, who, tenant, request.Project, request.Snapshot, request.Run, request.Ordinal,
		request.Intent.Operation, request.Intent.Binding, sourceRaw, []byte(request.Intent.ParamsJCS), runtimeSHA, capacity, proof)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(fresh, requestRaw) {
		return zero, v2input.ErrInvalid
	}
	var frozen FrozenLocalPortraitTask
	if savedPlan != nil {
		restored, e := c.Resolver.RestoreVerifiedLocalPortraitTask(ctx, who, tenant, request.Workflow, request.Ordinal, savedPlan, sourceRaw, runtimeSHA, capacity, proof)
		if e != nil {
			return zero, e
		}
		plan, e := v2portrait.CompileLocalTaskPlan(ctx, restored.draft.Operation, restored.binding, sourceRaw, restored.params, runtimeSHA, capacity)
		if e != nil {
			return zero, e
		}
		prepared := PreparedLocalPortraitTask{Published: v2portrait.PublishedLocalTaskPlan{ParamsRef: v2input.Ref{ArtifactID: restored.draft.ParamsArtifactID, SHA256: restored.draft.ParamsSHA}, Plan: plan}, Evidence: TaskInputEvidence{Entries: restored.entries}}
		frozen, e = FreezePublishedLocalPortraitIntent(ctx, request.Workflow, request.Ordinal, request.Intent, prepared, sourceRaw, runtimeSHA, capacity)
		if e != nil {
			return zero, e
		}
	}
	if err = c.Owners.Check(ctx, append([]byte(nil), requestRaw...)); err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if err = c.Owners.Begin(ctx, append([]byte(nil), requestRaw...), policySHA, reservedBytes); err != nil {
		return zero, err
	}
	if savedPlan == nil {
		published, err := PublishLocalPortraitOwnerParams(ctx, c.Documents, requestRaw, sourceRaw, runtimeSHA, capacity)
		if err != nil {
			return zero, err
		}
		var masks struct {
			Mask      v2input.Ref `json:"mask"`
			Protected v2input.Ref `json:"protected"`
		}
		if json.Unmarshal(published.Plan.Params, &masks) != nil {
			return zero, v2input.ErrInvalid
		}
		evidence, err := c.Resolver.ResolveLocalPortraitTaskInputEvidence(ctx, who, tenant, request.Intent.Binding,
			published.ParamsRef, published.Plan.Params, sourceRaw, proof.Root, proof.RootRaw, proof.OriginalParams, proof.OriginalRaw, proof.VectorsRaw,
			masks.Mask, masks.Protected)
		if err != nil {
			return zero, fmt.Errorf("resolve local portrait inputs: %w", err)
		}
		frozen, err = FreezePublishedLocalPortraitIntent(ctx, request.Workflow, request.Ordinal, request.Intent,
			PreparedLocalPortraitTask{Published: published, Evidence: evidence}, sourceRaw, runtimeSHA, capacity)
		if err != nil {
			return zero, fmt.Errorf("freeze local portrait intent: %w", err)
		}
	}
	graph, err := PublishStagedLocalPortraitInputs(ctx, c.Owners, c.Documents, tenant, request.Run, request.Ordinal, frozen)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if err = c.Owners.Admit(ctx, tenant, request.Run, request.Ordinal, frozen, graph); err != nil {
		return zero, err
	}
	// A successful committed admission is returned even if cancellation races
	// afterward; callers can discover the durable result by the stable run ID.
	return LocalPortraitAdmission{Run: request.Run, Workflow: request.Workflow, Task: request.Intent.TaskID}, nil
}

var _ LocalPortraitOwnerStore = SQLLocalPortraitOwnerScope{}
