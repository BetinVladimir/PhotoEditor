package semanticproposal

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

func TestNativeInputTransferStagesCommitOrderAndFailure(t *testing.T) {
	failure := errors.New("stage failed")
	for _, mode := range []string{"success", "begin", "first-commit", "publish", "register", "second-commit"} {
		t.Run(mode, func(t *testing.T) {
			var events []string
			txCount := 0
			tx := func(callback func(pgx.Tx) error) error {
				txCount++
				if err := callback(nil); err != nil {
					return err
				}
				events = append(events, "commit")
				if (mode == "first-commit" && txCount == 1) || (mode == "second-commit" && txCount == 2) {
					return failure
				}
				return nil
			}
			result, err := persistNativeInputTransferStages(context.Background(), tx,
				func(pgx.Tx) error {
					events = append(events, "begin")
					if mode == "begin" {
						return failure
					}
					return nil
				},
				func() ([]v2input.PreparedSemanticTask, error) {
					events = append(events, "publish")
					if mode == "publish" {
						return nil, failure
					}
					return []v2input.PreparedSemanticTask{{}}, nil
				},
				func(pgx.Tx, []v2input.PreparedSemanticTask) error {
					events = append(events, "register")
					if mode == "register" {
						return failure
					}
					return nil
				})
			if mode == "success" {
				if err != nil || len(result) != 1 || !reflect.DeepEqual(events, []string{"begin", "commit", "publish", "register", "commit"}) {
					t.Fatalf("wrong committed order: %v %v", events, err)
				}
			} else {
				if !errors.Is(err, failure) || result != nil {
					t.Fatalf("failed stage exposed result: %v", err)
				}
				if (mode == "begin" || mode == "first-commit") && len(events) > 2 {
					t.Fatalf("publication before blocked commit: %v", events)
				}
			}
		})
	}
}
