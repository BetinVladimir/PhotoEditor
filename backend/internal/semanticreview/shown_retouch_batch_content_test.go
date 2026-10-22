package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Builds review and preview from the same candidate/intent/source. This remains
// a content/orchestration fixture, not SQL or physical-reader qualification.
func shownRetouchBatchContentFixture(t *testing.T) (shownRetouchContentFixture, PreparedReview, []LockedTarget, ApplyDecision) {
	t.Helper()
	f := fullShownRetouchContentFixture(t)
	p := f.physical.linkage.Presentation.Params
	_, template, model, dataset := preparedDocumentsKindFixture(t, "retouch_region")
	sources := PlanSources{RunID: p.WorkflowID, TaskID: uuid.New(), ResultID: uuid.New(), Revision: 1}
	var documents []PlanDocument
	encode := func(value any) []byte {
		body, err := canonical.Marshal(value)
		if err != nil { t.Fatal(err) }
		return body
	}
	ref := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	add := func(contract string, body []byte, identity v2input.Ref) {
		source := PlanSource{Contract: contract, Ref: identity, Bytes: int64(len(body)), Bucket: "owned", Key: identity.ArtifactID.String(), Version: "v1", MediaType: "application/json"}
		sources.Objects = append(sources.Objects, source)
		documents = append(documents, PlanDocument{Source: source, Body: body})
	}
	var calibration v2input.Ref
	for _, document := range template {
		if document.Source.Contract == "Calibration" {
			calibration = document.Source.Ref
			add("Calibration", document.Body, calibration)
		}
	}
	snapshot, snapshotRaw, err := v2input.CompilePinnedSessionInputSnapshot(p.SnapshotID, []v2input.SessionSnapshotItem{{
		Ordinal: 0, Binding: p.Source, WorkingImage: p.Source.Source, Recipe: p.Source.Source,
	}})
	if err != nil { t.Fatal(err) }
	add("InputSnapshot", snapshotRaw, ref(snapshotRaw))
	add("EditIntent", f.intent, p.Intent)
	add("TransferCandidate", f.candidate, p.Candidate)
	var candidate v2input.SemanticCandidate
	if err := json.Unmarshal(f.candidate, &candidate); err != nil { t.Fatal(err) }
	params := v2input.SemanticAssembleParams{SchemaVersion: 1, Intent: p.Intent, Candidates: []v2input.Ref{p.Candidate}, Calibration: calibration, ModelLockSHA: model}
	paramsRaw := encode(params)
	add("Params", paramsRaw, ref(paramsRaw))
	plan := v2input.SemanticPlan{SchemaVersion: 1, IntentSHA: p.Intent.SHA256, SnapshotDigest: snapshot.SnapshotDigest,
		Candidates: []v2input.SemanticCandidate{candidate}, ModelLockSHA: model, Calibration: calibration}
	planRaw := encode(plan)
	p.Plan = ref(planRaw)
	add("SemanticPlan", planRaw, p.Plan)
	review, err := PrepareReviewDocuments(sources, documents, model, dataset)
	if err != nil { t.Fatal("aligned review", err) }
	p.WorkflowRevision, p.TargetRevision = "1", "1"
	f.physical.linkage.Presentation.Params = p
	var evidence RetouchPreviewEvidence
	if err := json.Unmarshal(f.physical.linkage.Evidence.raw, &evidence); err != nil { t.Fatal(err) }
	evidence.Params = p
	f.physical.linkage.Evidence.raw = encode(evidence)
	f.physical.linkage.Presentation.NumericalEvidence = ref(f.physical.linkage.Evidence.raw)
	f.physical.linkage.Document.raw = encode(f.physical.linkage.Presentation)
	f.physical.linkage.Document.Source.Ref = ref(f.physical.linkage.Document.raw)
	f.selected.Receipt.NumericalEvidence = f.physical.linkage.Presentation.NumericalEvidence
	f.selected.Receipt.Presentation = f.physical.linkage.Document.Source.Ref
	_, f.selected.Receipt.ParamsSHA, err = FreezeRetouchPreviewParams(p)
	if err != nil { t.Fatal(err) }
	candidateID := p.Candidate.ArtifactID
	targets := []LockedTarget{{ID: p.TargetID, AssetVersionID: p.Source.AssetVersionID, CandidateArtifactID: &candidateID, State: "proposed", Revision: 1}}
	request := ApplyDecision{PlanSHA: p.Plan.SHA256, ExpectedRevision: "1", TargetDecisions: []TargetDecision{{TargetID: p.TargetID, Decision: "approve", Preview: &PreviewSelection{
		Render: TargetDocument{ArtifactID: f.selected.Receipt.WorkingImage.ArtifactID, SHA256: f.selected.Receipt.WorkingImage.SHA256},
		Evidence: TargetDocument{ArtifactID: f.selected.Receipt.Presentation.ArtifactID, SHA256: f.selected.Receipt.Presentation.SHA256},
	}}}}
	drafts, err := CompileApprovedTaskDrafts(f.child, review, targets, request)
	if err != nil || len(drafts) != 1 { t.Fatal("aligned draft", err) }
	f.approved = drafts[0]
	return f, review, targets, request
}

