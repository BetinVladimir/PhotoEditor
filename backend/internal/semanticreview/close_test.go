package semanticreview

import (
	"errors"
	"strings"
	"testing"
)

func TestCloseRequiresAllDecisionsAndCurrentParent(t *testing.T) {
	request := ReviewClose{"1", strings.Repeat("a", 64)}
	if err := ValidateClose(request, request.PlanSHA, "1", "waiting_review", 0); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClose(request, request.PlanSHA, "1", "waiting_review", 1); !errors.Is(err, ErrReviewIncomplete) {
		t.Fatal(err)
	}
	for _, state := range []string{"completed", "cancelled", "running", "failed"} {
		if err := ValidateClose(request, request.PlanSHA, "1", state, 0); !errors.Is(err, ErrConflict) {
			t.Fatal(state, err)
		}
	}
	if err := ValidateClose(request, request.PlanSHA, "2", "waiting_review", 0); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := ValidateClose(request, request.PlanSHA, "1", "waiting_review", -1); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestStrictCloseContract(t *testing.T) {
	valid := `{"expected_revision":"1","plan_sha":"` + strings.Repeat("a", 64) + `"}`
	if _, err := DecodeReviewClose([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `{}`, `{"expected_revision":null,"plan_sha":"` + strings.Repeat("a", 64) + `"}`, strings.TrimSuffix(valid, "}") + `,"unknown":1}`, strings.TrimSuffix(valid, "}") + `,"expected_revision":"2"}`} {
		if _, err := DecodeReviewClose([]byte(raw)); err == nil {
			t.Fatal(raw)
		}
	}
}
