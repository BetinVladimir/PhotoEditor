package semanticproposal

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Consumes a private original manual child submission after its output becomes
// available. Publication/result/person verification precede parent preparation;
// the child ticket never becomes the public parent receipt. Missing outputs are
// refused and require later original-state recovery, not substitute evidence.
func (p TransferDraftPreparer) prepareAndActivateManualParentFromSubmission(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, raw []byte, child nativeInputManualInitialSubmission, built v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := verifyNativeInputManualInitialDraftSeal(ctx, child.plan); err != nil {
		return zero, err
	}
	p.Configuration = ownTransferPreparation(p.Configuration)
	if p.Scheduler == nil || p.Configuration.PersonProtection == nil {
		return zero, v2input.ErrInvalid
	}
	if child.ticket.RunID != child.plan.shell.Run || child.ticket.WorkflowID != child.plan.shell.WorkflowID || child.ticket.State != "queued" {
		return zero, v2input.ErrConflict
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	producer := child.plan.prepared.stage.dependencies.dependencies.producers.detector
	parent := producer.request
	if who.SessionID == uuid.Nil || parent.ActorID != who.UserID || parent.TenantID != request.TenantID || parent.RunID != request.RunID || parent.WorkflowID != request.WorkflowID || parent.RequestSHA != request.RequestSHA {
		return zero, v2input.ErrConflict
	}
	owned, err := p.Reader.compileNativeInputManualInitialDraft(ctx, who, child.plan.prepared, child.plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != child.plan.seal || !reflect.DeepEqual(owned.shell, child.plan.shell) {
		return zero, v2input.ErrConflict
	}
	if _, err := owned.prepared.compiled.frozen.BindPublishedTransport(ctx, built); err != nil {
		return zero, err
	}
	registered, err := p.Reader.publishRegisteredNativeInputManualInitialProtection(ctx, p.Scheduler, who, owned, p.Configuration.Color, p.Documents, built)
	if err != nil {
		return zero, err
	}
	configured := p.Configuration.SourceProtection
	if configured != (v2input.Ref{}) && configured != registered.verified.Root {
		return zero, v2input.ErrConflict
	}
	// Only independently verified original registration selects the source root.
	p.Configuration.SourceProtection = registered.verified.Root
	person, err := p.Reader.prepareNativeInputManualReferencePerson(ctx, who, request, producer.prepared.source, registered, p.Configuration.Color, p.Configuration.PersonProtection.Report)
	if err != nil {
		return zero, err
	}
	existing, err := p.Reader.hasNativeInputSavedParent(ctx, who, request)
	if err != nil {
		return zero, err
	}
	if existing {
		return p.loadAndResumeNativeInputManualTransfer(ctx, who, tenant, key, request.Body, person, built)
	}
	if len(p.Configuration.TargetProtection) == 0 {
		p.Configuration, err = p.Reader.selectTransferTargetProtectionConfiguration(ctx, who, request, producer.published, p.Configuration)
		if err != nil {
			return zero, err
		}
	}
	return p.prepareAndActivateNativeInputManualTransfer(ctx, who, tenant, key, request.Body, person, built)
}
