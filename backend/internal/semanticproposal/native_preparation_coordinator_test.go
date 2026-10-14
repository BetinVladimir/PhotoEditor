package semanticproposal

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativePreparationPublicationOutsideCommittedTransactions(t *testing.T) {
	for _, failure := range []string{"", "draft", "draft_commit", "publication", "registration", "registration_commit"} {
		t.Run(failure, func(t *testing.T) {
			dependency := errors.New("native coordinator test failure")
			active := false
			transactions, published, registered := 0, 0, 0
			transaction := func(callback func(pgx.Tx) error) error {
				transactions++
				if active {
					t.Fatal("nested preparation transaction")
				}
				active = true
				err := callback(nil)
				active = false
				if err != nil {
					return err
				}
				if (transactions == 1 && failure == "draft_commit") || (transactions == 2 && failure == "registration_commit") {
					return dependency
				}
				return nil
			}
			begin := func(tx pgx.Tx) error {
				if !active {
					t.Fatal("draft outside transaction")
				}
				if failure == "draft" {
					return dependency
				}
				return nil
			}
			publish := func() (v2input.PreparedSemanticTask, error) {
				if active || transactions != 1 {
					t.Fatal("publication spans SQL or precedes draft commit")
				}
				published++
				if failure == "publication" {
					return v2input.PreparedSemanticTask{}, dependency
				}
				var task v2input.PreparedSemanticTask
				task.Graph.Built.Count = 3
				return task, nil
			}
			register := func(tx pgx.Tx, built v2input.Built) error {
				if !active || published != 1 || built.Count != 3 {
					t.Fatal("registration lost committed published transport")
				}
				registered++
				if failure == "registration" {
					return dependency
				}
				return nil
			}
			prepared, err := persistNativePreparationInputs(context.Background(), transaction, begin, publish, register)
			if failure == "" {
				if err != nil || transactions != 2 || published != 1 || registered != 1 || prepared.Graph.Built.Count != 3 {
					t.Fatal("coordinator outcome", err)
				}
			} else {
				if !errors.Is(err, dependency) || prepared.Graph.Built.Count != 0 {
					t.Fatal("failed transaction/publication returned usable transport", err)
				}
				if (failure == "draft" || failure == "draft_commit") && published != 0 {
					t.Fatal("publication after failed draft")
				}
				if failure == "publication" && registered != 0 {
					t.Fatal("registration after failed publication")
				}
			}
		})
	}
}

func TestNativePreparationCancellationAfterDraftStopsPublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transaction := func(callback func(pgx.Tx) error) error { err := callback(nil); cancel(); return err }
	begin := func(pgx.Tx) error { return nil }
	publish := func() (v2input.PreparedSemanticTask, error) {
		t.Fatal("cancelled draft published")
		return v2input.PreparedSemanticTask{}, nil
	}
	register := func(pgx.Tx, v2input.Built) error { t.Fatal("cancelled draft registered"); return nil }
	if _, err := persistNativePreparationInputs(ctx, transaction, begin, publish, register); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
