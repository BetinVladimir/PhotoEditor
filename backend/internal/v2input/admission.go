package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/billing"
	"photoeditor/backend/internal/database"
)

// CursorFactory must reopen the same immutable/frozen input snapshot for each
// transaction retry. It must not perform object writes or create new inputs.
type CursorFactory func(context.Context) (Cursor, error)

// Admission owns the DB transaction for compiler admission.
type Admission struct{ DB *pgxpool.Pool }

// GraphTask carries an already-published input closure. Open must reopen the
// same frozen inventory on each transaction retry and performs no object IO.
type GraphTask struct {
	ID              uuid.UUID
	Built           Built
	Open            CursorFactory
	Direct          []Ref
	IdentityConsent *IdentityConsentInput
}

// IdentityConsentInput is the exact immutable snapshot for an identity.compare
// task. The activation function pins its live epochs in the same transaction
// that seals the graph.
type IdentityConsentInput struct {
	Workflow uuid.UUID
	Ordinal  int
	Ref      Ref
	Body     []byte
}

// CommitGraph registers every task closure and seals the graph atomically.
// All transport artifacts and the run/task shell must exist before this call.
func (a Admission) CommitGraph(ctx context.Context, tenant, run uuid.UUID, tasks []GraphTask) error {
	if a.DB == nil {
		return ErrInvalid
	}
	return database.InTransaction(ctx, a.DB, func(tx pgx.Tx) error {
		return SealGraphInTransaction(ctx, tx, tenant, run, tasks)
	})
}

// SealGraphInTransaction runs after draft shell commit and external transport
// publication. The caller must roll back on error and may retry the entire
// DB-only callback against the same frozen cursors.
func SealGraphInTransaction(ctx context.Context, tx pgx.Tx, tenant, run uuid.UUID, tasks []GraphTask) error {
	if tx == nil || tenant == uuid.Nil || run == uuid.Nil || len(tasks) == 0 || len(tasks) > 5000 {
		return ErrInvalid
	}
	seen := make(map[uuid.UUID]struct{}, len(tasks))
	var identity *IdentityConsentInput
	var identityTask uuid.UUID
	for _, task := range tasks {
		if task.ID == uuid.Nil || task.Open == nil || !validBuilt(task.Built, task.Direct) {
			return ErrInvalid
		}
		if _, exists := seen[task.ID]; exists {
			return ErrInvalid
		}
		seen[task.ID] = struct{}{}
		if task.IdentityConsent != nil {
			pin := task.IdentityConsent
			expectedWorkflow, err := WorkflowID(run)
			if err != nil || pin.Workflow != expectedWorkflow || len(tasks) != 1 ||
				!identityPinnedDocument(pin.Ref, pin.Body) ||
				ValidateSessionConsentSnapshot(pin.Body) != nil {
				return ErrInvalid
			}
			expectedTask, err := TaskID(pin.Workflow, "identity.compare", pin.Ordinal)
			if err != nil || task.ID != expectedTask {
				return ErrInvalid
			}
			found := false
			for _, ref := range task.Direct {
				found = found || ref == pin.Ref
			}
			if !found {
				return ErrInvalid
			}
			identity, identityTask = pin, task.ID
		}
	}
	if err := billing.PrepareOperations(ctx, tx, tenant, run); err != nil {
		return err
	}
	for _, task := range tasks {
		if err := RegisterInTransaction(ctx, tx, tenant, run, task.ID,
			task.Built, task.Open, task.Direct); err != nil {
			return err
		}
	}
	var sealed bool
	statement := "SELECT photo.seal_v2_graph($1,$2)"
	args := []any{tenant, run}
	if identity != nil {
		statement = "SELECT photo.activate_v23_identity_compare($1,$2,$3,$4,$5)"
		args = append(args, identity.Workflow, identityTask, string(identity.Body))
	}
	if err := tx.QueryRow(ctx, statement, args...).Scan(&sealed); err != nil {
		return billing.MapError(err)
	}
	if !sealed {
		return fmt.Errorf("%w: graph not sealed", ErrInvalid)
	}
	return nil
}

// Commit is for task-only registration during a controlled expansion. Initial
// graph admission must use CommitGraph or SealGraphInTransaction.
func (a Admission) Commit(ctx context.Context, tenant, run, task uuid.UUID, built Built,
	open CursorFactory, direct []Ref) error {
	if a.DB == nil {
		return ErrInvalid
	}
	return database.InTransaction(ctx, a.DB, func(tx pgx.Tx) error {
		return RegisterInTransaction(ctx, tx, tenant, run, task, built, open, direct)
	})
}

