package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// TransferDraftPreparer is server-configured. Configuration protection refs
// require independent policy authority before enabling a public create service.
// This preparer returns private graphs, never queued tickets. Completed graphs
// are reconstructed for authenticated activation recovery without registration.
type TransferDraftPreparer struct {
	Reader             SQLTransferSourceReader
	Documents          v2draft.Publisher
	Scheduler          *pgxpool.Pool
	TransportDocuments v2input.DocumentPublisher
	Configuration      TransferPreparation
	ReservedBytes      int64
}

type PreparedTransferDraft struct {
	published         PublishedIntent
	RunID, WorkflowID uuid.UUID
	Outcome           string
	plan              WorkflowPlan
	transports        []v2input.Built
}

// Prepare recovers partial durable documents/drafts using deterministic request
// identities. Every error returns an empty result. No object IO spans a SQL TX.
func (p TransferDraftPreparer) Prepare(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, raw []byte) (PreparedTransferDraft, error) {
	return p.prepareDraft(ctx, who, tenant, key, raw, nil)
}

// prepareNativeDraft consumes only internally verified original reference
// authority. It shares durable publication, quota admission and input replay
// with Prepare; it cannot activate the resulting blocked draft.
func (p TransferDraftPreparer) prepareNativeDraft(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, raw []byte, reference nativeTransferReferenceDocuments) (PreparedTransferDraft, error) {
	return p.prepareDraft(ctx, who, tenant, key, raw, &reference)
}

func (p TransferDraftPreparer) prepareDraft(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, raw []byte, native *nativeTransferReferenceDocuments) (PreparedTransferDraft, error) {
	p.Configuration = ownTransferPreparation(p.Configuration)
	var empty PreparedTransferDraft
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if p.Scheduler == nil || p.TransportDocuments == nil || p.ReservedBytes <= 0 || p.ReservedBytes > 9007199254740991 || !validMetadataBudget(p.Configuration) || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return empty, v2input.ErrInvalid
	}
	if err := validateTransferConfiguration(p.Configuration); err != nil {
		return empty, err
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return empty, err
	}
	if native != nil {
		if _, err := p.Reader.validateNativeTransferReference(who, request, p.Configuration, *native); err != nil {
			return empty, err
		}
	}
	published, err := PublishIntent(ctx, p.Documents, who, request)
	if err != nil {
		return empty, err
	}
	var inputs []TransferTaskInputs
	var documents []v2input.SemanticPinnedDocuments
	if native == nil {
		inputs, documents, err = p.Reader.PrepareTransferInputs(ctx, who, request, published, p.Configuration)
	} else {
		inputs, documents, err = p.Reader.prepareNativeTransferInputs(ctx, who, request, published, p.Configuration, *native)
	}
	if err != nil {
		return empty, err
	}
	refs, err := PublishTransferParams(ctx, p.Documents, who, request, key, published, p.Configuration.Qualification.ModelLockSHA, p.Configuration.Qualification.Calibration)
	if err != nil {
		return empty, err
	}
	snapshot, body, err := p.Reader.PublishTargetSnapshot(ctx, p.Documents, who, request, key, published)
	if err != nil {
		return empty, err
	}
	plan, err := CompileTransferWorkflowPlanFromDocuments(request, who.SessionID, published, snapshot, body, refs, inputs, p.Configuration.Qualification, documents)
	if err != nil {
		return empty, err
	}
	admitted, err := BeginDraft(ctx, p.Scheduler, who, request, key, published, plan, p.ReservedBytes)
	if err != nil {
		return empty, err
	}
	transportDocuments := p.TransportDocuments
	if admitted.Outcome == "replayed" {
		transportDocuments = nativeReadyTransportPublisher{db: p.Scheduler, who: who, tenant: tenant, run: request.RunID, bucket: p.Reader.Bucket}
	}
	tasks, err := PublishWorkflowTasks(ctx, plan, v2input.ScopedPublisher{Documents: transportDocuments, Tenant: tenant, Run: request.RunID})
	if err != nil {
		return empty, err
	}
	transports := make([]v2input.Built, len(tasks))
	for i, task := range tasks {
		transports[i] = task.Graph.Built
	}
	switch admitted.Outcome {
	case "created", "pending":
		if err := RegisterWorkflowInputs(ctx, p.Scheduler, who, request, published, plan, transports); err != nil {
			return empty, err
		}
	case "replayed":
		// Completed inputs are verified by authenticated activation recovery;
		// never try to register or repair an already sealed graph.
	default:
		return empty, v2input.ErrInvalid
	}
	return PreparedTransferDraft{published: published, RunID: request.RunID, WorkflowID: request.WorkflowID, Outcome: admitted.Outcome, plan: plan, transports: transports}, nil
}
