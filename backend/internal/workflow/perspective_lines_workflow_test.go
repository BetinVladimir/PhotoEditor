package workflow

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPerspectiveLinesWorkflowProofRequiresPolicyAndVerifiedSameSource(t *testing.T) {
	f := newLinesInputsFixture(t, true)
	verified, err := f.verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := CompilePerspectiveLinesExecutionPolicy(f.plan.workingICC, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	raw, witness, err := perspectiveLinesWorkflowProof(f.plan, policy, verified, "photos")
	if err != nil || !bytes.Equal(raw, policy.Bytes()) || len(witness) < 2 {
		t.Fatal("lost policy or witness", err)
	}
	if _, _, err := perspectiveLinesWorkflowProof(f.plan, PerspectiveLinesExecutionPolicy{}, verified, "photos"); err == nil {
		t.Fatal("zero policy accepted")
	}
	if _, _, err := perspectiveLinesWorkflowProof(f.plan, policy, VerifiedPerspectiveLinesInputs{}, "photos"); err == nil {
		t.Fatal("unverified source accepted")
	}
	foreign, _ := CompilePerspectiveLinesExecutionPolicy(strings.Repeat("b", 64), strings.Repeat("a", 64))
	if _, _, err := perspectiveLinesWorkflowProof(f.plan, foreign, verified, "photos"); err == nil {
		t.Fatal("foreign ICC accepted")
	}
	changed := verified
	changed.scope.Run = uuid.New()
	if _, _, err := perspectiveLinesWorkflowProof(f.plan, policy, changed, "photos"); err == nil {
		t.Fatal("foreign run accepted")
	}
	// The object store DTO carries no bucket; the witness names the configured
	// bucket and installed SQL compares it to every authorized locator. This
	// pure compiler cannot prove an alternate bucket absent catalog evidence.
	if _, witness, err := perspectiveLinesWorkflowProof(f.plan, policy, verified, "other-bucket"); err != nil || !bytes.Contains(witness, []byte(`"bucket":"other-bucket"`)) {
		t.Fatal("configured bucket missing from SQL witness", err)
	}
}

func TestPerspectiveLinesWorkflowCancellationAndMissingDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CreatePerspectiveLinesWorkflow(ctx, nil, PerspectiveLinesPlan{}, PerspectiveLinesExecutionPolicy{}, VerifiedPerspectiveLinesInputs{}, "photos"); err != context.Canceled {
		t.Fatal(err)
	}
	f := newLinesInputsFixture(t, false)
	verified, err := f.verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := CompilePerspectiveLinesExecutionPolicy(f.plan.workingICC, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePerspectiveLinesWorkflow(context.Background(), nil, f.plan, policy, verified, "photos"); err == nil {
		t.Fatal("missing database accepted")
	}
}
