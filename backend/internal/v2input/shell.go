package v2input

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
)

// DraftShell is the pre-publication run/task shell. The trusted compiler must
// derive IDs and digests from a frozen snapshot, then publish transport objects
// only after this transaction commits. Inputs remain blocked until graph seal.
type DraftShell struct {
	Tenant     uuid.UUID
	Run        uuid.UUID
	WorkflowID uuid.UUID
	Project    uuid.UUID
	Snapshot   uuid.UUID
	Actor      uuid.UUID
	Workflow   string
	PolicySHA  string
	ConfigSHA  string
	Config     json.RawMessage
	Tasks      []DraftTask
	Edges      []DraftEdge
	Bytes      int64
}

type DraftTask struct {
	ID               uuid.UUID       `json:"id"`
	NodeKey          string          `json:"node_key"`
	Operation        string          `json:"operation"`
	OperationVersion string          `json:"operation_version"`
	ResourceClass    string          `json:"resource_class"`
	ParamsArtifactID uuid.UUID       `json:"params_artifact_id"`
	ParamsSHA        string          `json:"params_sha"`
	InputDigest      string          `json:"input_digest"`
	ExecutionProfile json.RawMessage `json:"execution_profile"`
}

type DraftEdge struct {
	PrerequisiteID uuid.UUID `json:"prerequisite_id"`
	DependentID    uuid.UUID `json:"dependent_id"`
}

// CreateDraftShellInTransaction persists one bounded draft and its quota
// reservation. The SQL function repeats tenant, principal, project, snapshot,
// artifact and quota checks under locks; callers can add idempotency in this TX.
func encodeDraftShell(shell DraftShell) ([]byte, []byte, error) {
	if shell.Tenant == uuid.Nil || shell.Run == uuid.Nil || shell.WorkflowID == uuid.Nil || shell.Project == uuid.Nil ||
		shell.Snapshot == uuid.Nil || shell.Actor == uuid.Nil || shell.Bytes <= 0 ||
		!digestPattern.MatchString(shell.PolicySHA) || !digestPattern.MatchString(shell.ConfigSHA) ||
		len(shell.Tasks) < 1 || len(shell.Tasks) > 5000 || len(shell.Edges) > 20000 ||
		!json.Valid(shell.Config) {
		return nil, nil, ErrInvalid
	}
	configSHA, err := canonical.Hash(json.RawMessage(shell.Config))
	if err != nil || configSHA != shell.ConfigSHA {
		return nil, nil, ErrInvalid
	}
	seen := make(map[uuid.UUID]struct{}, len(shell.Tasks))
	for _, task := range shell.Tasks {
		if task.ID == uuid.Nil || task.ParamsArtifactID == uuid.Nil ||
			!digestPattern.MatchString(task.ParamsSHA) || !digestPattern.MatchString(task.InputDigest) ||
			!json.Valid(task.ExecutionProfile) {
			return nil, nil, ErrInvalid
		}
		if _, duplicate := seen[task.ID]; duplicate {
			return nil, nil, ErrInvalid
		}
		seen[task.ID] = struct{}{}
	}
	for _, edge := range shell.Edges {
		if edge.PrerequisiteID == edge.DependentID {
			return nil, nil, ErrInvalid
		}
		if _, ok := seen[edge.PrerequisiteID]; !ok {
			return nil, nil, ErrInvalid
		}
		if _, ok := seen[edge.DependentID]; !ok {
			return nil, nil, ErrInvalid
		}
	}
	tasks, err := json.Marshal(shell.Tasks)
	if err != nil {
		return nil, nil, err
	}
	edges, err := json.Marshal(shell.Edges)
	if err != nil {
		return nil, nil, err
	}
	return tasks, edges, nil
}

func CreateDraftShellInTransaction(ctx context.Context, tx pgx.Tx, shell DraftShell) error {
	if tx == nil {
		return ErrInvalid
	}
	tasks, edges, err := encodeDraftShell(shell)
	if err != nil {
		return err
	}
	var created bool
	err = tx.QueryRow(ctx, "SELECT photo.create_v2_draft_shell($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)",
		shell.Tenant, shell.Run, shell.WorkflowID, shell.Project, shell.Snapshot, shell.Actor, shell.Workflow,
		shell.PolicySHA, shell.ConfigSHA, shell.Config, json.RawMessage(tasks),
		json.RawMessage(edges), shell.Bytes).Scan(&created)
	if err != nil {
		return err
	}
	if !created {
		return fmt.Errorf("%w: draft shell rejected", ErrInvalid)
	}
	return nil
}

// BeginAdmissionInTransaction atomically binds the request key to the draft.
// A pending replay must resume publication for the returned run. A completed
// replay returns the same run; conflict and expired are terminal for that key.
func BeginAdmissionInTransaction(ctx context.Context, tx pgx.Tx, shell DraftShell,
	key, requestSHA string) (string, uuid.UUID, error) {
	if tx == nil || !idempotency.ValidKey(key) || !digestPattern.MatchString(requestSHA) {
		return "", uuid.Nil, ErrInvalid
	}
	tasks, edges, err := encodeDraftShell(shell)
	if err != nil {
		return "", uuid.Nil, err
	}
	var outcome string
	var run pgtype.UUID
	err = tx.QueryRow(ctx, "SELECT outcome,run_id FROM photo.begin_v2_admission($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)",
		shell.Tenant, shell.Run, shell.WorkflowID, shell.Project, shell.Snapshot, shell.Actor, shell.Workflow,
		shell.PolicySHA, shell.ConfigSHA, shell.Config, json.RawMessage(tasks),
		json.RawMessage(edges), shell.Bytes, key, requestSHA).Scan(&outcome, &run)
	if err != nil {
		return "", uuid.Nil, err
	}
	if outcome != "conflict" && outcome != "created" && outcome != "pending" &&
		outcome != "replayed" && outcome != "expired" {
		return "", uuid.Nil, fmt.Errorf("%w: invalid V2 admission outcome", ErrInvalid)
	}
	if !run.Valid {
		if outcome == "conflict" {
			return outcome, uuid.Nil, nil
		}
		return "", uuid.Nil, ErrInvalid
	}
	return outcome, uuid.UUID(run.Bytes), nil
}
