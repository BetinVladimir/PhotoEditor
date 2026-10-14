package semanticreview

import (
	"strings"
	"testing"
)

func TestReviewPreparationConfigDeniesAmbiguousAndUnboundedPins(t *testing.T) {
	raw := `{"schema_version":1,"review_model_sha":"` + strings.Repeat("a", 64) + `","review_dataset_sha":"` + strings.Repeat("b", 64) + `","document_budget_bytes":67108864}`
	if _, err := DecodeReviewPreparationConfig([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		raw + `{}`, strings.Replace(raw, `"schema_version":1`, `"schema_version":2`, 1),
		strings.Replace(raw, `67108864`, `67108865`, 1), strings.Replace(raw, `67108864`, `1`, 1),
		strings.Replace(raw, `"schema_version":1`, `"schema_version":1,"unknown":true`, 1),
		strings.Replace(raw, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(raw, strings.Repeat("a", 64), strings.Repeat("A", 64), 1),
	} {
		if _, err := DecodeReviewPreparationConfig([]byte(invalid)); err == nil {
			t.Fatal("invalid preparation config accepted")
		}
	}
}
