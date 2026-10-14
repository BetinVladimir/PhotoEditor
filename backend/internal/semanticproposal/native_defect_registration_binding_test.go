package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeDefectRegistrationBindingRejectsUnownedPlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	packet, err := marshalNativeDefectRegistrationBinding(ctx, nativeDefectDraftPlan{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || packet != nil {
		t.Fatal("cancelled binding exposed proof", err)
	}
	packet, err = marshalNativeDefectRegistrationBinding(context.Background(), nativeDefectDraftPlan{}, v2input.Built{})
	if !errors.Is(err, v2input.ErrInvalid) || packet != nil {
		t.Fatal("unowned binding exposed proof", err)
	}
	plan := nativeDefectDraftPlan{seal: "forged"}
	packet, err = marshalNativeDefectRegistrationBinding(context.Background(), plan, v2input.Built{})
	if err == nil || packet != nil {
		t.Fatal("forged plan exposed proof", err)
	}
}

func TestNativeDefectRegistrationCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	err := reader.registerNativeDefectInputsInTransaction(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{}, TransferColorProfile{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled registration accessed infrastructure", err)
	}
}

func TestNativeDefectCoordinatorAndBudgetCancelledBeforeInfrastructure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	prepared, err := reader.persistNativeDefectPreparationInputs(ctx, nil, nil, auth.Identity{}, nativeDefectDraftPlan{}, TransferColorProfile{})
	if !errors.Is(err, context.Canceled) || prepared.Graph.ID != uuid.Nil {
		t.Fatal("cancelled coordinator exposed transport", err)
	}
	err = reader.lockNativeDefectGraphBudgetInTransaction(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{}, TransferColorProfile{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled budget accessed SQL", err)
	}
}

func TestNativeDefectAdmissionEvidenceCancelledOrUnowned(t *testing.T) {
	reader := SQLTransferSourceReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	packet, err := reader.marshalNativeDefectAdmissionEvidence(ctx, auth.Identity{}, nativeDefectDraftPlan{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || packet != nil {
		t.Fatal("cancelled admission exposed evidence", err)
	}
	packet, err = reader.marshalNativeDefectAdmissionEvidence(context.Background(), auth.Identity{}, nativeDefectDraftPlan{}, v2input.Built{})
	if !errors.Is(err, v2input.ErrInvalid) || packet != nil {
		t.Fatal("unowned admission exposed evidence", err)
	}
}

func TestNativeDefectAdmissionLockCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (SQLTransferSourceReader{}).lockNativeDefectAdmissionInTransaction(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled admission lock accessed SQL", err)
	}
}

func TestNativeDefectSealCancelledExposesNoTicket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	ticket, err := reader.sealRegisteredNativeDefectPreparation(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled seal exposed ticket", err)
	}
	ticket, err = reader.sealNativeDefectPreparationInTransaction(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled transaction exposed ticket", err)
	}
	ticket, err = sealNativeDefectPacketInTransaction(ctx, nil, uuid.Nil, uuid.Nil, uuid.Nil, nil)
	if !errors.Is(err, context.Canceled) || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled packet seal exposed ticket", err)
	}
}

func TestNativeDefectRecoveryCancelledExposesNoTransportOrTicket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	built, err := recoverNativeDefectTransportInTransaction(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{})
	if !errors.Is(err, context.Canceled) || built.Count != 0 || built.Manifest.ArtifactID != uuid.Nil {
		t.Fatal("cancelled recovery exposed transport", err)
	}
	ticket, err := (SQLTransferSourceReader{}).resumeNativeDefectPreparation(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{})
	if !errors.Is(err, context.Canceled) || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled recovery exposed ticket", err)
	}
}

func TestNativeDefectResultLookupCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (SQLTransferSourceReader{}).nativeInputDefectResultCatalog(ctx, auth.Identity{}, nativeDefectDraftPlan{})
	if !errors.Is(err, context.Canceled) || result.ResultID != uuid.Nil || result.Outputs != nil {
		t.Fatal("cancelled lookup exposed catalog", err)
	}
}

func TestNativeDefectObjectLookupCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	objects, err := (SQLTransferSourceReader{}).nativeInputDefectResultObjects(ctx, auth.Identity{}, nativeDefectDraftPlan{}, nativeInputDefectResultCatalog{})
	if !errors.Is(err, context.Canceled) || objects != ([6]defectEvidenceObject{}) {
		t.Fatal("cancelled object lookup exposed pins", err)
	}
}

func TestNativeDefectContentCancelledExposesNoEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (SQLTransferSourceReader{}).readCompletedNativeInputDefect(ctx, auth.Identity{}, nativeDefectDraftPlan{}, TransferColorProfile{})
	if !errors.Is(err, context.Canceled) || result.catalog.ResultID != uuid.Nil || result.evidence.Params != nil || result.evidence.Proposal != nil || result.evidence.Report != nil {
		t.Fatal("cancelled content exposed evidence", err)
	}
}

