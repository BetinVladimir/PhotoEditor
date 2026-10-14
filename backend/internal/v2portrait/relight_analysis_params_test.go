package v2portrait

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRelightAnalysisParamsUseOriginalWorkerABI(t *testing.T) {
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	for _, operation := range []string{"depth.predict", "mask.person"} {
		raw, sha, err := RelightAnalysisParams(operation, source, strings.Repeat("b", 64))
		if err != nil || len(sha) != 64 {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			t.Fatal("json")
		}
		if _, exists := fields["schema_version"]; exists {
			t.Fatal("worker ABI has no schema_version")
		}
		var ref v2input.Ref
		json.Unmarshal(fields["image"], &ref)
		if ref != source {
			t.Fatal("source changed")
		}
	}
	if _, _, err := RelightAnalysisParams("unknown", source, strings.Repeat("b", 64)); err == nil {
		t.Fatal("unknown operation")
	}
	if _, _, err := RelightAnalysisParams("depth.predict", source, "invalid"); err == nil {
		t.Fatal("invalid model lock")
	}
}
