package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestDecodePlanPreservesFailedOrdinalsAndWaits(t *testing.T) {
	spec, r := ingestFixture(t)
	compiled, _ := CompileIngest(spec, r)
	docs := []PublishedIngestNode{}
	for _, node := range compiled.Nodes {
		sum := sha256.Sum256(node.Params)
		docs = append(docs, PublishedIngestNode{Node: node, Params: review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}})
	}
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: spec.Items[0].Source.SHA256}
	outcomes := []InspectionOutcome{{Ordinal: 8, Verify: Succeeded, Inspect: Succeeded, MetadataRef: &ref, Metadata: &IngestMetadata{Source: spec.Items[0].Source, Width: 400, Height: 300, Format: "png"}}, {Ordinal: 2, Verify: Failed, Inspect: Skipped, InspectReason: "upstream_failed"}}
	plan, err := PlanIngestDecode(spec, docs, outcomes)
	if err != nil || !plan.Ready || len(plan.Tasks) != 1 || len(plan.Failures) != 1 || plan.Failures[0].Ordinal != 2 || plan.Tasks[0].Task.Operation != "asset.decode" {
		t.Fatal(plan, err)
	}
	outcomes[1].Inspect = Blocked
	plan, err = PlanIngestDecode(spec, docs, outcomes)
	if err != nil || plan.Ready || len(plan.Tasks) != 0 || len(plan.Failures) != 0 {
		t.Fatal("partial plan escaped barrier", err)
	}
	outcomes[1].Verify = RetryWait
	plan, err = PlanIngestDecode(spec, docs, outcomes)
	if err != nil || plan.Ready {
		t.Fatal("retry treated terminal", err)
	}
	outcomes[1].Inspect = Succeeded
	if _, err = PlanIngestDecode(spec, docs, outcomes); err == nil {
		t.Fatal("impossible success accepted")
	}
}