func validBuilt(built Built, direct []Ref) bool {
	if !validRef(built.Manifest) || built.Count < 1 || built.Count > MaxEntries ||
		len(built.Chunks) != (built.Count+ChunkSize-1)/ChunkSize ||
		len(built.Chunks) < 1 || len(built.Chunks) > MaxChunks ||
		!digestPattern.MatchString(built.SHA256) || len(direct) > 127 {
		return false
	}
	seen := map[uuid.UUID]struct{}{built.Manifest.ArtifactID: {}}
	for _, chunk := range built.Chunks {
		if !validRef(chunk) {
			return false
		}
		if _, exists := seen[chunk.ArtifactID]; exists {
			return false
		}
		seen[chunk.ArtifactID] = struct{}{}
	}
	for _, ref := range direct {
		if !validRef(ref) {
			return false
		}
		if _, exists := seen[ref.ArtifactID]; exists {
			return false
		}
		seen[ref.ArtifactID] = struct{}{}
	}
	return true
}

// RegisterInTransaction writes all exact input rows and seals the task with
// the verified transport references. Its caller must roll back on any error;
// no object IO is allowed while the transaction is open.
func RegisterInTransaction(ctx context.Context, tx pgx.Tx, tenant, run, task uuid.UUID,
	built Built, open CursorFactory, direct []Ref) error {
	return registerTaskInputs(ctx, tx, tenant, run, task, built, open, direct, nil)
}

// RegisterInterchangeInTransaction uses a fixed dedicated SQL dialect for late
// import continuations. Caller must roll back the entire registration on error.
func RegisterInterchangeInTransaction(ctx context.Context, tx pgx.Tx, tenant, run, task uuid.UUID, built Built, open CursorFactory, direct []Ref, parsed []byte) error {
	if len(parsed) < 2 || len(parsed) > 8<<20 {
		return ErrInvalid
	}
	return registerTaskInputs(ctx, tx, tenant, run, task, built, open, direct, append([]byte(nil), parsed...))
}
func registerTaskInputs(ctx context.Context, tx pgx.Tx, tenant, run, task uuid.UUID,
	built Built, open CursorFactory, direct []Ref, parsed []byte) error {
	if tx == nil || tenant == uuid.Nil || run == uuid.Nil || task == uuid.Nil ||
		open == nil || !validBuilt(built, direct) {
		return ErrInvalid
	}
	cursor, err := open(ctx)
	if err != nil {
		return err
	}
	if cursor == nil {
		return ErrInvalid
	}
	if closer, ok := cursor.(io.Closer); ok {
		defer closer.Close()
	}
	h := sha256.New()
	_, _ = h.Write([]byte("["))
	batch := make([]Entry, 0, ChunkSize)
	count := 0
	var previous uuid.UUID
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		payload, err := json.Marshal(batch)
		if err != nil || len(payload) > 1_048_576 {
			return ErrInvalid
		}
		first := count - len(batch)
		var inserted int
		statement := "SELECT photo.register_v2_task_input_batch($1,$2,$3,$4,$5)"
		args := []any{tenant, run, task, first, json.RawMessage(payload)}
		if parsed != nil {
			statement = "SELECT photo.register_v30_interchange_input_batch($1,$2,$3,$4,$5,$6)"
			args = append(args, string(parsed))
		}
		if err := tx.QueryRow(ctx, statement, args...).Scan(&inserted); err != nil {
			return err
		}
		if inserted != len(batch) {
			return ErrInvalid
		}
		batch = batch[:0]
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, ok, err := cursor.Next(ctx)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if !validEntry(entry) || count >= built.Count ||
			(count > 0 && bytes.Compare(previous[:], entry.Ref.ArtifactID[:]) >= 0) {
			return ErrInvalid
		}
		if err := writeEntryDigest(h, entry, count == 0); err != nil {
			return err
		}
		batch = append(batch, entry)
		count++
		previous = entry.Ref.ArtifactID
		if len(batch) == ChunkSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if count != built.Count {
		return ErrInvalid
	}
	if err := flush(); err != nil {
		return err
	}
	_, _ = h.Write([]byte("]"))
	if hex.EncodeToString(h.Sum(nil)) != built.SHA256 {
		return ErrInvalid
	}
	manifest, _ := json.Marshal(built.Manifest)
	chunks, _ := json.Marshal(built.Chunks)
	roots, _ := json.Marshal(direct)
	var sealed bool
	statement := "SELECT photo.seal_v2_task_inputs($1,$2,$3,$4,$5,$6,$7,$8)"
	args := []any{tenant, run, task, json.RawMessage(manifest), json.RawMessage(chunks), built.Count, built.SHA256, json.RawMessage(roots)}
	if parsed != nil {
		statement = "SELECT photo.seal_v30_interchange_inputs($1,$2,$3,$4,$5,$6,$7,$8,$9)"
		args = append(args, string(parsed))
	}
	if err := tx.QueryRow(ctx, statement, args...).Scan(&sealed); err != nil {
		return billing.MapError(err)
	}
	if !sealed {
		return fmt.Errorf("%w: task input closure not sealed", ErrInvalid)
	}
	return nil
}
