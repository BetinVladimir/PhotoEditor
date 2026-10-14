package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestPerspectiveLinesTransportSealRejectsIncompleteEvidenceBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SealPerspectiveLinesTransport(ctx, nil, PerspectiveLinesPlan{}, review.ArtifactRef{}, PerspectiveLinesClosure{}, VerifiedPerspectiveLinesInputs{}, "photos"); err != context.Canceled {
		t.Fatal(err)
	}
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
	for _, evidence := range []struct {
		c PerspectiveLinesClosure
		v VerifiedPerspectiveLinesInputs
	}{
		{PerspectiveLinesClosure{}, verified}, {closure, VerifiedPerspectiveLinesInputs{}}, {closure, verified},
	} {
		if err := SealPerspectiveLinesTransport(context.Background(), nil, f.plan, params, evidence.c, evidence.v, "photos"); err == nil {
			t.Fatal("missing evidence/database accepted")
		}
	}
}
