package semanticproposal

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestInitialManualCoordinatorCommitAndPublicationFailures(t *testing.T) {
	failure := errors.New("phase failed or commit outcome unknown")
	built := v2input.Built{Count: 3, SHA256: "transport"}
	wanted := v2input.JobTicket{RunID: uuid.New(), WorkflowID: uuid.New(), State: "queued"}
	for _, fail := range []string{"begin", "publish", "admit", ""} {
		t.Run(fail, func(t *testing.T) {
			var calls []string
			got, err := coordinateNativeInputManualInitial(context.Background(), func() error {
				calls = append(calls, "begin")
				if fail == "begin" {
					return failure
				}
				return nil
			}, func() (v2input.PreparedSemanticTask, error) {
				calls = append(calls, "publish")
				out := v2input.PreparedSemanticTask{Graph: v2input.GraphTask{Built: built}}
				if fail == "publish" {
					return out, failure
				}
				return out, nil
			}, func(actual v2input.Built) (v2input.JobTicket, error) {
				calls = append(calls, "admit")
				if !reflect.DeepEqual(actual, built) {
					t.Fatal("transport changed")
				}
				if fail == "admit" {
					return wanted, failure
				}
				return wanted, nil
			})
			expected := []string{"begin", "publish", "admit"}
			if fail == "begin" {
				expected = expected[:1]
			}
			if fail == "publish" {
				expected = expected[:2]
			}
			if !reflect.DeepEqual(calls, expected) {
				t.Fatal(calls)
			}
			if fail != "" {
				if !errors.Is(err, failure) || got != (v2input.JobTicket{}) {
					t.Fatal("failed phase exposed ticket", got, err)
				}
			} else if err != nil || got != wanted {
				t.Fatal(got, err)
			}
		})
	}
}

func TestInitialManualCoordinatorCancellationAfterCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	published := false
	ticket, err := coordinateNativeInputManualInitial(ctx, func() error { cancel(); return nil }, func() (v2input.PreparedSemanticTask, error) {
		published = true
		return v2input.PreparedSemanticTask{}, nil
	}, func(v2input.Built) (v2input.JobTicket, error) {
		t.Fatal("admission after cancellation")
		return v2input.JobTicket{}, nil
	})
	if err != context.Canceled || published || ticket != (v2input.JobTicket{}) {
		t.Fatal(ticket, err, published)
	}
}
