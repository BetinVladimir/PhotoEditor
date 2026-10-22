package semanticreview

import (
	"encoding/json"
	"testing"
)

func TestReviewStatusKindIsOptionalAdvisoryMetadata(t *testing.T) {
	for _, kind := range []*string{nil, func() *string { value := "background_cleanup"; return &value }()} {
		raw, err := json.Marshal(ReviewStatus{Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]json.RawMessage
		if err = json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		_, present := value["kind"]
		if present != (kind != nil) {
			t.Fatal("legacy status shape changed", string(raw))
		}
		if kind != nil && string(value["kind"]) != `"background_cleanup"` {
			t.Fatal(string(raw))
		}
	}
}
