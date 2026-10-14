package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Nil infrastructure intentionally makes accidental DB/object access fail.
// Cancelled requests must stop at the boundary and expose no retained proof.
func TestNativeSkinCancelledBeforeDependencyIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	defectTransport, defectProof, defectTransportErr := publishNativeDefectDraftTransport(ctx, nativeDefectDraftPlan{}, nil)
	if !errors.Is(defectTransportErr, context.Canceled) || defectTransport.Graph.ID != uuid.Nil || defectProof.Count != 0 {
		t.Fatal("cancelled native defect transport exposed publication", defectTransportErr)
	}
	defectAdmission, defectAdmissionErr := reader.beginNativeDefectDraftInTransaction(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{}, TransferColorProfile{})
	if !errors.Is(defectAdmissionErr, context.Canceled) || defectAdmission.RunID != uuid.Nil {
		t.Fatal("cancelled native defect begin exposed draft", defectAdmissionErr)
	}
	draft, draftErr := reader.compileNativeDefectDraftPlan(ctx, auth.Identity{}, nativeDefectTaskPreparation{}, 1)
	if !errors.Is(draftErr, context.Canceled) || draft.seal != "" {
		t.Fatal("cancelled native defect draft exposed plan", draftErr)
	}
	nativeTask, nativeTaskErr := reader.compileNativeDefectTask(ctx, auth.Identity{}, nativeDefectDependencies{}, nativeDefectStageQualification{}, defectEvidenceObject{}, nil, 1, 0, 1, 1)
	if !errors.Is(nativeTaskErr, context.Canceled) || nativeTask.entries != "" || nativeTask.params.ref.ArtifactID != uuid.Nil {
		t.Fatal("cancelled native defect compilation exposed task", nativeTaskErr)
	}
	entries, sizes, inventoryErr := compileNativeDefectInventory(ctx, nativeDefectFacePreparation{}, defectEvidenceObject{}, nil)
	if !errors.Is(inventoryErr, context.Canceled) || entries != nil || sizes != nil {
		t.Fatal("cancelled defect inventory exposed files", inventoryErr)
	}
	if err := reader.lockNativeDefectStageInTransaction(ctx, nil, auth.Identity{}, nativeDefectDependencies{}, nativeDefectStageQualification{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled native defect stage lock accessed SQL", err)
	}
	qualification, qualificationErr := reader.prepareNativeDefectStageQualification(ctx, auth.Identity{}, nativeDefectFacePreparation{}, TransferColorProfile{}, v2input.Ref{}, v2input.SemanticQualifiedCosts{})
	if !errors.Is(qualificationErr, context.Canceled) || qualification.landmarksSeal != "" || qualification.report.expected != "" {
		t.Fatal("cancelled native defect qualification exposed proof", qualificationErr)
	}
	nativeParams, nativeParamsErr := reader.publishNativeDefectVersionParams(ctx, auth.Identity{}, nativeDefectFacePreparation{}, TransferColorProfile{}, v2draft.Publisher{})
	if !errors.Is(nativeParamsErr, context.Canceled) || nativeParams.Ref.ArtifactID != uuid.Nil || nativeParams.Inputs != nil {
		t.Fatal("cancelled native defect publication exposed Params", nativeParamsErr)
	}
	dependencies, dependencyErr := reader.prepareNativeDefectDependencies(ctx, auth.Identity{}, nativeDefectFacePreparation{}, TransferColorProfile{}, v2input.Built{}, v2input.Built{}, v2input.Built{}, v2input.Built{})
	if !errors.Is(dependencyErr, context.Canceled) || dependencies.seal != "" {
		t.Fatal("cancelled native defect dependencies exposed proof", dependencyErr)
	}
	if err := reader.lockNativeDefectDependenciesInTransaction(ctx, nil, auth.Identity{}, nativeDefectDependencies{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled defect dependency lock accessed SQL", err)
	}
	landmarkPacket, landmarkErr := reader.marshalNativeLandmarkCompletedEvidence(ctx, auth.Identity{}, nativeFaceDraftPlan{}, TransferColorProfile{}, completedNativeLandmarks{}, v2input.Built{}, v2input.Built{})
	if !errors.Is(landmarkErr, context.Canceled) || landmarkPacket != nil {
		t.Fatal("cancelled landmarks exposed completed packet", landmarkErr)
	}
	if err := reader.lockNativeCompletedLandmarksInTransaction(ctx, nil, auth.Identity{}, nativeFaceDraftPlan{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled landmark lock accessed SQL", err)
	}
	defectFaces, defectErr := reader.prepareNativeDefectFaceParams(ctx, auth.Identity{}, nativeFaceDraftPlan{}, nativeSkinDraftPlan{}, TransferColorProfile{}, [2]float64{.002, .012}, nil, nil, nil)
	if !errors.Is(defectErr, context.Canceled) || defectFaces.params.params != "" || defectFaces.landmarks.body != "" || defectFaces.skin.body != "" {
		t.Fatal("cancelled native defect face preparation exposed proof", defectErr)
	}
	if err := reader.lockNativeCompletedSkinInTransaction(ctx, nil, auth.Identity{}, nativeSkinDraftPlan{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled completed skin lock accessed SQL", err)
	}
	completedPacket, completedErr := reader.marshalNativeSkinCompletedEvidence(ctx, auth.Identity{}, nativeSkinDraftPlan{}, TransferColorProfile{}, completedNativeSkin{}, v2input.Built{}, v2input.Built{}, v2input.Built{})
	if !errors.Is(completedErr, context.Canceled) || completedPacket != nil {
		t.Fatal("cancelled completed skin exposed evidence", completedErr)
	}
	submission, startErr := reader.startNativeSkinPreparation(ctx, nil, v2draft.Publisher{}, nil, auth.Identity{}, nativeFaceDraftPlan{}, TransferColorProfile{}, 1, 1, 1, 1)
	if !errors.Is(startErr, context.Canceled) || submission.ticket != (v2input.JobTicket{}) || submission.plan.seal != "" {
		t.Fatal("cancelled skin start exposed submission", startErr)
	}
	content, contentErr := reader.readCompletedNativeSkin(ctx, auth.Identity{}, nativeSkinDraftPlan{}, TransferColorProfile{})
	if !errors.Is(contentErr, context.Canceled) || content.body != "" || content.catalog.ResultID != uuid.Nil {
		t.Fatal("cancelled skin content exposed proof", contentErr)
	}
	recovered, recoveryErr := reader.resumeNativeSkinPreparation(ctx, nil, auth.Identity{}, nativeSkinDraftPlan{})
	if !errors.Is(recoveryErr, context.Canceled) || recovered != (v2input.JobTicket{}) {
		t.Fatal("cancelled skin recovery exposed ticket", recoveryErr)
	}
	ticket, sealErr := reader.sealRegisteredNativeSkinPreparation(ctx, nil, auth.Identity{}, nativeSkinDraftPlan{}, v2input.Built{}, v2input.Built{}, v2input.Built{})
	if !errors.Is(sealErr, context.Canceled) || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled skin seal exposed ticket", sealErr)
	}
	admission, admissionErr := reader.marshalNativeSkinAdmissionEvidence(ctx, auth.Identity{}, nativeSkinDraftPlan{}, v2input.Built{}, v2input.Built{}, v2input.Built{})
	if !errors.Is(admissionErr, context.Canceled) || admission != nil {
		t.Fatal("cancelled skin admission exposed evidence", admissionErr)
	}
	dependency, dependencyErr := reader.marshalNativeSkinDependencyEvidence(ctx, auth.Identity{}, nativeSkinDraftPlan{})
	if !errors.Is(dependencyErr, context.Canceled) || dependency != nil {
		t.Fatal("cancelled dependency exposed evidence", dependencyErr)
	}
	params, err := reader.publishNativeSkinStageParams(ctx, auth.Identity{}, nativeFaceDraftPlan{}, TransferColorProfile{}, v2draft.Publisher{})
	if !errors.Is(err, context.Canceled) || params.document != uuid.Nil || params.ref.ArtifactID != uuid.Nil {
		t.Fatal("cancelled skin publication exposed Params", err)
	}
	task, err := reader.prepareNativeSkinTask(ctx, auth.Identity{}, nativeFaceDraftPlan{}, TransferColorProfile{}, nativeSkinStageParams{}, 1, 1, 1)
	if !errors.Is(err, context.Canceled) || task.params.ref.ArtifactID != uuid.Nil {
		t.Fatal("cancelled skin preparation exposed task", err)
	}
	prepared, err := reader.persistNativeSkinPreparationInputs(ctx, nil, nil, auth.Identity{}, nativeSkinDraftPlan{}, TransferColorProfile{})
	if !errors.Is(err, context.Canceled) || prepared.Graph.ID != uuid.Nil || prepared.Graph.Built.Manifest.ArtifactID != uuid.Nil {
		t.Fatal("cancelled skin coordinator exposed transport", err)
	}
	transport, proof, err := publishNativeSkinDraftTransport(ctx, nativeSkinDraftPlan{}, nil)
	if !errors.Is(err, context.Canceled) || transport.Graph.ID != uuid.Nil || proof.Count != 0 {
		t.Fatal("cancelled skin transport exposed proof", err)
	}
	packet, err := marshalNativeSkinRegistrationBinding(ctx, nativeSkinDraftPlan{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || packet != nil {
		t.Fatal("cancelled skin binding exposed evidence", err)
	}
}

func TestNativeSkinUnsealedPlanRefusesWithoutInfrastructure(t *testing.T) {
	reader := SQLTransferSourceReader{}
	prepared, err := reader.persistNativeSkinPreparationInputs(context.Background(), nil, nil, auth.Identity{}, nativeSkinDraftPlan{}, TransferColorProfile{})
	if !errors.Is(err, v2input.ErrInvalid) || prepared.Graph.ID != uuid.Nil {
		t.Fatal("unsealed coordinator plan accepted", err)
	}
	packet, err := marshalNativeSkinRegistrationBinding(context.Background(), nativeSkinDraftPlan{}, v2input.Built{})
	if !errors.Is(err, v2input.ErrInvalid) || packet != nil {
		t.Fatal("unsealed registration packet exposed", err)
	}
}
