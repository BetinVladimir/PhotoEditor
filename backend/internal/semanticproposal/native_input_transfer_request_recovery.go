package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

// Metadata is loaded by a server-owned reader; this boundary has no publisher.
// It reconstructs the transfer plan from original pinned documents and live
// source/person/qualification authority, then resumes only saved transport.
func (p TransferDraftPreparer) reconstructAndResumeNativeInputTransfer(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, raw []byte, person nativeInputReferencePerson, defectBuilt v2input.Built, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage, params []v2input.Ref) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	p.Configuration = ownTransferPreparation(p.Configuration)
	if p.Reader.DB == nil || p.Reader.Objects == nil || p.Scheduler == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if err := validateTransferConfiguration(p.Configuration); err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	saved, err := p.Reader.readNativeInputTransferSavedConfig(ctx, who, request, p.Configuration)
	if err != nil {
		return zero, err
	}
	p.Configuration.legacyNativeQueueOmitted = saved.legacyQueueOmitted
	if saved.snapshot != snapshot || !reflect.DeepEqual(saved.params, params) {
		return zero, v2input.ErrConflict
	}
	actualSnapshot, err := p.Reader.readNativeInputTransferSavedSnapshot(ctx, who, request, saved, p.Configuration.MetadataBytes)
	if err != nil {
		return zero, err
	}
	if string(actualSnapshot) != string(snapshotBody) {
		return zero, v2input.ErrConflict
	}
	if err := p.Reader.verifyNativeInputTransferSavedParams(ctx, who, request, published, saved, p.Configuration); err != nil {
		return zero, err
	}
	plan, err := p.reconstructNativeInputTransferPlan(ctx, who, request, person, published, snapshot, snapshotBody, params)
	if err != nil {
		return zero, err
	}
	if plan.core.configSHA != saved.configSHA {
		return zero, v2input.ErrConflict
	}
	currentSaved, err := p.Reader.readNativeInputTransferSavedConfig(ctx, who, request, p.Configuration)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(currentSaved, saved) {
		return zero, v2input.ErrConflict
	}
	return p.Reader.resumeNativeInputTransfer(ctx, p.Scheduler, who, request, key, published, plan, p.Configuration, defectBuilt)
}

func (p TransferDraftPreparer) reconstructNativeInputTransferPlan(ctx context.Context, who auth.Identity, request Request, person nativeInputReferencePerson, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage, params []v2input.Ref) (nativeInputTransferWorkflowPlan, error) {
	var zero nativeInputTransferWorkflowPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(params) == 0 || len(params) > 5000 || len(params) != len(published.Inventory.Targets) || snapshot.ArtifactID == uuid.Nil || len(snapshotBody) == 0 {
		return zero, v2input.ErrInvalid
	}
	reference, err := p.Reader.readNativeInputTransferReferenceDocuments(ctx, who, request, person, p.Configuration.Color, p.Configuration.MetadataBytes)
	if err != nil {
		return zero, err
	}
	if err := p.Reader.validateNativeInputTransferReference(ctx, who, request, p.Configuration, reference); err != nil {
		return zero, err
	}
	inputs, documents, err := p.Reader.prepareNativeInputTransferInputs(ctx, who, request, published, p.Configuration, reference)
	if err != nil {
		return zero, err
	}
	return p.Reader.compileNativeInputTransferWorkflowPlan(ctx, who, request, published, snapshot, snapshotBody, params, inputs, p.Configuration.Qualification, documents, p.Configuration)
}

// Loads transfer metadata and snapshot from original saved records. Original
// native producer reconstruction still supplies the private person proof.
func (p TransferDraftPreparer) loadAndResumeNativeInputTransfer(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, raw []byte, person nativeInputReferencePerson, defectBuilt v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	p.Configuration = ownTransferPreparation(p.Configuration)
	if p.Reader.DB == nil || p.Reader.Objects == nil || p.Scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	if err := validateTransferConfiguration(p.Configuration); err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	if _, err := composeNativeInputSourceContinuation(ctx, who, request, person.source.source, person.source.protection, p.Configuration.Color); err != nil {
		return zero, err
	}
	saved, err := p.Reader.readNativeInputTransferSavedConfig(ctx, who, request, p.Configuration)
	if err != nil {
		return zero, err
	}
	p.Configuration.legacyNativeQueueOmitted = saved.legacyQueueOmitted
	snapshotBody, err := p.Reader.readNativeInputTransferSavedSnapshot(ctx, who, request, saved, p.Configuration.MetadataBytes)
	if err != nil {
		return zero, err
	}
	parent := person.source.protection.published.preparation.plan.prepared.dependencies.preparation.landmarksPlan.detector
	return p.reconstructAndResumeNativeInputTransfer(ctx, who, tenant, key, raw, person, defectBuilt, parent.published, saved.snapshot, snapshotBody, saved.params)
}
