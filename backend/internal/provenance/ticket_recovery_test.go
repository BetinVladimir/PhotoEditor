package provenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type signingTicketTx struct {
	outcome string
	body    []byte
	err     error
	calls   int
	args    []any
}

func (s *signingTicketTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	s.calls++
	s.args = args
	return signingTicketRow{s.outcome, s.body, s.err}
}

type signingTicketRow struct {
	outcome string
	body    []byte
	err     error
}

func (r signingTicketRow) Scan(dst ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dst[0].(*string)) = r.outcome
	*(dst[1].(*[]byte)) = append([]byte(nil), r.body...)
	return nil
}
func TestSigningTicketRecoveryRefusesUncommittedOrForeignAcknowledgement(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tid, export, policy := uuid.New(), uuid.New(), uuid.New()
	key := "ticket"
	workflow := uuid.NewSHA1(tid, []byte("provenance:"+who.UserID.String()+":"+key))
	expected := v2input.JobTicket{RunID: uuid.NewSHA1(workflow, []byte("run")), WorkflowID: workflow, State: "queued"}
	for _, mode := range []string{"replayed", "missing", "pending", "conflict", "expired", "forbidden", "invalid", "unknown", "foreign_run", "foreign_workflow", "state", "unknown_field", "null_body", "unexpected_body", "sql_error", "cancelled", "bad_key"} {
		t.Run(mode, func(t *testing.T) {
			raw, err := canonical.Marshal(expected)
			if err != nil {
				t.Fatal(err)
			}
			tx := &signingTicketTx{outcome: "replayed", body: raw}
			ctx := context.Background()
			givenKey := key
			cause := errors.New("database unavailable")
			switch mode {
			case "missing", "pending", "conflict", "expired", "forbidden", "invalid", "unknown":
				tx.outcome = mode
				tx.body = nil
			case "foreign_run":
				value := expected
				value.RunID = uuid.New()
				tx.body, _ = canonical.Marshal(value)
			case "foreign_workflow":
				value := expected
				value.WorkflowID = uuid.New()
				tx.body, _ = canonical.Marshal(value)
			case "state":
				value := expected
				value.State = "completed"
				tx.body, _ = canonical.Marshal(value)
			case "unknown_field":
				tx.body = append([]byte(`{"unknown":true,`), raw[1:]...)
			case "null_body":
				tx.body = nil
			case "unexpected_body":
				tx.outcome = "pending"
			case "sql_error":
				tx.err = cause
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "bad_key":
				givenKey = "bad key"
			}
			got, found, err := RecoverSigningTicket(ctx, tx, who, tid, export, policy, givenKey)
			if mode == "replayed" {
				if err != nil || !found || got != expected {
					t.Fatal("valid ticket lost", err)
				}
				hash, _ := canonical.Hash(struct {
					Export uuid.UUID `json:"export_id"`
					Policy uuid.UUID `json:"signing_policy_id"`
				}{export, policy})
				if len(tx.args) != 7 || tx.args[0] != tid || tx.args[3] != export || tx.args[4] != policy || tx.args[6] != hash {
					t.Fatal("recovery request binding lost")
				}
			} else if mode == "missing" || mode == "pending" {
				if err != nil || found || got.RunID != uuid.Nil {
					t.Fatal("pending became ticket")
				}
			} else {
				if err == nil || found || got.RunID != uuid.Nil {
					t.Fatal("invalid ticket exposed", mode)
				}
				if mode == "sql_error" && !errors.Is(err, cause) {
					t.Fatal("database error lost")
				}
			}
			if (mode == "cancelled" || mode == "bad_key") && tx.calls != 0 {
				t.Fatal("invalid recovery reached SQL")
			}
		})
	}
}
