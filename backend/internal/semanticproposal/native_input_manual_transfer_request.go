package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Server-only initial orchestration, using original byte-verified native source
// and protection authority. Public service chooses that authority independently
// of user intent. Sealed/restart reconstruction uses a separate recovery route.
func (p TransferDraftPreparer) prepareAndActivateNativeInputManualTransfer(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, raw []byte, person nativeInputManualReferencePerson, defectBuilt v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	p.Configuration = ownTransferPreparation(p.Configuration)
	if p.Reader.DB == nil || p.Reader.Objects == nil || p.Documents.DB == nil || p.Documents.Objects == nil || p.Scheduler == nil || p.TransportDocuments == nil || p.ReservedBytes <= 0 || p.ReservedBytes > 9007199254740991 || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if err := validateTransferConfiguration(p.Configuration); err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	reference, err := p.Reader.readNativeInputManualTransferReferenceDocuments(ctx, who, request, person, p.Configuration.Color, p.Configuration.MetadataBytes)
	if err != nil {
		return zero, err
	}
	if err := p.Reader.validateNativeInputManualTransferReference(ctx, who, request, p.Configuration, reference); err != nil {
		return zero, err
	}
	published, err := PublishIntent(ctx, p.Documents, who, request)
	if err != nil {
		return zero, err
	}
	inputs, documents, err := p.Reader.prepareNativeInputManualTransferInputs(ctx, who, request, published, p.Configuration, reference)
	if err != nil {
		return zero, err
	}
	params, err := PublishTransferParams(ctx, p.Documents, who, request, key, published, p.Configuration.Qualification.ModelLockSHA, p.Configuration.Qualification.Calibration)
	if err != nil {
		return zero, err
	}
	snapshot, body, err := p.Reader.PublishTargetSnapshot(ctx, p.Documents, who, request, key, published)
	if err != nil {
		return zero, err
	}
	plan, err := p.Reader.compileNativeInputManualTransferWorkflowPlan(ctx, who, request, published, snapshot, body, params, inputs, p.Configuration.Qualification, documents, p.Configuration)
	if err != nil {
		return zero, err
	}
	prepared, err := p.Reader.persistNativeInputManualTransfer(ctx, p.Scheduler, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: tenant, Run: request.RunID}, who, request, key, published, plan, p.Configuration, defectBuilt, p.ReservedBytes)
	if err != nil {
		return zero, err
	}
	transports := make([]v2input.Built, len(prepared))
	for i, task := range prepared {
		transports[i] = task.Graph.Built
	}
	return p.Reader.activateNativeInputManualTransfer(ctx, p.Scheduler, who, request, key, published, plan, p.Configuration, defectBuilt, transports)
}
