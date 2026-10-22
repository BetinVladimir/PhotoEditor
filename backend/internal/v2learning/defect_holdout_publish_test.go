package v2learning

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type defectPublishRepo struct{ calls int }

func (r *defectPublishRepo) Reserve(_ context.Context,
	scope artifactjson.Scope, sha string, _ int64) (artifactjson.Reservation, error) {
	r.calls++
	ref, err := ReviewedDefectHoldoutRef(scope.Run, scope.Owner, sha)
	return artifactjson.Reservation{ArtifactID: ref.ArtifactID,
		Token: uuid.New()}, err
}

func (*defectPublishRepo) Complete(_ context.Context,
	_ artifactjson.Scope, ticket artifactjson.Reservation,
	object objectstore.Object) (artifactjson.Ref, error) {
	return artifactjson.Ref{ID: ticket.ArtifactID,
		SHA256: object.SHA256}, nil
}

func TestPublishReviewedDefectHoldoutRequiresExactEventManifestPin(t *testing.T) {
	ref := func(char string) review.ArtifactRef {
		return review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat(char, 64)}
	}
	snapshot := review.DefectSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(),
		RunID: uuid.New(), Revision: 1, State: "pending", Proposal: ref("a"),
		Image: ref("b"), LabelMap: ref("c"),
		CandidateIDs: []string{strings.Repeat("d", 64)}}
	actor := uuid.New()
	plan, err := review.PlanDefects(snapshot, actor, review.Decision{
		ExpectedRevision: 1, CandidateHash: snapshot.Proposal.SHA256,
		Decision: "accept", Accepted: snapshot.CandidateIDs})
	if err != nil {
		t.Fatal(err)
	}
	eventID, versionID, trainingRun := uuid.New(), uuid.New(), uuid.New()
	image := v2input.Ref{ArtifactID: snapshot.Image.ID,
		SHA256: snapshot.Image.SHA256}
	protected := v2input.Ref{ArtifactID: uuid.New(),
		SHA256: strings.Repeat("e", 64)}
	params := v2input.Ref{ArtifactID: uuid.New(),
		SHA256: strings.Repeat("f", 64)}
	decision := DefectHoldoutDecision{TenantID: snapshot.TenantID,
		ReviewID: snapshot.ReviewID, RunID: snapshot.RunID, ActorID: actor,
		Decision: "accept", Route: plan.Route, DecisionHash: plan.DecisionHash,
		Revision: plan.Revision, Selection: plan.Selection}
	document, body, sha, err := CompileFrozenDefectHoldoutEvidence(
		decision, eventID, versionID, image, protected, params)
	if err != nil {
		t.Fatal(err)
	}
	manifestRef, err := ReviewedDefectHoldoutRef(trainingRun, eventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	draft := TrainingDraftPrepared{TenantID: snapshot.TenantID,
		ActorID: uuid.New(), AuthSessionID: uuid.New(), RunID: trainingRun}
	draft.Manifest.Examples = []ManifestExample{{EventID: eventID,
		RenderCase: TrainingRenderCase{DefectEvidence: &manifestRef,
			Binding:      v2input.SessionVersionBinding{AssetVersionID: versionID},
			WorkingImage: image, Protected: protected}}}
	prepared := PreparedReviewedDefectHoldout{Document: document,
		Body: body, SHA256: sha}
	draft.RecoveryBody, err = canonical.Marshal(trainingDraftRecovery{
		SchemaVersion: 1, ManifestSHA: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	recoveredDraft, err := AttachReviewedDefectRecovery(draft,
		map[uuid.UUID]PreparedReviewedDefectHoldout{eventID: prepared})
	if err != nil {
		t.Fatal("reviewed evidence recovery not attached", err)
	}
	var recovery trainingDraftRecovery
	if err := json.Unmarshal(recoveredDraft.RecoveryBody, &recovery); err != nil {
		t.Fatal(err)
	}
	restored, err := reviewedDefectsFromRecovery(recoveredDraft,
		[]byte(recovery.DefectJCS))
	if err != nil || restored[eventID].SHA256 != sha ||
		string(restored[eventID].Body) != string(body) {
		t.Fatal("reviewed evidence recovery changed bytes", err)
	}
	repo := &defectPublishRepo{}
	publisher := artifactjson.Publisher{Repository: repo,
		Objects: frozenPublishObjects{}}
	got, err := PublishReviewedDefectHoldout(context.Background(),
		publisher, draft, prepared)
	if err != nil || got != manifestRef || repo.calls != 1 {
		t.Fatal("exact reviewed evidence not published", err)
	}
	draft.Manifest.Examples[0].RenderCase.DefectEvidence = &v2input.Ref{
		ArtifactID: uuid.New(), SHA256: manifestRef.SHA256}
	if _, err := PublishReviewedDefectHoldout(context.Background(),
		publisher, draft, prepared); err == nil || repo.calls != 1 {
		t.Fatal("foreign manifest evidence reached publisher")
	}
	if _, err := reviewedDefectsFromRecovery(draft,
		[]byte(recovery.DefectJCS)); err == nil {
		t.Fatal("foreign recovery manifest evidence accepted")
	}
}
