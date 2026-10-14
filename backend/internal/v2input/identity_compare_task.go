package v2input

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

// FrozenIdentityCompareTask owns a complete immutable transport and bounded
// profile. It is deliberately not an admission authority or enablement flag.
type FrozenIdentityCompareTask struct {
	task        uuid.UUID
	workflow    uuid.UUID
	ordinal     int
	key         string
	params      Ref
	consents    Ref
	consentBody []byte
	profile     json.RawMessage
	closure     CaptureClosure
}

func CompileIdentityCompareTask(ctx context.Context, workflow uuid.UUID, ordinal int, backend string,
	params IdentityCompareParams, paramsRef Ref, documents IdentityCompareDocuments,
	expectedDatasetSHA string, sizes []SemanticInputSize, qualified SemanticQualifiedCosts,
	ramLimit, vramLimit, scratchLimit, outputLimit int64) (FrozenIdentityCompareTask, error) {
	var zero FrozenIdentityCompareTask
	closure, err := CompileIdentityCompareClosure(ctx, workflow, ordinal, params, paramsRef, documents, expectedDatasetSHA)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(documents.After)
	if err != nil {
		return zero, ErrInvalid
	}
	refs := make([]Ref, 0, len(closure.Direct)-1)
	for _, ref := range closure.Direct {
		if ref != paramsRef {
			refs = append(refs, ref)
		}
	}
	profile, err := CompileIdentityCompareProfileFromInputs(backend, image.Canvas.Width, image.Canvas.Height,
		paramsRef, refs, sizes, qualified, ramLimit, vramLimit, scratchLimit, outputLimit)
	if err != nil {
		return zero, err
	}
	task, err := TaskID(workflow, "identity.compare", ordinal)
	if err != nil {
		return zero, err
	}
	key, err := NodeKey("identity.compare", ordinal)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return FrozenIdentityCompareTask{task: task, workflow: workflow, ordinal: ordinal,
		key: key, params: paramsRef, consents: params.Consents,
		consentBody: append([]byte(nil), documents.Consents...),
		profile:     append(json.RawMessage(nil), profile...), closure: closure}, nil
}

func (f FrozenIdentityCompareTask) DraftTask() (DraftTask, error) {
	if f.task == uuid.Nil || f.workflow == uuid.Nil || f.ordinal < 0 ||
		!validRef(f.params) || !identityPinnedDocument(f.consents, f.consentBody) ||
		ValidateSessionConsentSnapshot(f.consentBody) != nil ||
		f.closure.Open == nil ||
		f.closure.InputDigest == "" || !json.Valid(f.profile) {
		return DraftTask{}, ErrInvalid
	}
	return DraftTask{ID: f.task, NodeKey: f.key, Operation: "identity.compare", OperationVersion: "1",
		ResourceClass: "gpu", ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256,
		InputDigest: f.closure.InputDigest, ExecutionProfile: append(json.RawMessage(nil), f.profile...)}, nil
}

func PublishIdentityCompareTask(ctx context.Context, frozen FrozenIdentityCompareTask,
	publisher Publisher) (PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSemanticTask{}, err
	}
	if publisher == nil {
		return PreparedSemanticTask{}, ErrInvalid
	}
	if _, err := frozen.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	cursor, err := frozen.closure.Open(ctx)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	built, err := BuildInputClosure(ctx, frozen.task, cursor, publisher)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	return frozen.BindPublishedTransport(ctx, built)
}

func (f FrozenIdentityCompareTask) BindPublishedTransport(ctx context.Context, built Built) (PreparedSemanticTask, error) {
	if _, err := f.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	prepared, err := bindFrozenTaskTransport(ctx, f.task, f.params, f.profile, f.key, f.closure, built)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	prepared.Graph.IdentityConsent = &IdentityConsentInput{Workflow: f.workflow,
		Ordinal: f.ordinal, Ref: f.consents, Body: append([]byte(nil), f.consentBody...)}
	return prepared, nil
}

func (f FrozenIdentityCompareTask) ParamsByteCount() (int64, error) {
	if _, err := f.DraftTask(); err != nil {
		return 0, err
	}
	return int64(len(f.closure.ParamsJCS)), nil
}

// TaskSource is the only graph source for this frozen identity task. The
// consent bytes travel with the source into SealGraphInTransaction.
func (f FrozenIdentityCompareTask) TaskSource() (TaskSource, error) {
	if _, err := f.DraftTask(); err != nil {
		return TaskSource{}, err
	}
	return TaskSource{ID: f.task, Open: f.closure.Open,
		Direct: append([]Ref(nil), f.closure.Direct...),
		IdentityConsent: &IdentityConsentInput{Workflow: f.workflow,
			Ordinal: f.ordinal, Ref: f.consents,
			Body: append([]byte(nil), f.consentBody...)}}, nil
}

// AdmitIdentityCompare binds the exact qualified frozen task and its consent
// witness to the ordinary two-transaction V2 admission coordinator.
func AdmitIdentityCompare(ctx context.Context, coordinator Coordinator,
	shell DraftShell, key, requestSHA string,
	frozen FrozenIdentityCompareTask) (JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return JobTicket{}, err
	}
	draft, err := frozen.DraftTask()
	if err != nil || len(shell.Tasks) != 1 || len(shell.Edges) != 0 ||
		shell.WorkflowID != frozen.workflow || shell.Tasks[0].ID != draft.ID ||
		shell.Tasks[0].NodeKey != draft.NodeKey ||
		shell.Tasks[0].Operation != draft.Operation ||
		shell.Tasks[0].OperationVersion != draft.OperationVersion ||
		shell.Tasks[0].ResourceClass != draft.ResourceClass ||
		shell.Tasks[0].ParamsArtifactID != draft.ParamsArtifactID ||
		shell.Tasks[0].ParamsSHA != draft.ParamsSHA ||
		shell.Tasks[0].InputDigest != draft.InputDigest ||
		!bytes.Equal(shell.Tasks[0].ExecutionProfile, draft.ExecutionProfile) {
		return JobTicket{}, ErrInvalid
	}
	source, err := frozen.TaskSource()
	if err != nil {
		return JobTicket{}, err
	}
	return coordinator.Admit(ctx, shell, key, requestSHA, []TaskSource{source})
}
