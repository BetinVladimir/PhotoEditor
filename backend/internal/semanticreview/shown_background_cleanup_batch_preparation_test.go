package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestShownBackgroundCleanupBatchPreparationCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PrepareShownBackgroundCleanupApplyBatch(ctx, nil, nil, "owned", auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil,
		PreparedReview{}, nil, ApplyDecision{}, nil, nil)
	if err != context.Canceled {
		t.Fatal(err)
	}
}

func TestShownBackgroundCleanupBatchPreparationRejectsMissingPublicationDependencies(t *testing.T) {
	_, err := PrepareShownBackgroundCleanupApplyBatch(context.Background(), nil, nil, "owned", auth.Identity{}, uuid.New(), uuid.New(), uuid.New(),
		PreparedReview{}, nil, ApplyDecision{}, nil, func(context.Context) error { t.Fatal("unexpected authority callback"); return nil })
	if err == nil {
		t.Fatal("missing dependencies accepted")
	}
}

func TestCleanupBatchCoreRejectsInvalidDecisionBeforePublication(t *testing.T) {
	calls := 0
	publication := cleanupBatchPublication{
		approved: func(context.Context, ApplyDecision) error { calls++; return nil },
		copyPublisher: func(ShownBackgroundCleanupPreparationInput, FrozenShownBackgroundCleanupTask) (v2input.Publisher, error) {
			calls++
			return nil, ErrInvalid
		},
		decision: func(context.Context, ApplyDecision) (PublishedDecision, error) {
			calls++
			return PublishedDecision{}, ErrInvalid
		},
	}
	result, err := prepareShownBackgroundCleanupBatch(context.Background(), uuid.New(), uuid.New(), uuid.New(), PreparedReview{}, nil,
		ApplyDecision{}, nil, func(context.Context) error { calls++; return nil }, publication)
	if err == nil || calls != 0 || result.Child != uuid.Nil || len(result.ShownTasks) != 0 {
		t.Fatal("invalid batch published", result, err, calls)
	}
}

func TestCleanupBatchCoreCancellationBeforePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := prepareShownBackgroundCleanupBatch(ctx, uuid.Nil, uuid.Nil, uuid.Nil, PreparedReview{}, nil, ApplyDecision{}, nil, nil, cleanupBatchPublication{})
	if err != context.Canceled || result.Child != uuid.Nil || len(result.ShownTasks) != 0 {
		t.Fatal("canceled batch escaped", result, err)
	}
}

// Reuse validated calibration bytes, but bind a fresh six-document review to
// the exact candidate/intent used by the physical-precondition fixture.
func cleanupReviewForSource(t *testing.T, candidateRaw, intentRaw []byte) PreparedReview {
	t.Helper()
	_, calibrationDocs, model, dataset := preparedDocumentsKindFixture(t, "background_cleanup")
	sources := PlanSources{RunID: uuid.New(), TaskID: uuid.New(), ResultID: uuid.New(), Revision: 1}
	var documents []PlanDocument
	add := func(contract string, raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		source := PlanSource{Contract: contract, Ref: ref, Bytes: int64(len(raw)), Bucket: "bucket", Key: ref.ArtifactID.String(), Version: "v1", MediaType: "application/json"}
		sources.Objects = append(sources.Objects, source)
		documents = append(documents, PlanDocument{Source: source, Body: append([]byte(nil), raw...)})
		return ref
	}
	marshal := func(value any) []byte {
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var intent v2input.SemanticEditIntent
	var candidate v2input.SemanticCandidate
	if json.Unmarshal(intentRaw, &intent) != nil || json.Unmarshal(candidateRaw, &candidate) != nil {
		t.Fatal("source documents")
	}
	intentRef := add("EditIntent", intentRaw)
	candidateRef := add("TransferCandidate", candidateRaw)
	var calibration v2input.Ref
	for _, doc := range calibrationDocs {
		if doc.Source.Contract == "Calibration" {
			calibration = add("Calibration", doc.Body)
		}
	}
	snapshot, snapshotRaw, err := v2input.CompilePinnedSessionInputSnapshot(intent.TargetSnapshotID, []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: candidate.Target, WorkingImage: candidate.Target.Source, Recipe: intentRef}})
	if err != nil {
		t.Fatal(err)
	}
	add("InputSnapshot", snapshotRaw)
	add("Params", marshal(v2input.SemanticAssembleParams{SchemaVersion: 1, Intent: intentRef, Candidates: []v2input.Ref{candidateRef}, ModelLockSHA: model, Calibration: calibration}))
	add("SemanticPlan", marshal(v2input.SemanticPlan{SchemaVersion: 1, IntentSHA: intentRef.SHA256, SnapshotDigest: snapshot.SnapshotDigest, Candidates: []v2input.SemanticCandidate{candidate}, ModelLockSHA: model, Calibration: calibration}))
	prepared, err := PrepareReviewDocuments(sources, documents, model, dataset)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}
