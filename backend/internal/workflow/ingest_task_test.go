package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestIngestResourceBindings(t *testing.T) {
	s, r := ingestFixture(t)
	plan, err := CompileIngest(s, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range plan.Nodes {
		hash := sha256.Sum256(node.Params)
		params := review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(hash[:])}
		sourceBytes := int64(2345)
		format := "raw"
		if node.Inputs[0] == s.Items[0].Source {
			sourceBytes = 1234
			format = "png"
		}
		meta := &IngestMetadata{Source: node.Inputs[0], Width: 400, Height: 300, Format: format, HasAlpha: format == "png"}
		task, e := PrepareIngestTask(node, params, sourceBytes, meta)
		if e != nil {
			t.Fatal(node.Operation, e)
		}
		estimate, e := ParseResourceEstimate(task.ExecutionProfile)
		if e != nil || estimate.ScratchBytes < sourceBytes+estimate.OutputBytes || estimate.PixelCount != 120000 {
			t.Fatal(estimate, e)
		}
		if node.Operation == "asset.decode" {
			if format == "png" && estimate.OutputBytes < 120000*14 {
				t.Fatal("alpha underbudgeted")
			}
			if _, e = PrepareIngestTask(node, params, sourceBytes, nil); e == nil {
				t.Fatal("decode lacks metadata")
			}
		}
		if _, e = PrepareIngestTask(node, params, sourceBytes+1, meta); e == nil {
			t.Fatal("source size mismatch accepted")
		}
		params.SHA256 = "bad"
		if _, e = PrepareIngestTask(node, params, sourceBytes, meta); e == nil {
			t.Fatal("wrong params hash accepted")
		}
	}
}