func TestNativeDefectStartCancelledExposesNoSubmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	submission, err := (SQLTransferSourceReader{}).startNativeDefectPreparation(ctx, nil, v2draft.Publisher{}, nil, auth.Identity{}, nativeDefectFacePreparation{}, TransferColorProfile{}, v2input.Ref{}, v2input.SemanticQualifiedCosts{}, v2input.Built{}, v2input.Built{}, v2input.Built{}, v2input.Built{}, 1, 0, 1, 1, 8388609)
	if !errors.Is(err, context.Canceled) || submission.plan.seal != "" || submission.ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled start exposed submission", err)
	}
}

func TestNativeDefectCompletedPacketCancelledExposesNoEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	packet, err := (SQLTransferSourceReader{}).marshalNativeDefectCompletedEvidence(ctx, auth.Identity{}, nativeDefectDraftPlan{}, TransferColorProfile{}, completedNativeInputDefect{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || packet != nil {
		t.Fatal("cancelled completed packet exposed evidence", err)
	}
}

func TestNativeDefectCompletedLockCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (SQLTransferSourceReader{}).lockNativeCompletedDefectInTransaction(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled completed lock accessed SQL", err)
	}
}

func TestNativeDefectBootstrapCancelledOrUnconfigured(t *testing.T) {
	bootstrap := nativePreparationBootstrap{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	submission, err := bootstrap.bootstrapDefect(ctx, auth.Identity{}, nativeFaceDraftPlan{}, nativeSkinDraftPlan{}, TransferColorProfile{}, [2]float64{.002, .012}, v2input.Ref{}, nativeDefectProducerTransports{}, nativePreparationBudget{})
	if !errors.Is(err, context.Canceled) || submission.plan.seal != "" || submission.ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled bootstrap exposed child", err)
	}
	submission, err = bootstrap.bootstrapDefect(context.Background(), auth.Identity{}, nativeFaceDraftPlan{}, nativeSkinDraftPlan{}, TransferColorProfile{}, [2]float64{.002, .012}, v2input.Ref{}, nativeDefectProducerTransports{}, nativePreparationBudget{})
	if !errors.Is(err, v2input.ErrInvalid) || submission.plan.seal != "" || submission.ticket != (v2input.JobTicket{}) {
		t.Fatal("unconfigured bootstrap exposed child", err)
	}
}

func TestNativeInputDefectProtectionCancelledExposesNoDocument(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prepared, err := (SQLTransferSourceReader{}).prepareNativeInputDefectProtection(ctx, auth.Identity{}, nativeDefectDraftPlan{}, TransferColorProfile{})
	if !errors.Is(err, context.Canceled) || prepared.document != uuid.Nil || prepared.body != "" || prepared.pixels != (v2input.Ref{}) {
		t.Fatal("cancelled protection exposed document", err)
	}
}

func TestNativeInputDefectProtectionPublicationCancelledExposesNoRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	published, err := (SQLTransferSourceReader{}).publishNativeInputDefectProtection(ctx, auth.Identity{}, nativeDefectDraftPlan{}, TransferColorProfile{}, v2draft.Publisher{})
	if !errors.Is(err, context.Canceled) || published.root != (v2input.Ref{}) || published.preparation.body != "" {
		t.Fatal("cancelled publication exposed root", err)
	}
}

func TestNativeInputProtectionRegistrationCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (SQLTransferSourceReader{}).registerNativeInputDefectProtection(ctx, nil, auth.Identity{}, publishedNativeInputDefectProtection{}, TransferColorProfile{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled registration accessed SQL", err)
	}
}

func TestRegisteredNativeInputProtectionCancelledExposesNoProof(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (SQLTransferSourceReader{}).readRegisteredNativeInputProtection(ctx, auth.Identity{}, publishedNativeInputDefectProtection{}, TransferColorProfile{})
	if !errors.Is(err, context.Canceled) || result.verified.Root != (v2input.Ref{}) || result.verified.Body != nil {
		t.Fatal("cancelled read exposed proof", err)
	}
}

func TestNativeInputSourceContinuationCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	proof, err := composeNativeInputSourceContinuation(ctx, auth.Identity{}, Request{}, sourcePreparationProof{}, registeredNativeInputProtection{}, TransferColorProfile{})
	if !errors.Is(err, context.Canceled) || proof.source.tenant != uuid.Nil {
		t.Fatal("cancelled continuation exposed source", err)
	}
	err = (SQLTransferSourceReader{}).lockNativeInputSourceContinuation(ctx, nil, auth.Identity{}, Request{}, nativeInputSourceContinuation{}, TransferColorProfile{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled continuation accessed SQL", err)
	}
}

func TestNativeInputPersonCoverageCancelledBeforeIOAndSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	proof, err := reader.prepareNativeInputReferencePerson(ctx, auth.Identity{}, Request{}, sourcePreparationProof{}, registeredNativeInputProtection{}, TransferColorProfile{}, v2input.Ref{})
	if !errors.Is(err, context.Canceled) || proof.person.expected != "" || proof.policy.body != "" {
		t.Fatal("cancelled coverage exposed proof", err)
	}
	err = reader.lockNativeInputReferencePerson(ctx, nil, auth.Identity{}, Request{}, nativeInputReferencePerson{}, TransferColorProfile{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled person lock accessed SQL", err)
	}
}
