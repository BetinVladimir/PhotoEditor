package v2input

import (
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// FrozenCaptureGraph is the durable prepared draft. Prepare must commit the
// blocked task shell and pin these exact documents before returning; retries
// recover the same draft rather than observing mutable session state.
type FrozenCaptureGraph struct {
	Run, Workflow   uuid.UUID
	Params          CaptureAssessParams
	ParamsRef       Ref
	ContextBody     json.RawMessage
	Source, Proxy   CaptureImageInputs
	Shots, Consents Ref
}
type CaptureGraphPreparer interface {
	PrepareCapture(context.Context, CaptureAssessmentLease) (FrozenCaptureGraph, error)
}
type CaptureGraphCommitter interface {
	CommitCapture(context.Context, CaptureAssessmentLease, uuid.UUID, uuid.UUID, GraphTask, json.RawMessage, json.RawMessage) error
}
type PreparedCaptureAdmission struct {
	Source            CaptureGraphPreparer
	TransportForGraph func(CaptureAssessmentLease, FrozenCaptureGraph) Publisher
	Committer         CaptureGraphCommitter
}

// AdmitCapture transports a prepared immutable inventory outside transactions.
// The committer repeats the fence and receipt/policy binding in its transaction.
func (a PreparedCaptureAdmission) AdmitCapture(ctx context.Context, l CaptureAssessmentLease) error {
	if a.Source == nil || a.TransportForGraph == nil || a.Committer == nil || !validCaptureLease(l) {
		return ErrInvalid
	}
	graph, err := a.Source.PrepareCapture(ctx, l)
	if err != nil {
		return err
	}
	workflow, err := WorkflowID(graph.Run)
	if err != nil || workflow != graph.Workflow || graph.Params.Binding.AssetVersionID != l.AssetVersionID {
		return ErrInvalid
	}
	frozenPolicy, err := canonical.Bytes(l.Policy)
	policy, policyErr := canonical.Marshal(graph.Params.Policy)
	if err != nil || policyErr != nil || string(frozenPolicy) != string(policy) {
		return ErrInvalid
	}
	contextJCS, err := canonical.Bytes(graph.ContextBody)
	if err != nil || string(contextJCS) != string(graph.ContextBody) {
		return ErrInvalid
	}
	contextSHA, err := canonical.Hash(graph.ContextBody)
	if err != nil || contextSHA != graph.Params.Context.SHA256 {
		return ErrInvalid
	}
	var captureContext CaptureContext
	if json.Unmarshal(graph.ContextBody, &captureContext) != nil || captureContext.SessionID != l.SessionID ||
		captureContext.ShotList != graph.Shots || captureContext.Consents != graph.Consents {
		return ErrInvalid
	}
	taskID, err := TaskID(graph.Workflow, "capture.assess", 0)
	if err != nil {
		return err
	}
	closure, err := CompileCaptureClosure(taskID, graph.Params, graph.ParamsRef, graph.Source, graph.Proxy, graph.Shots, graph.Consents)
	if err != nil {
		return err
	}
	transport := a.TransportForGraph(l, graph)
	if transport == nil {
		return ErrInvalid
	}
	cursor, err := closure.Open(ctx)
	if err != nil {
		return err
	}
	built, buildErr := BuildInputClosure(ctx, taskID, cursor, transport)
	if closer, ok := cursor.(io.Closer); ok {
		_ = closer.Close()
	}
	if buildErr != nil {
		return buildErr
	}
	return a.Committer.CommitCapture(ctx, l, graph.Run, graph.Workflow,
		GraphTask{ID: taskID, Built: built, Open: closure.Open, Direct: closure.Direct}, closure.ParamsJCS, graph.ContextBody)
}