func TestShownRetouchBatchRefusesEveryLostAuthorityFence(t *testing.T) {
	for failAt := 1; failAt <= 6; failAt++ {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			f, review, targets, request := shownRetouchBatchContentFixture(t)
			p := f.physical.linkage.Presentation.Params
			input := ShownRetouchPreparationInput{Selected: f.selected, Physical: f.physical, Candidate: f.candidate, Intent: f.intent,
				Documents: f.docs, Owner: f.owner, Limits: f.limits, Qualification: f.pin}
			checks, approved, copies, decisions := 0, 0, 0, 0
			lost := errors.New("current selection authority lost")
			publication := retouchBatchPublication{
				approved: func(context.Context, ApplyDecision) error { approved++; return nil },
				copyPublisher: func(ShownRetouchPreparationInput, FrozenShownRetouchTask) (v2input.Publisher, error) {
					copies++
					return &previewPublicationRecorder{}, nil
				},
				decision: func(context.Context, ApplyDecision) (PublishedDecision, error) {
					decisions++
					return PublishedDecision{BatchID: uuid.New()}, nil
				},
			}
			result, err := prepareShownRetouchBatch(context.Background(), p.TenantID, p.IntentID, f.child, review, targets, request,
				[]ShownRetouchPreparationInput{input}, func(context.Context) error {
					checks++
					if checks == failAt { return lost }
					return nil
				}, publication)
			wantApproved, wantCopies, wantDecisions := 0, 0, 0
			if failAt > 1 { wantApproved = 1 }
			if failAt > 2 { wantCopies = 1 }
			if failAt > 5 { wantDecisions = 1 }
			if !errors.Is(err, lost) || checks != failAt || approved != wantApproved || copies != wantCopies || decisions != wantDecisions ||
				result.Child != uuid.Nil || len(result.ShownTasks) != 0 || result.Decision.BatchID != uuid.Nil {
				t.Fatal("authority loss escaped batch preparation", err, checks, approved, copies, decisions)
			}
		})
	}
}

