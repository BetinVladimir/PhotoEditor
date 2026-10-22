package workflow

import (
	"bytes"
	"strings"
	"testing"
)

func TestPerspectiveLinesExecutionPolicyPinsRuntimeAndOwnsBytes(t *testing.T) {
	scope, source, icc := perspectiveLinesFixture(t, false)
	plan, err := CompilePerspectiveLinesPlan(scope, source, icc)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := CompilePerspectiveLinesExecutionPolicy(icc, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := policy.ForPlan(plan)
	if err != nil || renderDerivativeBytesSHA(raw) != policy.SHA256() {
		t.Fatal(err)
	}
	raw[0] = '!'
	if _, err := policy.ForPlan(plan); err != nil {
		t.Fatal("caller mutated policy", err)
	}
	other, err := CompilePerspectiveLinesExecutionPolicy(icc, strings.Repeat("b", 64))
	if err != nil || other.SHA256() == policy.SHA256() {
		t.Fatal("runtime not pinned", err)
	}
	foreign, _ := CompilePerspectiveLinesExecutionPolicy(strings.Repeat("f", 64), strings.Repeat("a", 64))
	if _, err := foreign.ForPlan(plan); err == nil {
		t.Fatal("foreign ICC accepted")
	}
	policy.body = bytes.ReplaceAll(policy.body, []byte(`"max_lines":128`), []byte(`"max_lines":129`))
	policy.sha = renderDerivativeBytesSHA(policy.body)
	if _, err := policy.ForPlan(plan); err == nil {
		t.Fatal("changed limits accepted")
	}
}

func TestPerspectiveLinesExecutionPolicyRejectsUnpinnedRuntime(t *testing.T) {
	for _, digest := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63), strings.Repeat("x", 64)} {
		if _, err := CompilePerspectiveLinesExecutionPolicy(strings.Repeat("a", 64), digest); err == nil {
			t.Fatal("invalid runtime accepted")
		}
		if _, err := CompilePerspectiveLinesExecutionPolicy(digest, strings.Repeat("a", 64)); err == nil {
			t.Fatal("invalid ICC accepted")
		}
	}
	scope, source, icc := perspectiveLinesFixture(t, false)
	plan, err := CompilePerspectiveLinesPlan(scope, source, icc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (PerspectiveLinesExecutionPolicy{}).ForPlan(plan); err == nil {
		t.Fatal("zero policy accepted")
	}
}
