package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestExposureDocumentsDeriveInputsAndRejectSubstitutedBytes(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	json.Unmarshal(raw, &fixtures)
	imageBody := fixtures[0]
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		t.Fatal(err)
	}
	hashRef := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	canvas, _ := canonical.Hash(image.Canvas)
	source := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: hashRef(imageBody), CanvasHash: canvas}
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1, Protected: false}
	maskBody, _ := canonical.Marshal(mask)
	maskRef := hashRef(maskBody)
	mask.Protected = true
	protectionBody, _ := canonical.Marshal(mask)
	protection := hashRef(protectionBody)
	arbitrary := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	ev := .25
	edit := ApprovedEdit{SchemaVersion: 1, TargetID: uuid.New(), Source: source, Intent: arbitrary(), Candidate: arbitrary(), Mask: maskRef, Transform: arbitrary(), Kind: "local_exposure", Strength: .5, EV: &ev, PreservePersons: true}
	child := uuid.New()
	task := uuid.NewSHA1(child, []byte("semantic.edit:"+edit.TargetID.String()))
	body, _ := canonical.Marshal(edit)
	params := hashRef(body)
	params.ArtifactID = uuid.NewSHA1(task, []byte("Params"))
	draft := ApprovedTaskDraft{TaskID: task, TargetID: edit.TargetID, Params: params, Body: body, Kind: edit.Kind}
	sha := strings.Repeat("a", 64)
	pin := ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: sha, Report: arbitrary(), Calibration: arbitrary(), ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, IssuerID: uuid.New(), Backend: "cpu"}
	docs := ExposureSourceDocuments{Source: imageBody, Reference: imageBody, Mask: maskBody, Protection: protectionBody}
	// Preview resolves the same source documents without borrowing an approval.
	preview := previewParamsFixture()
	preview.Source = source
	preview.Protection = protection
	candidateBody, _ := canonical.Marshal(v2input.SemanticCandidate{SchemaVersion: 1, Target: source,
		Mask: &maskRef, Transform: &edit.Transform, Confidence: 1, State: "proposed"})
	intentBody, _ := canonical.Marshal(v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "local_exposure",
		Reference: source, ReferenceMask: maskRef, TargetSnapshotID: preview.SnapshotID,
		Strength: .5, EV: &ev, PreservePersons: true})
	preview.Candidate = hashRef(candidateBody)
	preview.Intent = hashRef(intentBody)
	removal := RemovalPreviewParams{ExposurePreviewParams: preview, Kind: "remove_object", ModelID: "big_lama", ContextPixels: 128, FeatherPixels: 8}
	removalIntent, _ := canonical.Marshal(v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object",
		Reference: source, ReferenceMask: maskRef, TargetSnapshotID: preview.SnapshotID, Strength: .5, PreservePersons: true})
	removal.Intent = hashRef(removalIntent)
	removalRun := uuid.New()
	removalTask, removalParams, removalClosure, removalErr := CompileRemovalPreviewInventory(removalRun, removal, candidateBody, removalIntent, docs)
	if removalErr != nil || removalTask != uuid.NewSHA1(removalRun, []byte("semantic.preview_remove_object:"+removal.TargetID.String())) || removalClosure.Open == nil || removalParams.ArtifactID != uuid.NewSHA1(removalTask, []byte("Params")) {
		t.Fatal("removal inventory binding", removalErr)
	}
	if _, _, _, err := CompileRemovalPreviewInventory(removalRun, removal, candidateBody, intentBody, docs); err == nil {
		t.Fatal("exposure intent admitted removal")
	}
	removalPin := RemovalQualificationPin(pin)
	removalPin.Backend = "mps"
	removal.ModelLockSHA = pin.ModelLockSHA
	removal.RuntimeLockSHA = pin.RuntimeLockSHA
	removal.DeviceProfileSHA = pin.DeviceProfileSHA
	removalLimits := ExposureResourceEstimate{RAMBytes: 1 << 40, VRAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	frozenRemoval, compileErr := CompileRemovalPreviewTask(removalRun, removal, candidateBody, removalIntent, docs,
		ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}, removalLimits, removalPin, 12000000000)
	if compileErr != nil {
		t.Fatal("removal task compilation", compileErr)
	}
	removalDraft, compileErr := frozenRemoval.DraftTask()
	if compileErr != nil || removalDraft.ID != removalTask || removalDraft.Operation != "semantic.preview_remove_object" {
		t.Fatal("removal task projection", compileErr)
	}
	var removalProfile RemovalExecutionProfile
	if decodeStrict(removalDraft.ExecutionProfile, &removalProfile) != nil ||
		removalProfile.Backend != "mps" || removalProfile.Qualification != removalPin ||
		removalProfile.ModelLockID != removalPin.ModelLockSHA || removalProfile.ModelLockSHA != removalPin.ModelLockSHA ||
		removalProfile.RuntimeLockSHA != removalPin.RuntimeLockSHA || removalProfile.DeviceProfileSHA != removalPin.DeviceProfileSHA ||
		removalProfile.DatasetSHA != removalPin.DatasetSHA || removalProfile.ResourceEstimate.VRAMBytes != 12000000000 {
		t.Fatal("removal compiled profile lost server-owned qualification")
	}
	for _, mutation := range []string{"model", "runtime", "device", "policy", "report_alias", "backend", "source", "candidate", "intent"} {
		t.Run("removal_qualification_"+mutation,func(t *testing.T) {
			changedPin := removalPin
			changedDocs := docs
			changedCandidate,changedIntent := candidateBody,removalIntent
			switch mutation {
			case "model": changedPin.ModelLockSHA = strings.Repeat("b",64)
			case "runtime": changedPin.RuntimeLockSHA = strings.Repeat("b",64)
			case "device": changedPin.DeviceProfileSHA = strings.Repeat("b",64)
			case "policy": changedPin.PolicyID = uuid.Nil
			case "report_alias": changedPin.Calibration = changedPin.Report
			case "backend": changedPin.Backend = "unknown"
			case "source": changedDocs.Source = append(append([]byte(nil),docs.Source...), '\n')
			case "candidate": changedCandidate = append(append([]byte(nil),candidateBody...), '\n')
			case "intent": changedIntent = intentBody
			}
			invalid,err := CompileRemovalPreviewTask(removalRun,removal,changedCandidate,changedIntent,changedDocs,
				ExposureOwnerCosts{RuntimeRAMBytes:1,InputIndexScratchBytes:16*1024*1024},removalLimits,changedPin,12000000000)
			if err == nil {t.Fatal("substituted removal qualification/source admitted",mutation)}
			if _,err := invalid.DraftTask(); err == nil {t.Fatal("refused compilation leaked dispatchable task",mutation)}
		})
	}
	backgroundCleanup := BackgroundCleanupPreviewParams{ExposurePreviewParams: preview, Kind: "background_cleanup", ModelID: "big_lama", ContextPixels: 128, FeatherPixels: 8}
	backgroundCleanupIntent, _ := canonical.Marshal(v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "background_cleanup",
		Reference: source, ReferenceMask: maskRef, TargetSnapshotID: preview.SnapshotID, Strength: .5, PreservePersons: true})
	backgroundCleanup.Intent = hashRef(backgroundCleanupIntent)
	backgroundCleanupRun := uuid.New()
	backgroundCleanupTask, backgroundCleanupParams, backgroundCleanupClosure, backgroundCleanupErr := CompileBackgroundCleanupPreviewInventory(backgroundCleanupRun, backgroundCleanup, candidateBody, backgroundCleanupIntent, docs)
	if backgroundCleanupErr != nil || backgroundCleanupTask != uuid.NewSHA1(backgroundCleanupRun, []byte("semantic.preview_background_cleanup:"+backgroundCleanup.TargetID.String())) || backgroundCleanupClosure.Open == nil || backgroundCleanupParams.ArtifactID != uuid.NewSHA1(backgroundCleanupTask, []byte("Params")) {
		t.Fatal("backgroundCleanup inventory binding", backgroundCleanupErr)
	}
	if _, _, _, err := CompileBackgroundCleanupPreviewInventory(backgroundCleanupRun, backgroundCleanup, candidateBody, intentBody, docs); err == nil {
		t.Fatal("exposure intent admitted backgroundCleanup")
	}
	backgroundCleanupPin := BackgroundCleanupQualificationPin(pin)
	backgroundCleanupPin.Backend = "mps"
	backgroundCleanup.ModelLockSHA = pin.ModelLockSHA
	backgroundCleanup.RuntimeLockSHA = pin.RuntimeLockSHA
	backgroundCleanup.DeviceProfileSHA = pin.DeviceProfileSHA
	backgroundCleanupLimits := ExposureResourceEstimate{RAMBytes: 1 << 40, VRAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	frozenBackgroundCleanup, compileErr := CompileBackgroundCleanupPreviewTask(backgroundCleanupRun, backgroundCleanup, candidateBody, backgroundCleanupIntent, docs,
		ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}, backgroundCleanupLimits, backgroundCleanupPin, 12000000000)
	if compileErr != nil {
		t.Fatal("backgroundCleanup task compilation", compileErr)
	}
	backgroundCleanupDraft, compileErr := frozenBackgroundCleanup.DraftTask()
	if compileErr != nil || backgroundCleanupDraft.ID != backgroundCleanupTask || backgroundCleanupDraft.Operation != "semantic.preview_background_cleanup" {
		t.Fatal("backgroundCleanup task projection", compileErr)
	}

	// Compose own shown-copy preparation with the exact source/intent/candidate
	// documents above. Physical verification is a fixture precondition here;
	// this scenario is not object-store or SQL admission evidence.
	for _, mode := range []string{"valid", "revoked_before", "revoked_after", "factory_error"} {
		copyChild, copyApproved, copySelected, copyPhysical := shownBackgroundCleanupCompileFixture(t)
		copyEdit := ApprovedEdit{SchemaVersion: 1, TargetID: backgroundCleanup.TargetID, Source: backgroundCleanup.Source,
			Intent: backgroundCleanup.Intent, Candidate: backgroundCleanup.Candidate, Mask: maskRef, Transform: edit.Transform,
			Kind: "background_cleanup", Strength: .5, PreservePersons: true}
		copyBody, copyErr := canonical.Marshal(copyEdit)
		if copyErr != nil {
			t.Fatal(copyErr)
		}
		copyApproved = ApprovedTaskDraft{TargetID: copyEdit.TargetID, Params: hashRef(copyBody), Body: copyBody, Kind: "background_cleanup"}
		copyPhysical.linkage.Presentation.Params = backgroundCleanup
		copySelected.TargetID = backgroundCleanup.TargetID
		copyPhysical.linkage.Document.TaskID = uuid.NewSHA1(copySelected.PreviewRunID, []byte("semantic.preview_background_cleanup:"+backgroundCleanup.TargetID.String()))
		_, copySHA, copyErr := FreezeBackgroundCleanupPreviewParams(backgroundCleanup)
		if copyErr != nil {
			t.Fatal(copyErr)
		}
		copySelected.Receipt.TaskID = copyPhysical.linkage.Document.TaskID
		copySelected.Receipt.ParamsSHA = copySHA
		// Bundle publication requires byte/hash ownership of these documents. The
		// production physical reader checks their full schemas before this boundary.
		for _, rawTarget := range []*[]byte{&copyPhysical.linkage.Document.raw, &copyPhysical.linkage.Evidence.raw, &copyPhysical.image.ImageBytes} {
			*rawTarget = []byte(`{"schema_version":1}`)
		}
		copyPhysical.linkage.Document.Source.Ref = hashRef(copyPhysical.linkage.Document.raw)
		copyPhysical.linkage.Presentation.NumericalEvidence = hashRef(copyPhysical.linkage.Evidence.raw)
		copyPhysical.linkage.Presentation.WorkingImage = hashRef(copyPhysical.image.ImageBytes)
		copyPhysical.linkage.Presentation.Before = arbitrary()
		copyPhysical.linkage.Presentation.After = arbitrary()
		copyPhysical.linkage.Presentation.Affected = arbitrary()
		copyPhysical.linkage.Presentation.Protected = arbitrary()
		copySelected.Receipt.Presentation = copyPhysical.linkage.Document.Source.Ref
		copySelected.Receipt.NumericalEvidence = copyPhysical.linkage.Presentation.NumericalEvidence
		copySelected.Receipt.WorkingImage = copyPhysical.linkage.Presentation.WorkingImage
		recorder := &previewPublicationRecorder{}
		checks, factories := 0, 0
		preparedCopy, copyErr := PrepareShownBackgroundCleanupWithPublisher(context.Background(), copyChild, copyApproved, copySelected, copyPhysical, candidateBody, backgroundCleanupIntent, docs,
			ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}, backgroundCleanupLimits, pin,
			func(FrozenShownBackgroundCleanupTask) (v2input.Publisher, error) {
				factories++
				if mode == "factory_error" {
					return nil, ErrUnavailable
				}
				return recorder, nil
			},
			func(context.Context) error {
				checks++
				if mode == "revoked_before" && checks == 1 || mode == "revoked_after" && checks == 2 {
					return ErrConflict
				}
				return nil
			})
		if mode == "valid" {
			var node v2input.DraftTask
			if copyErr != nil || json.Unmarshal(preparedCopy.payload.Node, &node) != nil || node.Operation != "semantic.adopt_shown_background_cleanup" || node.ResourceClass != "cpu" || len(preparedCopy.documents) == 0 || checks != 2 || factories != 1 {
				t.Fatal("shown cleanup preparation", copyErr, node, checks, factories)
			}
			batchReview := cleanupReviewForSource(t, candidateBody, backgroundCleanupIntent)
			batchParams := copyPhysical.linkage.Presentation.Params
			batchParams.Plan, batchParams.Intent = batchReview.PlanRef, batchReview.IntentRef
			batchParams.WorkflowRevision, batchParams.TargetRevision = "1", "1"
			for _, document := range batchReview.documents {
				if document.Source.Contract == "TransferCandidate" {
					batchParams.Candidate = document.Source.Ref
				}
			}
			copyPhysical.linkage.Presentation.Params = batchParams
			_, copySelected.Receipt.ParamsSHA, copyErr = FreezeBackgroundCleanupPreviewParams(batchParams)
			if copyErr != nil {
				t.Fatal(copyErr)
			}
			candidateID := batchParams.Candidate.ArtifactID
			batchTargets := []LockedTarget{{ID: batchParams.TargetID, AssetVersionID: batchParams.Source.AssetVersionID, CandidateArtifactID: &candidateID, State: "proposed", Revision: 1}}
			batchRequest := ApplyDecision{ExpectedRevision: "1", PlanSHA: batchReview.PlanRef.SHA256, TargetDecisions: []TargetDecision{{TargetID: batchParams.TargetID, Decision: "approve", Preview: &PreviewSelection{
				Render:   TargetDocument{ArtifactID: copySelected.Receipt.WorkingImage.ArtifactID, SHA256: copySelected.Receipt.WorkingImage.SHA256},
				Evidence: TargetDocument{ArtifactID: copySelected.Receipt.Presentation.ArtifactID, SHA256: copySelected.Receipt.Presentation.SHA256}}}}}
			batchInput := ShownBackgroundCleanupPreparationInput{Selected: copySelected, Physical: copyPhysical, Candidate: candidateBody, Intent: backgroundCleanupIntent, Documents: docs,
				Owner: ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}, Limits: backgroundCleanupLimits, Qualification: pin}
			batchActor := uuid.New()
			batchRecorder := &previewPublicationRecorder{}
			approvedCalls, decisionCalls, batchChecks := 0, 0, 0
			batchResult, batchErr := prepareShownBackgroundCleanupBatch(context.Background(), batchParams.TenantID, batchParams.IntentID, copyChild, batchReview, batchTargets, batchRequest, []ShownBackgroundCleanupPreparationInput{batchInput},
				func(context.Context) error { batchChecks++; return nil }, cleanupBatchPublication{
					approved: func(context.Context, ApplyDecision) error { approvedCalls++; return nil },
					copyPublisher: func(ShownBackgroundCleanupPreparationInput, FrozenShownBackgroundCleanupTask) (v2input.Publisher, error) {
						if approvedCalls != 1 {
							t.Fatal("copy before approved publication")
						}
						return batchRecorder, nil
					},
					decision: func(_ context.Context, owned ApplyDecision) (PublishedDecision, error) {
						decisionCalls++
						if len(batchRecorder.calls) == 0 {
							t.Fatal("decision before copy publication")
						}
						proof, err := BuildDecisionProof(batchReview, batchTargets, owned)
						return PublishedDecision{BatchID: decisionDocumentOwner(batchParams.TenantID, batchReview.Sources.RunID, batchActor, proof.RequestSHA256), Ref: hashRef(proof.Body), Proof: proof}, err
					},
				})
			if batchErr != nil || approvedCalls != 1 || decisionCalls != 1 || batchChecks != 5 || len(batchResult.ShownTasks) != 1 || batchResult.Child != copyChild {
				t.Fatal("full cleanup batch preparation", batchErr, approvedCalls, decisionCalls, batchChecks)
			}
			commitInputs, commitErr := PrepareShownBackgroundCleanupBatchCommitInputs(batchParams.TenantID, batchParams.IntentID, batchActor, copyChild, batchReview, batchTargets, batchRequest, batchResult.Decision, batchResult.ShownTasks)
			if commitErr != nil || len(commitInputs.Documents) == 0 || len(commitInputs.Tasks) == 0 {
				t.Fatal("batch commit projection", commitErr)
			}
			brokenInput := batchInput
			brokenInput.Physical.image.ImageBytes = []byte(`{"schema_version":2}`)
			publicationCalls := 0
			broken, brokenErr := prepareShownBackgroundCleanupBatch(context.Background(), batchParams.TenantID, batchParams.IntentID, copyChild, batchReview, batchTargets, batchRequest, []ShownBackgroundCleanupPreparationInput{brokenInput},
				func(context.Context) error { publicationCalls++; return nil }, cleanupBatchPublication{
					approved: func(context.Context, ApplyDecision) error { publicationCalls++; return nil },
					copyPublisher: func(ShownBackgroundCleanupPreparationInput, FrozenShownBackgroundCleanupTask) (v2input.Publisher, error) {
						publicationCalls++
						return nil, ErrInvalid
					},
					decision: func(context.Context, ApplyDecision) (PublishedDecision, error) {
						publicationCalls++
						return PublishedDecision{}, ErrInvalid
					},
				})
			if brokenErr == nil || publicationCalls != 0 || broken.Child != uuid.Nil {
				t.Fatal("invalid raw bundle reached publication", brokenErr, publicationCalls)
			}
			for failCheck := 1; failCheck <= 5; failCheck++ {
				checks, approvedCount, decisionCount := 0, 0, 0
				failedRecorder := &previewPublicationRecorder{}
				failed, failure := prepareShownBackgroundCleanupBatch(context.Background(), batchParams.TenantID, batchParams.IntentID, copyChild, batchReview, batchTargets, batchRequest, []ShownBackgroundCleanupPreparationInput{batchInput},
					func(context.Context) error {
						checks++
						if checks == failCheck {
							return ErrConflict
						}
						return nil
					}, cleanupBatchPublication{
						approved: func(context.Context, ApplyDecision) error { approvedCount++; return nil },
						copyPublisher: func(ShownBackgroundCleanupPreparationInput, FrozenShownBackgroundCleanupTask) (v2input.Publisher, error) {
							return failedRecorder, nil
						},
						decision: func(_ context.Context, owned ApplyDecision) (PublishedDecision, error) {
							decisionCount++
							proof, err := BuildDecisionProof(batchReview, batchTargets, owned)
							return PublishedDecision{BatchID: decisionDocumentOwner(batchParams.TenantID, batchReview.Sources.RunID, batchActor, proof.RequestSHA256), Ref: hashRef(proof.Body), Proof: proof}, err
						},
					})
				if failure != ErrConflict || failed.Child != uuid.Nil || len(failed.ShownTasks) != 0 || checks != failCheck {
					t.Fatal("batch recheck refusal escaped", failCheck, failure, checks)
				}
				if failCheck == 1 && approvedCount != 0 || failCheck <= 2 && len(failedRecorder.calls) != 0 || failCheck <= 4 && decisionCount != 0 {
					t.Fatal("publication after authority refusal", failCheck, approvedCount, decisionCount)
				}
			}
			for _, failureMode := range []string{"approved", "factory", "copy", "decision", "cancel_before_approved", "cancel_before_decision", "cancel_copy"} {
				ctx, cancel := context.WithCancel(context.Background())
				checks, approvals, decisions, factories := 0, 0, 0, 0
				failedRecorder := &previewPublicationRecorder{}
				if failureMode == "copy" {
					failedRecorder.failure = "error"
				}
				if failureMode == "cancel_copy" {
					failedRecorder.cancel = cancel
				}
				failed, failure := prepareShownBackgroundCleanupBatch(ctx, batchParams.TenantID, batchParams.IntentID, copyChild, batchReview, batchTargets, batchRequest, []ShownBackgroundCleanupPreparationInput{batchInput},
					func(context.Context) error {
						checks++
						if failureMode == "cancel_before_approved" && checks == 1 || failureMode == "cancel_before_decision" && checks == 4 {
							cancel()
						}
						return nil
					}, cleanupBatchPublication{
						approved: func(context.Context, ApplyDecision) error {
							approvals++
							if failureMode == "approved" {
								return ErrUnavailable
							}
							return nil
						},
						copyPublisher: func(ShownBackgroundCleanupPreparationInput, FrozenShownBackgroundCleanupTask) (v2input.Publisher, error) {
							factories++
							if failureMode == "factory" {
								return nil, ErrUnavailable
							}
							return failedRecorder, nil
						},
						decision: func(context.Context, ApplyDecision) (PublishedDecision, error) {
							decisions++
							return PublishedDecision{}, ErrUnavailable
						},
					})
				cancel()
				if failure == nil || failed.Child != uuid.Nil || len(failed.ShownTasks) != 0 || failed.Decision.Ref.ArtifactID != uuid.Nil {
					t.Fatal("failed publication escaped", failureMode, failure)
				}
				if failureMode == "cancel_before_approved" && approvals != 0 || failureMode == "approved" && factories != 0 || failureMode == "factory" && len(failedRecorder.calls) != 0 {
					t.Fatal("publication continued after failure", failureMode)
				}
				if failureMode != "decision" && decisions != 0 {
					t.Fatal("decision published after failure", failureMode)
				}
			}
		} else {
			if copyErr == nil || preparedCopy.payload.Node != nil || preparedCopy.documents != nil {
				t.Fatal("failed copy preparation escaped", mode, copyErr)
			}
			if mode == "revoked_before" && (factories != 0 || len(recorder.calls) != 0) {
				t.Fatal("revoked copy published")
			}
			if mode == "factory_error" && len(recorder.calls) != 0 {
				t.Fatal("failed factory published")
			}
		}
	}
	// Exercise the real background cleanup source compiler through transport
	// publication. The publisher is a byte/hash recorder, not durable SQL/object IO.
	bgDependencies, _, _, bgErr := ResolveBackgroundCleanupPreviewDocuments(backgroundCleanup, candidateBody, backgroundCleanupIntent, docs)
	if bgErr != nil {
		t.Fatal(bgErr)
	}
	bgPublished, bgErr := PublishBackgroundCleanupPreviewTask(context.Background(), frozenBackgroundCleanup, &previewPublicationRecorder{})
	if bgErr != nil {
		t.Fatal("compiled cleanup publication", bgErr)
	}
	bgCommit, bgErr := PrepareBackgroundCleanupPreviewCommitInputs(context.Background(), frozenBackgroundCleanup, bgPublished)
	if bgErr != nil {
		t.Fatal("compiled cleanup commit projection", bgErr)
	}
	var bgInventory ExposureInventoryBinding
	var bgTransport ExposureTransportBinding
	var bgNode v2input.DraftTask
	if json.Unmarshal(bgCommit.Inventory, &bgInventory) != nil || json.Unmarshal(bgCommit.Transport, &bgTransport) != nil || json.Unmarshal(bgCommit.Node, &bgNode) != nil {
		t.Fatal("invalid cleanup transport projection")
	}
	if bgNode.ID != backgroundCleanupTask || bgNode.Operation != "semantic.preview_background_cleanup" || bgInventory.TaskID != bgNode.ID || bgTransport.TaskID != bgNode.ID || bgTransport.EntryCount != len(bgInventory.Entries) {
		t.Fatal("cleanup compiler identity lost at publication")
	}
	bgSeen := map[v2input.Ref]string{}
	for _, entry := range bgInventory.Entries {
		bgSeen[entry.Ref] = entry.Contract
	}
	bgRequired := []v2input.Entry{
		{Ref: frozenBackgroundCleanup.frozen.params, Contract: "Params"},
		{Ref: backgroundCleanup.Plan, Contract: "SemanticPlan"},
		{Ref: backgroundCleanup.Intent, Contract: "EditIntent"},
		{Ref: backgroundCleanup.Candidate, Contract: "TransferCandidate"},
		{Ref: backgroundCleanup.Source.Source, Contract: "WorkingImage"},
		{Ref: backgroundCleanup.Protection, Contract: "MaskRef"},
		{Ref: maskRef, Contract: "MaskRef"}, {Ref: edit.Transform, Contract: "ResolvedTransform"},
		{Ref: bgDependencies.Reference, Contract: "WorkingImage"},
		{Ref: bgDependencies.Source.Pixels, Contract: "NpyArray"},
		{Ref: bgDependencies.ReferenceImage.Pixels, Contract: "NpyArray"},
		{Ref: bgDependencies.MaskPixels, Contract: "NpyArray"},
		{Ref: bgDependencies.ProtectionPixels, Contract: "NpyArray"},
	}
	for _, alpha := range []*v2input.Ref{bgDependencies.Source.Alpha, bgDependencies.ReferenceImage.Alpha} {
		if alpha != nil {
			bgRequired = append(bgRequired, v2input.Entry{Ref: *alpha, Contract: "NpyArray"})
		}
	}
	for _, ref := range bgDependencies.Geometry {
		bgRequired = append(bgRequired, v2input.Entry{Ref: ref, Contract: "NpyArray"})
	}
	for _, entry := range bgRequired {
		if bgSeen[entry.Ref] != entry.Contract {
			t.Fatal("cleanup compiled dependency omitted or misclassified", entry)
		}
	}
	bgCanonical, _, bgErr := FreezeBackgroundCleanupPreviewParams(backgroundCleanup)
	if bgErr != nil || string(bgCommit.ParamsJCS) != string(bgCanonical) || bgTransport.EntriesSHA != bgPublished.Graph.Built.SHA256 {
		t.Fatal("cleanup compiler byte/digest identity lost", bgErr)
	}
	deps, width, height, previewErr := ResolveExposurePreviewDocuments(preview, candidateBody, intentBody, docs)
	if previewErr != nil || width != int64(image.Canvas.Width) || height != int64(image.Canvas.Height) || deps.Protection != protection {
		t.Fatal("preview source projection", previewErr)
	}
	previewRun := uuid.New()
	previewTask, previewParams, closure, inventoryErr := CompileExposurePreviewInventory(previewRun, preview, candidateBody, intentBody, docs)
	if inventoryErr != nil || previewTask != uuid.NewSHA1(previewRun, []byte("semantic.preview_local_exposure:"+preview.TargetID.String())) || previewParams.ArtifactID != uuid.NewSHA1(previewTask, []byte("Params")) {
		t.Fatal("preview inventory", inventoryErr)
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[v2input.Ref]bool{}
	for {
		entry, ok, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		seen[entry.Ref] = true
	}
	for _, required := range []v2input.Ref{previewParams, preview.Plan, preview.Intent, preview.Candidate, preview.Source.Source, preview.Protection, maskRef, edit.Transform} {
		if !seen[required] {
			t.Fatal("missing preview dependency", required)
		}
	}
	_, _, replay, err := CompileExposurePreviewInventory(previewRun, preview, candidateBody, intentBody, docs)
	if err != nil || replay.InputDigest != closure.InputDigest {
		t.Fatal("unstable inventory")
	}
	if _, _, _, err := CompileExposurePreviewInventory(preview.WorkflowID, preview, candidateBody, intentBody, docs); err == nil {
		t.Fatal("preview reused parent namespace")
	}
	for _, failure := range []string{"candidate", "intent", "source", "protection", "snapshot"} {
		p, c, i, changed := preview, candidateBody, intentBody, docs
		switch failure {
		case "candidate":
			c = append(append([]byte(nil), c...), ' ')
		case "intent":
			i = append(append([]byte(nil), i...), ' ')
		case "source":
			changed.Source = append(append([]byte(nil), imageBody...), ' ')
		case "protection":
			changed.Protection = maskBody
		case "snapshot":
			p.SnapshotID = uuid.New()
		}
		if _, w, h, err := ResolveExposurePreviewDocuments(p, c, i, changed); err == nil || w != 0 || h != 0 {
			t.Fatal("substituted preview input accepted", failure)
		}
	}
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	preview.ModelLockSHA, preview.RuntimeLockSHA, preview.DeviceProfileSHA = pin.ModelLockSHA, pin.RuntimeLockSHA, pin.DeviceProfileSHA
	frozenPreview, err := CompileExposurePreviewTask(previewRun, preview, candidateBody, intentBody, docs, owner, limits, pin)
	if err != nil {
		t.Fatal("preview task compilation", err)
	}
	previewDraft, err := frozenPreview.DraftTask()
	if err != nil || previewDraft.Operation != "semantic.preview_local_exposure" || previewDraft.ResourceClass != "cpu" {
		t.Fatal("preview draft", err)
	}
	originalProfile := string(previewDraft.ExecutionProfile)
	previewDraft.ExecutionProfile[0] = '!'
	ownedDraft, err := frozenPreview.DraftTask()
	if err != nil || string(ownedDraft.ExecutionProfile) != originalProfile {
		t.Fatal("mutable frozen profile")
	}
	previewObjects := &forbiddenExposureObjects{}
	productionPublisher, err := NewExposurePreviewPublisher(&pgxpool.Pool{}, previewObjects, "fixture", uuid.New(), uuid.New(), previewRun, frozenPreview)
	if err != nil {
		t.Fatal("preview publisher scope", err)
	}
	for _, wrong := range []struct {
		task     uuid.UUID
		ordinal  int
		contract string
		body     []byte
	}{
		{uuid.New(), 0, "Params", frozenPreview.closure.ParamsJCS},
		{previewDraft.ID, 1, "Params", frozenPreview.closure.ParamsJCS},
		{previewDraft.ID, 0, "Params", []byte(`{}`)},
		{previewDraft.ID, -1, "InputClosureChunk", []byte(`{}`)},
		{previewDraft.ID, 0, "InputClosureManifest", []byte(`{}`)},
		{previewDraft.ID, 0, "ApprovedSemanticEdit", []byte(`{}`)},
	} {
		if _, err := productionPublisher.Publish(context.Background(), wrong.task, wrong.ordinal, wrong.contract, wrong.body); err == nil {
			t.Fatal("invalid preview publication accepted")
		}
	}
	if previewObjects.calls != 0 {
		t.Fatal("invalid scope reached object IO")
	}
	if _, err := NewExposurePreviewPublisher(&pgxpool.Pool{}, previewObjects, "fixture", uuid.New(), uuid.New(), uuid.New(), frozenPreview); err == nil {
		t.Fatal("wrong preview namespace accepted")
	}
	var previewProfile ExposureExecutionProfile
	if json.Unmarshal(ownedDraft.ExecutionProfile, &previewProfile) != nil || previewProfile.Backend != "cpu" || previewProfile.RendererVersion != "v2.2" || previewProfile.Qualification != pin {
		t.Fatal("preview qualification profile lost")
	}
	if previewProfile.ResourceEstimate.RAMBytes < 512*int64(image.Canvas.Width)*int64(image.Canvas.Height)+64*1024*1024+owner.RuntimeRAMBytes {
		t.Fatal("preview compute admission underestimated")
	}
	previewPublisher := &exposureTransportPublisher{}
	published, err := PublishExposurePreviewInputs(context.Background(), frozenPreview, previewPublisher)
	if err != nil || published.Draft.ID != previewDraft.ID || published.Params.ArtifactID != previewParams.ArtifactID || previewPublisher.calls < 2 {
		t.Fatal("preview input publication", err)
	}
	published.Graph.Open = nil // Verification must replay the private compiler cursor.
	verifiedPreview, err := VerifyExposurePreviewPublication(context.Background(), frozenPreview, published)
	if err != nil || verifiedPreview.Draft.ID != previewTask || verifiedPreview.Graph.Open == nil {
		t.Fatal("preview publication verification", err)
	}
	for _, field := range []string{"params", "digest", "profile", "manifest", "direct"} {
		changed := published
		switch field {
		case "params":
			changed.Params.SHA256 = strings.Repeat("0", 64)
		case "digest":
			changed.Draft.InputDigest = strings.Repeat("0", 64)
		case "profile":
			changed.Draft.ExecutionProfile = json.RawMessage(`{}`)
		case "manifest":
			changed.Graph.Built.Manifest.SHA256 = strings.Repeat("0", 64)
		case "direct":
			changed.Graph.Direct = nil
		}
		if _, err := VerifyExposurePreviewPublication(context.Background(), frozenPreview, changed); err == nil {
			t.Fatal("changed preview publication accepted", field)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	beforeCalls := previewPublisher.calls
	if _, err := PublishExposurePreviewInputs(ctx, frozenPreview, previewPublisher); err == nil || previewPublisher.calls != beforeCalls {
		t.Fatal("cancelled preview published")
	}
	if _, err := PublishExposurePreviewInputs(context.Background(), frozenPreview, &exposureTransportPublisher{bad: true}); err == nil {
		t.Fatal("changed publication SHA accepted")
	}
	resolved, resolveErr := ResolveExposureTaskInputsFromDocuments(draft, source, protection, docs, owner, limits, pin)
	if resolveErr != nil || resolved.Source != source || resolved.Reference != source || resolved.Dependencies.Source.Pixels.ArtifactID != image.Pixels.ID || resolved.Dependencies.Protection != protection {
		t.Fatal("document input projection", resolveErr)
	}
	badDraft := draft
	badDraft.Params.SHA256 = strings.Repeat("0", 64)
	if _, err := ResolveExposureTaskInputsFromDocuments(badDraft, source, protection, docs, owner, limits, pin); err == nil {
		t.Fatal("changed Params pin accepted")
	}
	compiled, err := CompileExposureTaskFromDocuments(child, draft, source, protection, docs, owner, limits, pin)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := compiled.DraftTask()
	var profile ExposureExecutionProfile
	json.Unmarshal(node.ExecutionProfile, &profile)
	if profile.ResourceEstimate.PixelCount != int64(image.Canvas.Width)*int64(image.Canvas.Height) {
		t.Fatal("dimensions not derived")
	}
	for _, field := range []string{"source", "reference", "mask", "protection"} {
		changed := docs
		switch field {
		case "source":
			changed.Source = append(append([]byte(nil), imageBody...), ' ')
		case "reference":
			changed.Reference = append(append([]byte(nil), imageBody...), ' ')
		case "mask":
			changed.Mask = protectionBody
		case "protection":
			changed.Protection = maskBody
		}
		if _, err := CompileExposureTaskFromDocuments(child, draft, source, protection, changed, owner, limits, pin); err == nil {
			t.Fatal("substituted document accepted", field)
		}
	}
	if _, err := CompileExposureTaskFromDocuments(child, draft, source, maskRef, ExposureSourceDocuments{Source: imageBody, Reference: imageBody, Mask: maskBody, Protection: maskBody}, owner, limits, pin); err == nil {
		t.Fatal("unprotected root accepted")
	}
}

func TestExposureDocumentBatchRequiresExactApprovalCoverage(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	child := uuid.New()
	for _, documents := range [][]ExposureTargetDocuments{nil, {{TargetID: uuid.New()}}, {{TargetID: targets[0].ID}, {TargetID: targets[0].ID}}} {
		result, err := ResolveApprovedExposureDocumentInputs(child, prepared, targets, request, documents)
		if err == nil || result != nil {
			t.Fatal("invalid batch document coverage accepted")
		}
	}
	request.TargetDecisions[0].Decision = "reject"
	result, err := ResolveApprovedExposureDocumentInputs(child, prepared, targets, request, nil)
	if err != nil || len(result) != 0 {
		t.Fatal("rejection resolved image documents", err)
	}
}
