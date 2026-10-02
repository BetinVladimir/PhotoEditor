package v2input

import (
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
)

// SessionAnalysisDraft is built from the frozen V2 snapshot and resolved
// WorkingImage manifests. The caller has already committed the V22 draft run;
// this value must be reconstructed identically on every pending retry.
type SessionAnalysisDraft struct {
	Tenant, Run, Workflow uuid.UUID
	Items                 []SessionSnapshotItem
	SnapshotDigest        string
	ContextBody           []byte
	ContextSHA            string
	Calibration           Ref
	ModelLockSHA          string
	PolicySHA             string
	Profiles              []json.RawMessage
}

type SessionAnalysisImageSource interface {
	Load(context.Context, uuid.UUID, uuid.UUID,
		[]SessionSnapshotItem) ([]SessionDescribeImageInputs, error)
}

type SessionAnalysisStage interface {
	Stage(context.Context, uuid.UUID, uuid.UUID, uuid.UUID,
		SessionDescriptorPlan, []AnalysisDescribeStageTask) error
}

type SessionAnalysisCommitter interface {
	CommitGraph(context.Context, uuid.UUID, uuid.UUID, []GraphTask) error
}

// SessionAnalysisOrchestrator publishes immutable objects between database
// calls. A failed commit leaves the draft and published documents available
// for the same fenced retry; no worker can see an unsealed task.
type SessionAnalysisOrchestrator struct {
	Documents         DocumentPublisher
	Transport         Publisher
	TransportForDraft func(SessionAnalysisDraft) Publisher
	Images            SessionAnalysisImageSource
	Consents          SessionAnalysisConsentPinner
	Stage             SessionAnalysisStage
	Committer         SessionAnalysisCommitter
}

func (o SessionAnalysisOrchestrator) AdmitPending(ctx context.Context,
	draft SessionAnalysisDraft) (JobTicket, error) {
	transport := o.Transport
	if o.TransportForDraft != nil {
		transport = o.TransportForDraft(draft)
	}
	if o.Documents == nil || transport == nil || o.Images == nil ||
		o.Consents == nil || o.Stage == nil ||
		o.Committer == nil || draft.Tenant == uuid.Nil || draft.Run == uuid.Nil ||
		draft.Workflow == uuid.Nil || len(draft.Items) < 1 || len(draft.Items) > 5000 ||
		!validRef(draft.Calibration) ||
		!digestPattern.MatchString(draft.ModelLockSHA) ||
		!digestPattern.MatchString(draft.PolicySHA) {
		return JobTicket{}, ErrInvalid
	}
	expectedWorkflow, err := WorkflowID(draft.Run)
	if err != nil || expectedWorkflow != draft.Workflow {
		return JobTicket{}, ErrInvalid
	}
	plan, err := CompileSessionDescriptors(draft.Workflow, len(draft.Items))
	if err != nil || len(draft.Profiles) != len(plan.Tasks) {
		return JobTicket{}, ErrInvalid
	}
	imageInputs, err := o.Images.Load(ctx, draft.Tenant, draft.Run, draft.Items)
	if err != nil || len(imageInputs) != len(draft.Items) {
		if err != nil {
			return JobTicket{}, err
		}
		return JobTicket{}, ErrInvalid
	}
	for i, input := range imageInputs {
		if input.Ordinal != i || !validRef(input.Pixels) ||
			(input.Alpha != nil && !validRef(*input.Alpha)) {
			return JobTicket{}, ErrInvalid
		}
	}
	for _, profile := range draft.Profiles {
		if !validSessionDescribeProfile(profile) {
			return JobTicket{}, ErrInvalid
		}
	}
	snapshot, _, err := CompileSessionInputSnapshot(draft.Run, draft.Items)
	if err != nil || snapshot.SnapshotDigest != draft.SnapshotDigest {
		return JobTicket{}, ErrInvalid
	}
	consentsBody, err := o.Consents.Pin(ctx, draft.Tenant, draft.Run, snapshot)
	if err != nil {
		return JobTicket{}, err
	}
	roots, err := PublishSessionAnalysisRoots(ctx, o.Documents, draft.Tenant,
		draft.Run, draft.Items, draft.SnapshotDigest,
		draft.ContextBody, draft.ContextSHA, consentsBody)
	if err != nil {
		return JobTicket{}, err
	}
	if snapshot.SnapshotDigest != roots.SnapshotDigest {
		return JobTicket{}, ErrInvalid
	}
	closures := make([]SessionDescribeClosure, len(plan.Tasks))
	stage := make([]AnalysisDescribeStageTask, len(plan.Tasks))
	for ordinal, task := range plan.Tasks {
		params, err := PublishSessionDescribeParams(ctx, o.Documents,
			draft.Tenant, draft.Run, ordinal, task, roots.Snapshot,
			draft.Calibration, roots.Consents, draft.ModelLockSHA)
		if err != nil {
			return JobTicket{}, err
		}
		start := task.Ordinals[0]
		end := start + len(task.Ordinals)
		closure, err := CompileSessionDescribeClosure(draft.Workflow, draft.Run,
			task, snapshot, imageInputs[start:end], roots.Snapshot,
			roots.Consents, draft.Calibration, params, draft.PolicySHA,
			draft.Profiles[ordinal])
		if err != nil {
			return JobTicket{}, err
		}
		closures[ordinal] = closure
		stage[ordinal] = AnalysisDescribeStageTask{Params: params,
			InputDigest: closure.InputDigest, Profile: closure.Profile}
	}
	if err := o.Stage.Stage(ctx, draft.Tenant, draft.Run, draft.Workflow,
		plan, stage); err != nil {
		return JobTicket{}, err
	}
	graph := make([]GraphTask, len(plan.Tasks))
	for i, task := range plan.Tasks {
		cursor, err := closures[i].Open(ctx)
		if err != nil || cursor == nil {
			return JobTicket{}, ErrInvalid
		}
		built, buildErr := BuildInputClosure(ctx, task.ID, cursor, transport)
		if closer, ok := cursor.(io.Closer); ok {
			_ = closer.Close()
		}
		if buildErr != nil {
			return JobTicket{}, buildErr
		}
		graph[i] = GraphTask{ID: task.ID, Built: built,
			Open: closures[i].Open, Direct: closures[i].Direct}
	}
	if err := o.Committer.CommitGraph(ctx, draft.Tenant, draft.Run, graph); err != nil {
		return JobTicket{}, err
	}
	return JobTicket{RunID: draft.Run, WorkflowID: draft.Workflow, State: "queued"}, nil
}
