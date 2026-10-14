package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type recordingProvisioner struct {
	calls int
	limit int
}

func (p *recordingProvisioner) ProvisionNode(_ context.Context, _, _, _ string, limit int) (string, error) {
	p.calls++
	p.limit = limit
	return "gpu-node-3", nil
}

func TestApplyScaleUpRequiresOperatorAndLimit(t *testing.T) {
	request := ScaleUpRequest{Pool: "gpu", PolicySHA: strings.Repeat("a", 64),
		IdempotencyKey: uuid.NewString(), CurrentNodes: 2, MaximumNodes: 3,
		Decision: ScaleDecision{Delta: 1, Reason: "eligible_wait"}}
	provider := &recordingProvisioner{}
	if _, err := ApplyScaleUp(context.Background(), request, nil); err == nil {
		t.Fatal("unconfigured paid provisioner accepted")
	}
	request.LocalOnly = true
	if _, err := ApplyScaleUp(context.Background(), request, provider); err == nil || provider.calls != 0 {
		t.Fatal("local mode provisioned a node")
	}
	request.LocalOnly = false
	request.CurrentNodes = 3
	if _, err := ApplyScaleUp(context.Background(), request, provider); err == nil || provider.calls != 0 {
		t.Fatal("limit reached but node provisioned")
	}
	request.CurrentNodes = 2
	node, err := ApplyScaleUp(context.Background(), request, provider)
	if err != nil || node != "gpu-node-3" || provider.calls != 1 || provider.limit != 3 {
		t.Fatal(node, err, provider)
	}
}

func TestApplyScaleUpRejectsUnpinnedDecision(t *testing.T) {
	request := ScaleUpRequest{Pool: "gpu", PolicySHA: strings.Repeat("a", 64),
		IdempotencyKey: uuid.NewString(), CurrentNodes: 1, MaximumNodes: 2,
		Decision: ScaleDecision{Delta: 1, Reason: "eligible_wait"}}
	provider := &recordingProvisioner{}
	request.PolicySHA = strings.Repeat("A", 64)
	if _, err := ApplyScaleUp(context.Background(), request, provider); err == nil {
		t.Fatal("noncanonical policy accepted")
	}
	request.PolicySHA = strings.Repeat("a", 64)
	request.Decision.Delta = -1
	if _, err := ApplyScaleUp(context.Background(), request, provider); err == nil {
		t.Fatal("scale-down used provisioner")
	}
	request.Decision.Delta = 1
	request.IdempotencyKey = "not-a-uuid"
	if _, err := ApplyScaleUp(context.Background(), request, provider); err == nil {
		t.Fatal("unkeyed provision accepted")
	}
	if provider.calls != 0 {
		t.Fatal("invalid request reached provisioner")
	}
}