func TestShownRetouchBatchPreparesCompleteOwnedContent(t *testing.T) {
	f, review, targets, request := shownRetouchBatchContentFixture(t)
	p := f.physical.linkage.Presentation.Params
	input := ShownRetouchPreparationInput{Selected: f.selected, Physical: f.physical, Candidate: f.candidate, Intent: f.intent,
		Documents: f.docs, Owner: f.owner, Limits: f.limits, Qualification: f.pin}
	approved, copies, decisions, checks := 0, 0, 0, 0
	want := PublishedDecision{BatchID: uuid.New()}
	publication := retouchBatchPublication{
		approved: func(context.Context, ApplyDecision) error { approved++; return nil },
		copyPublisher: func(ShownRetouchPreparationInput, FrozenShownRetouchTask) (v2input.Publisher, error) {
			if approved != 1 || decisions != 0 { t.Fatal("copy publication out of order") }
			copies++
			return &previewPublicationRecorder{}, nil
		},
		decision: func(context.Context, ApplyDecision) (PublishedDecision, error) {
			if approved != 1 || copies != 1 { t.Fatal("decision published before copy") }
			decisions++
			return want, nil
		},
	}
	result, err := prepareShownRetouchBatch(context.Background(), p.TenantID, p.IntentID, f.child, review, targets, request,
		[]ShownRetouchPreparationInput{input}, func(context.Context) error { checks++; return nil }, publication)
	if err != nil || result.Child != f.child || len(result.ShownTasks) != 1 || result.Decision.BatchID != want.BatchID || approved != 1 || copies != 1 || decisions != 1 || checks != 6 {
		t.Fatal("complete content preparation", err, approved, copies, decisions, checks)
	}
	if _, err := VerifyShownRetouchBatch(p.TenantID, p.IntentID, f.child, review, targets, request, result.ShownTasks); err != nil {
		t.Fatal("returned batch lost selected preview", err)
	}
}

func TestShownRetouchBatchPublicationFailuresReturnNoPreparedBatch(t *testing.T) {
	for _, mode := range []string{"approved", "factory", "nil_publisher", "storage", "sha", "identity", "decision"} {
		t.Run(mode, func(t *testing.T) {
			f, review, targets, request := shownRetouchBatchContentFixture(t)
			p := f.physical.linkage.Presentation.Params
			input := ShownRetouchPreparationInput{Selected: f.selected, Physical: f.physical, Candidate: f.candidate, Intent: f.intent,
				Documents: f.docs, Owner: f.owner, Limits: f.limits, Qualification: f.pin}
			failure := errors.New("publication failed")
			approved, factories, decisions, checks := 0, 0, 0, 0
			recorder := &previewPublicationRecorder{}
			switch mode {
			case "storage":
				recorder.failure = "error"
			case "sha", "identity":
				recorder.failure = mode
			}
			publication := retouchBatchPublication{
				approved: func(context.Context, ApplyDecision) error {
					approved++
					if mode == "approved" { return failure }
					return nil
				},
				copyPublisher: func(ShownRetouchPreparationInput, FrozenShownRetouchTask) (v2input.Publisher, error) {
					factories++
					if mode == "factory" { return nil, failure }
					if mode == "nil_publisher" { return nil, nil }
					return recorder, nil
				},
				decision: func(context.Context, ApplyDecision) (PublishedDecision, error) {
					decisions++
					if mode != "decision" { t.Fatal("decision reached after earlier publication failure") }
					return PublishedDecision{}, failure
				},
			}
			result, err := prepareShownRetouchBatch(context.Background(), p.TenantID, p.IntentID, f.child, review, targets, request,
				[]ShownRetouchPreparationInput{input}, func(context.Context) error { checks++; return nil }, publication)
			wantFactories, wantDecisions, wantChecks := 1, 0, 3
			switch mode {
			case "approved":
				wantFactories, wantChecks = 0, 1
			case "factory", "nil_publisher":
				wantChecks = 2
			case "decision":
				wantDecisions, wantChecks = 1, 5
			}
			if err == nil || approved != 1 || factories != wantFactories || decisions != wantDecisions || checks != wantChecks ||
				result.Child != uuid.Nil || len(result.ShownTasks) != 0 || result.Decision.BatchID != uuid.Nil {
				t.Fatal("publication failure escaped orchestration", err, approved, factories, decisions, checks)
			}
			if (mode == "approved" || mode == "factory" || mode == "decision") && !errors.Is(err, failure) {
				t.Fatal("original publisher failure lost", err)
			}
			if (mode == "approved" || mode == "factory" || mode == "nil_publisher") && len(recorder.calls) != 0 {
				t.Fatal("object publication continued after failure")
			}
		})
	}
}
