package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestPerspectiveLinesRegistrationRequiresSamePlanClosureAndVerifiedInputs(t *testing.T) {
	f := newLinesInputsFixture(t, true)
	verified, err := f.verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(f.plan.Params())}
	closure, err := BuildPerspectiveLinesClosure(context.Background(), f.plan, params, &linesClosurePublisher{})
	if err != nil {
		t.Fatal(err)
	}
	proof, witness, err := perspectiveLinesRegistrationProof(f.plan, params, closure, verified, "photos")
	if err != nil || len(proof) < 2 || len(witness) < 2 {
		t.Fatal("missing complete registration evidence", err)
	}
	if _, _, err := perspectiveLinesRegistrationProof(f.plan, params, PerspectiveLinesClosure{}, verified, "photos"); err == nil {
		t.Fatal("unpublished closure accepted")
	}
	if _, _, err := perspectiveLinesRegistrationProof(f.plan, params, closure, VerifiedPerspectiveLinesInputs{}, "photos"); err == nil {
		t.Fatal("unverified inputs accepted")
	}
	changed := f.plan
	changed.scope.Run = uuid.New()
	if _, _, err := perspectiveLinesRegistrationProof(changed, params, closure, verified, "photos"); err == nil {
		t.Fatal("foreign run accepted")
	}
	params.ID = uuid.New()
	if _, _, err := perspectiveLinesRegistrationProof(f.plan, params, closure, verified, "photos"); err == nil {
		t.Fatal("foreign Params accepted")
	}
}
func TestPerspectiveLinesRegistrationCancellationBeforeEvidenceAndDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RegisterPerspectiveLinesInputs(ctx, nil, PerspectiveLinesPlan{}, review.ArtifactRef{}, PerspectiveLinesClosure{}, VerifiedPerspectiveLinesInputs{}, "photos"); err != context.Canceled {
		t.Fatal(err)
	}
	if err := RegisterPerspectiveLinesInputs(context.Background(), nil, PerspectiveLinesPlan{}, review.ArtifactRef{}, PerspectiveLinesClosure{}, VerifiedPerspectiveLinesInputs{}, "photos"); err == nil {
		t.Fatal("empty evidence accepted")
	}
}
