package delivery

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

func TestCreateActivationValidatesExactTicketAndReplayReceipt(t *testing.T) {
	i, session, _, _ := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	for _, kind := range []string{"fresh", "replay", "run", "workflow", "state", "missing-marker", "unknown", "null"} {
		t.Run(kind, func(t *testing.T) {
			ticket := v2input.JobTicket{RunID: i.identity.Run, WorkflowID: i.identity.Workflow, State: "queued"}
			switch kind {
			case "run":
				ticket.RunID = uuid.New()
			case "workflow":
				ticket.WorkflowID = uuid.New()
			case "state":
				ticket.State = "running"
			}
			body := map[string]any{"ticket": ticket, "replayed": kind == "replay"}
			if kind == "missing-marker" {
				delete(body, "replayed")
			}
			if kind == "unknown" {
				body["unexpected"] = true
			}
			raw, err := canonical.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "null" {
				raw = []byte("null")
			}
			tx := &deliveryReadTx{raw: raw}
			out, err := i.ActivateCreate(context.Background(), tx, who, i.tenant, "photos")
			valid := kind == "fresh" || kind == "replay"
			if (err == nil) != valid {
				t.Fatalf("%s: %v", kind, err)
			}
			if valid && (out.Status != 202 || out.ResourceID == nil || *out.ResourceID != i.identity.Package || out.Replayed != (kind == "replay")) {
				t.Fatal("response identity changed")
			}
			if len(tx.args) != 6 || tx.args[0] != i.tenant || tx.args[1] != who.UserID || tx.args[2] != session || tx.args[3] != i.key || tx.args[4] != i.requestSHA || tx.args[5] != "photos" {
				t.Fatal("activation scope changed")
			}
		})
	}
	tx := &deliveryReadTx{}
	if _, err := i.ActivateCreate(context.Background(), tx, who, i.tenant, "ab"); err == nil || tx.calls != 0 {
		t.Fatal("invalid bucket queried activation")
	}
	if _, err := i.ActivateCreate(context.Background(), tx, who, uuid.New(), "photos"); err == nil || tx.calls != 0 {
		t.Fatal("foreign scope queried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := i.ActivateCreate(ctx, tx, who, i.tenant, "photos"); err != context.Canceled || tx.calls != 0 {
		t.Fatal("cancelled SQL")
	}
}

func TestCreateActivationMapsOnlyExplicitSealRefusal(t *testing.T) {
	i, session, _, _ := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	for _, tc := range []struct {
		name, code, message string
		conflict            bool
	}{
		{"seal refusal", "23514", "DELIVERY_INITIAL_ACTIVATION_REFUSED", true},
		{"other constraint", "23514", "unrelated integrity failure", false},
		{"connection failure", "08006", "DELIVERY_INITIAL_ACTIVATION_REFUSED", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := &pgconn.PgError{Code: tc.code, Message: tc.message}
			wrapped := fmt.Errorf("activation query: %w", cause)
			out, err := i.ActivateCreate(context.Background(), &deliveryReadTx{err: wrapped}, who, i.tenant, "photos")
			if err == nil || out.Status != 0 || len(out.Body) != 0 {
				t.Fatal("failed activation returned success")
			}
			if tc.conflict {
				var business *domain.Error
				if !errors.As(err, &business) || business.Code != "DELIVERY_CREATE_STALE" {
					t.Fatal("missing business conflict", err)
				}
			} else if !errors.Is(err, cause) {
				t.Fatal("database cause lost", err)
			}
		})
	}
}

func TestCreateActivationRejectsExpiredRuntimeLease(t *testing.T) {
	i, session, _, _ := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	cause := &pgconn.PgError{Code: "P0001", Message: "DELIVERY_RUNTIME_UNAVAILABLE"}
	_, err := i.ActivateCreate(context.Background(), &deliveryReadTx{err: cause}, who, i.tenant, "photos")
	var business *domain.Error
	if !errors.As(err, &business) || business.Code != "DELIVERY_UNAVAILABLE" || business.HTTPStatus != 503 {
		t.Fatal("runtime loss was not classified", err)
	}
}

func TestCreateActivationRejectsMissingTenantSigningRuntime(t *testing.T) {
	i, session, _, _ := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	cause := &pgconn.PgError{Code: "P0001", Message: "DELIVERY_SIGNING_RUNTIME_UNAVAILABLE"}
	_, err := i.ActivateCreate(context.Background(), &deliveryReadTx{err: cause}, who, i.tenant, "photos")
	var business *domain.Error
	if !errors.As(err, &business) || business.Code != "DELIVERY_PROVENANCE_UNAVAILABLE" || business.HTTPStatus != 503 {
		t.Fatal("signing runtime loss was not classified", err)
	}
}
