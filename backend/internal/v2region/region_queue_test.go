package v2region

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestRegionQueueUnavailableDependenciesAreNotInvalidRequest(t *testing.T) {
	queue := QueueService{}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, asset := uuid.New(), uuid.New()
	ticket, err := queue.Queue(context.Background(), who, tenant, asset, "region-ready", RegionPreviewRequest{})
	if !errors.Is(err, ErrRegionUnavailable) || ticket.RunID != uuid.Nil || ticket.WorkflowID != uuid.Nil {
		t.Fatal("dependency failure was not unavailable", ticket, err)
	}
	if _, err := queue.Queue(context.Background(), auth.Identity{}, tenant, asset, "region-ready", RegionPreviewRequest{}); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal("invalid identity classified as availability", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := queue.Queue(ctx, who, tenant, asset, "region-ready", RegionPreviewRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal("dependency checks masked cancellation", err)
	}
}

func TestRegionExecutionPolicyBoundsAndOwnsProviderBytes(t *testing.T) {
	raw := []byte("profile")
	policy := RegionExecutionPolicy{Profile: raw, PolicySHA: strings.Repeat("a", 64), ReservedBytes: 1}
	owned, err := ownRegionExecutionPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'X'
	if string(owned.Profile) != "profile" {
		t.Fatal("provider bytes alias queue policy")
	}
	for _, size := range []int{0, 65536, 65537} {
		policy.Profile = make([]byte, size)
		result, err := ownRegionExecutionPolicy(policy)
		if (size == 65536) != (err == nil) {
			t.Fatal("profile envelope bound", size, err)
		}
		if err != nil && len(result.Profile) != 0 {
			t.Fatal("invalid profile returned partial policy")
		}
	}
}

func TestQueuedRegionPolicyRequiresMeasuredProofAndExactQuota(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	policy, err := CompileRegionExecutionPolicy(plan, regionCostFixture(t, plan))
	if err != nil {
		t.Fatal(err)
	}
	valid, err := validateQueuedRegionPolicy(plan, policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Profile[0] = 'X'
	if _, err = validateQueuedRegionPolicy(plan, valid); err != nil {
		t.Fatal("provider bytes alias queued profile", err)
	}
	for _, mutate := range []func(*RegionExecutionPolicy){
		func(p *RegionExecutionPolicy) { p.PolicySHA = strings.Repeat("0", 64) },
		func(p *RegionExecutionPolicy) { p.ReservedBytes-- },
		func(p *RegionExecutionPolicy) { p.ReservedBytes++ },
	} {
		changed := valid
		mutate(&changed)
		out, err := validateQueuedRegionPolicy(plan, changed)
		if !errors.Is(err, ErrRegionInvalid) || len(out.Profile) != 0 {
			t.Fatal("unbound admission quota/policy", out, err)
		}
	}
	var fields map[string]any
	json.Unmarshal(valid.Profile, &fields)
	delete(fields, "region_qualification")
	missing, _ := canonical.Marshal(fields)
	out, err := validateQueuedRegionPolicy(plan, RegionExecutionPolicy{Profile: missing, PolicySHA: valid.PolicySHA, ReservedBytes: valid.ReservedBytes})
	if !errors.Is(err, ErrRegionUnavailable) || len(out.Profile) != 0 {
		t.Fatal("unqualified new task admitted", out, err)
	}
	fields["region_qualification"] = nil
	malformed, _ := canonical.Marshal(fields)
	if _, err = validateQueuedRegionPolicy(plan, RegionExecutionPolicy{Profile: malformed, PolicySHA: valid.PolicySHA, ReservedBytes: valid.ReservedBytes}); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal("malformed proof accepted", err)
	}
}
