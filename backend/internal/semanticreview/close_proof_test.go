package semanticreview

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCloseProofDoesNotWaitForChildren(t *testing.T) {
	run := uuid.New()
	request := ReviewClose{"9", strings.Repeat("a", 64)}
	targets := []LockedTarget{}
	for _, state := range []string{"unsupported", "rejected", "queued", "applied", "failed", "cancelled"} {
		targets = append(targets, LockedTarget{ID: uuid.New(), State: state})
	}
	proof, err := BuildCloseProof(run, request, request.PlanSHA, "9", "waiting_review", targets)
	if err != nil || len(proof.Body) == 0 || len(proof.SHA256) != 64 {
		t.Fatal(proof, err)
	}
	other, err := HashCloseRequest(uuid.New(), request)
	if err != nil || other == proof.RequestSHA256 {
		t.Fatal("receipt fingerprint must bind parent", err)
	}
	targets[0].State = "proposed"
	if _, err := BuildCloseProof(run, request, request.PlanSHA, "9", "waiting_review", targets); !errors.Is(err, ErrReviewIncomplete) {
		t.Fatal(err)
	}
	targets[0].State = "unsupported"
	targets[1].ID = targets[0].ID
	if _, err := BuildCloseProof(run, request, request.PlanSHA, "9", "waiting_review", targets); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestCloseProofRejectsRevisionOverflowAndStalePlan(t *testing.T) {
	targets := []LockedTarget{{ID: uuid.New(), State: "unsupported"}}
	request := ReviewClose{"9223372036854775807", strings.Repeat("a", 64)}
	if _, err := BuildCloseProof(uuid.New(), request, request.PlanSHA, request.ExpectedRevision, "waiting_review", targets); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	request.ExpectedRevision = "1"
	if _, err := BuildCloseProof(uuid.New(), request, strings.Repeat("b", 64), "1", "waiting_review", targets); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
