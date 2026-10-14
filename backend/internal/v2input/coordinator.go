package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
)

var ErrConflict = errors.New("V2 admission idempotency conflict")
var ErrExpired = errors.New("V2 draft expired")

type JobTicket struct {
	RunID      uuid.UUID `json:"run_id"`
	WorkflowID uuid.UUID `json:"workflow_id"`
	State      string    `json:"state"`
}

type TaskSource struct {
	ID              uuid.UUID
	Open            CursorFactory
	Direct          []Ref
	IdentityConsent *IdentityConsentInput
}

type AdmissionStore interface {
	Begin(context.Context, DraftShell, string, string) (string, uuid.UUID, error)
	Seal(context.Context, uuid.UUID, uuid.UUID, []GraphTask) error
	Ticket(context.Context, uuid.UUID, uuid.UUID, string, string) (JobTicket, error)
}

// Coordinator is the two-transaction V2 admission protocol. Publisher calls
// are deliberately between the two database transactions. Callers must use a
// stable run ID and frozen task IDs/cursors on retries of the same request.
type Coordinator struct {
	Store     AdmissionStore
	Publisher Publisher
}

func (c Coordinator) Admit(ctx context.Context, shell DraftShell, key, requestSHA string,
	sources []TaskSource) (JobTicket, error) {
	if c.Store == nil || c.Publisher == nil || len(sources) != len(shell.Tasks) || len(sources) == 0 {
		return JobTicket{}, ErrInvalid
	}
	expectedRun, err := RunID(shell.Tenant, shell.Actor, key)
	if err != nil || shell.Run != expectedRun {
		return JobTicket{}, ErrInvalid
	}
	expectedWorkflow, err := WorkflowID(shell.Run)
	if err != nil || shell.WorkflowID != expectedWorkflow {
		return JobTicket{}, ErrInvalid
	}
	for i, source := range sources {
		if source.ID == uuid.Nil || source.ID != shell.Tasks[i].ID || source.Open == nil ||
			!validTaskIdentity(shell.WorkflowID, source.ID, shell.Tasks[i].NodeKey) {
			return JobTicket{}, ErrInvalid
		}
	}
	outcome, run, err := c.Store.Begin(ctx, shell, key, requestSHA)
	if err != nil {
		return JobTicket{}, err
	}
	switch outcome {
	case "conflict":
		return JobTicket{}, ErrConflict
	case "expired":
		return JobTicket{}, ErrExpired
	case "replayed":
		return c.ticket(ctx, shell, key, requestSHA, run)
	case "created", "pending":
		if run != shell.Run { // never publish a replay under newly generated IDs
			return JobTicket{}, ErrInvalid
		}
	default:
		return JobTicket{}, ErrInvalid
	}
	graph := make([]GraphTask, 0, len(sources))
	for _, source := range sources {
		cursor, err := source.Open(ctx)
		if err != nil {
			return JobTicket{}, err
		}
		if cursor == nil {
			return JobTicket{}, ErrInvalid
		}
		built, buildErr := BuildInputClosure(ctx, source.ID, cursor, c.Publisher)
		if closer, ok := cursor.(io.Closer); ok {
			_ = closer.Close()
		}
		if buildErr != nil {
			return JobTicket{}, buildErr
		}
		graph = append(graph, GraphTask{ID: source.ID, Built: built, Open: source.Open,
			Direct: source.Direct, IdentityConsent: source.IdentityConsent})
	}
	if err := c.Store.Seal(ctx, shell.Tenant, run, graph); err != nil {
		return JobTicket{}, err
	}
	return c.ticket(ctx, shell, key, requestSHA, run)
}

func (c Coordinator) ticket(ctx context.Context, shell DraftShell, key, requestSHA string, run uuid.UUID) (JobTicket, error) {
	ticket, err := c.Store.Ticket(ctx, shell.Tenant, shell.Actor, key, requestSHA)
	if err != nil {
		return JobTicket{}, err
	}
	if ticket.RunID != run || ticket.WorkflowID != shell.WorkflowID || ticket.State != "queued" {
		return JobTicket{}, ErrInvalid
	}
	return ticket, nil
}

type SQLAdmissionStore struct{ DB *pgxpool.Pool }

func (s SQLAdmissionStore) Begin(ctx context.Context, shell DraftShell, key, requestSHA string) (string, uuid.UUID, error) {
	if s.DB == nil {
		return "", uuid.Nil, ErrInvalid
	}
	var outcome string
	var run uuid.UUID
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		outcome, run, e = BeginAdmissionInTransaction(ctx, tx, shell, key, requestSHA)
		return e
	})
	return outcome, run, err
}

func (s SQLAdmissionStore) Seal(ctx context.Context, tenant, run uuid.UUID, graph []GraphTask) error {
	return (Admission{DB: s.DB}).CommitGraph(ctx, tenant, run, graph)
}

func (s SQLAdmissionStore) Ticket(ctx context.Context, tenant, actor uuid.UUID, key, requestSHA string) (JobTicket, error) {
	if s.DB == nil || tenant == uuid.Nil || actor == uuid.Nil {
		return JobTicket{}, ErrInvalid
	}
	var raw []byte
	err := s.DB.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)",
		tenant, actor, key, requestSHA).Scan(&raw)
	if err != nil {
		return JobTicket{}, err
	}
	var ticket JobTicket
	if json.Unmarshal(raw, &ticket) != nil || ticket.RunID == uuid.Nil ||
		ticket.WorkflowID == uuid.Nil || ticket.State != "queued" {
		return JobTicket{}, ErrInvalid
	}
	return ticket, nil
}
