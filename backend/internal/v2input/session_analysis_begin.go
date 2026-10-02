package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
)

type SessionAnalysisSelectionLoader interface {
	Load(context.Context, SessionAnalysisSelection) (SessionInputSnapshot, []byte, error)
}

type SessionAnalysisBeginRequest struct {
	Selection     SessionAnalysisSelection
	Policy        json.RawMessage
	ModelLockSHA  string
	Key           string
	ReservedBytes int64
}

type sessionAnalysisConfig struct {
	ProjectID uuid.UUID `json:"project_id"`
	SessionID uuid.UUID `json:"session_id"`
	Plan      struct {
		SchemaVersion  int             `json:"schema_version"`
		ItemCount      int             `json:"item_count"`
		SnapshotDigest string          `json:"snapshot_digest"`
		Policy         json.RawMessage `json:"policy"`
		ContextSHA     string          `json:"context_sha"`
	} `json:"v22_session_plan"`
}

type SessionAnalysisBeginResult struct {
	Outcome       string
	Run, Workflow uuid.UUID
	RequestSHA    string
	Snapshot      SessionInputSnapshot
	ContextBody   []byte
	ContextSHA    string
	PolicySHA     string
	ModelLockSHA  string
	Project       uuid.UUID
}

type sessionAnalysisRecovery struct {
	SchemaVersion int                  `json:"schema_version"`
	Snapshot      SessionInputSnapshot `json:"snapshot"`
	Context       json.RawMessage      `json:"context"`
	ContextSHA    string               `json:"context_sha"`
	PolicySHA     string               `json:"policy_sha"`
	ModelLockSHA  string               `json:"model_lock_sha"`
	ReservedBytes int64                `json:"reserved_bytes"`
}

func (s SQLSessionAnalysisBegin) pending(ctx context.Context,
	request SessionAnalysisBeginRequest,
	probe SessionAnalysisReplayResult) (SessionAnalysisBeginResult, error) {
	selection := request.Selection
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT photo.read_v22_analysis_recovery($1,$2,$3,$4,$5,$6)`,
		selection.Tenant, selection.Actor, selection.AuthSession, selection.Run,
		request.Key, probe.RequestSHA).Scan(&raw); err != nil {
		return SessionAnalysisBeginResult{}, err
	}
	if len(raw) == 0 {
		// Another scheduler may have sealed between the initial probe and read.
		refreshed, err := s.Probe(ctx, request)
		if err != nil {
			return SessionAnalysisBeginResult{}, err
		}
		if refreshed.Outcome == "replayed" {
			return SessionAnalysisBeginResult{Outcome: "replayed", Run: selection.Run,
				Workflow: refreshed.Workflow, RequestSHA: refreshed.RequestSHA,
				PolicySHA: refreshed.PolicySHA}, nil
		}
		return SessionAnalysisBeginResult{}, ErrExpired
	}
	return decodeSessionAnalysisRecovery(raw, selection.Run, probe)
}

func decodeSessionAnalysisRecovery(raw []byte, run uuid.UUID,
	probe SessionAnalysisReplayResult) (SessionAnalysisBeginResult, error) {
	if len(raw) < 2 || len(raw) > 10485760 ||
		!exactJSONKeys(raw, "schema_version", "snapshot", "context",
			"context_sha", "policy_sha", "model_lock_sha", "reserved_bytes") {
		return SessionAnalysisBeginResult{}, ErrExpired
	}
	var saved sessionAnalysisRecovery
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&saved) != nil || decoder.Decode(new(any)) != io.EOF ||
		saved.SchemaVersion != 1 ||
		saved.PolicySHA != probe.PolicySHA || saved.ReservedBytes < 1 ||
		!digestPattern.MatchString(saved.ModelLockSHA) ||
		!digestPattern.MatchString(saved.ContextSHA) {
		return SessionAnalysisBeginResult{}, ErrInvalid
	}
	snapshot, _, err := CompileSessionInputSnapshot(run, saved.Snapshot.Items)
	if err != nil || !reflect.DeepEqual(snapshot, saved.Snapshot) {
		return SessionAnalysisBeginResult{}, ErrInvalid
	}
	contextSHA, err := canonical.Hash(json.RawMessage(saved.Context))
	if err != nil || contextSHA != saved.ContextSHA ||
		validateSessionPlanContext(saved.Context, snapshot) != nil {
		return SessionAnalysisBeginResult{}, ErrInvalid
	}
	return SessionAnalysisBeginResult{Outcome: "pending", Run: run,
		Workflow: probe.Workflow, RequestSHA: probe.RequestSHA,
		Snapshot: snapshot, ContextBody: saved.Context,
		ContextSHA: saved.ContextSHA, PolicySHA: saved.PolicySHA,
		ModelLockSHA: saved.ModelLockSHA}, nil
}

type SQLSessionAnalysisBegin struct {
	DB        *pgxpool.Pool
	Selection SessionAnalysisSelectionLoader
}

type SessionAnalysisReplayResult struct {
	Outcome               string
	Run, Workflow         uuid.UUID
	RequestSHA, PolicySHA string
}

// Probe checks the idempotency record and current editor authorization without
// reading the temporary selection or mutable recipe streams. The caller may
// use a sealed result even after the original selection has expired.
func (s SQLSessionAnalysisBegin) Probe(ctx context.Context,
	request SessionAnalysisBeginRequest) (SessionAnalysisReplayResult, error) {
	var result SessionAnalysisReplayResult
	selection := request.Selection
	if s.DB == nil || selection.Session == uuid.Nil || selection.Snapshot == uuid.Nil ||
		selection.AuthSession == uuid.Nil || selection.ExpectedRevision < 1 ||
		!json.Valid(request.Policy) || !json.Valid(selection.ManualReferences) {
		return result, ErrInvalid
	}
	expectedRun, err := RunID(selection.Tenant, selection.Actor, request.Key)
	if err != nil || expectedRun != selection.Run {
		return result, ErrInvalid
	}
	workflow, err := WorkflowID(selection.Run)
	if err != nil {
		return result, err
	}
	policySHA, err := canonical.Hash(json.RawMessage(request.Policy))
	if err != nil || !digestPattern.MatchString(policySHA) {
		return result, ErrInvalid
	}
	requestSHA, err := canonical.Hash(struct {
		SessionID        uuid.UUID       `json:"session_id"`
		SelectionID      uuid.UUID       `json:"selection_snapshot_id"`
		ExpectedRevision int64           `json:"expected_revision"`
		Policy           json.RawMessage `json:"policy"`
		Manual           json.RawMessage `json:"manual_references"`
	}{selection.Session, selection.Snapshot, selection.ExpectedRevision,
		request.Policy, selection.ManualReferences})
	if err != nil {
		return result, err
	}
	var outcome string
	var replayRun, replayWorkflow *uuid.UUID
	if err := s.DB.QueryRow(ctx, `SELECT outcome,run_id,workflow_id FROM
	 photo.v22_analysis_replay($1,$2,$3,$4,$5,$6)`, selection.Tenant,
		selection.Actor, selection.AuthSession, selection.Run, request.Key, requestSHA).
		Scan(&outcome, &replayRun, &replayWorkflow); err != nil {
		return result, err
	}
	if outcome != "missing" && outcome != "conflict" && outcome != "expired" &&
		outcome != "replayed" && outcome != "pending" {
		return result, ErrInvalid
	}
	if outcome == "replayed" || outcome == "pending" {
		if replayRun == nil || *replayRun != selection.Run ||
			replayWorkflow == nil || *replayWorkflow != workflow {
			return result, ErrInvalid
		}
	}
	return SessionAnalysisReplayResult{Outcome: outcome, Run: selection.Run,
		Workflow: workflow, RequestSHA: requestSHA, PolicySHA: policySHA}, nil
}

// Begin first compiles the selected bytes outside a transaction, then freezes
// the relational inventory and admits the draft atomically. No object IO
// occurs while locks are held. If admission fails, freeze rolls back too.
func (s SQLSessionAnalysisBegin) Begin(ctx context.Context,
	request SessionAnalysisBeginRequest) (SessionAnalysisBeginResult, error) {
	var result SessionAnalysisBeginResult
	if s.DB == nil || s.Selection == nil || request.ReservedBytes < 1 ||
		!digestPattern.MatchString(request.ModelLockSHA) {
		return result, ErrInvalid
	}
	selection := request.Selection
	probe, err := s.Probe(ctx, request)
	if err != nil {
		return result, err
	}
	workflow, policySHA, requestSHA := probe.Workflow, probe.PolicySHA, probe.RequestSHA
	switch probe.Outcome {
	case "conflict":
		return result, ErrConflict
	case "expired":
		return result, ErrExpired
	case "replayed":
		return SessionAnalysisBeginResult{Outcome: "replayed", Run: selection.Run,
			Workflow: workflow, RequestSHA: requestSHA, PolicySHA: policySHA}, nil
	case "pending":
		return s.pending(ctx, request, probe)
	case "missing":
	default:
		return result, ErrInvalid
	}
	snapshot, _, err := s.Selection.Load(ctx, selection)
	if err != nil {
		return result, err
	}
	if snapshot.SnapshotID == uuid.Nil || len(snapshot.Items) < 1 ||
		len(snapshot.Items) > 5000 || !digestPattern.MatchString(snapshot.SnapshotDigest) {
		return result, ErrInvalid
	}
	recompiled, _, err := CompileSessionInputSnapshot(selection.Run, snapshot.Items)
	if err != nil || recompiled.SnapshotID != snapshot.SnapshotID ||
		recompiled.SnapshotDigest != snapshot.SnapshotDigest {
		return result, ErrInvalid
	}
	err = database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var frozenID uuid.UUID
		var frozen json.RawMessage
		if err := tx.QueryRow(ctx, `SELECT snapshot_id,context FROM
		 photo.freeze_v22_analysis_snapshot($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9)`,
			selection.Tenant, selection.Run, selection.Session,
			selection.ExpectedRevision, selection.Snapshot,
			string(selection.ManualReferences), selection.Actor,
			selection.AuthSession, snapshot.SnapshotDigest).Scan(&frozenID, &frozen); err != nil {
			return err
		}
		if frozenID != snapshot.SnapshotID {
			return ErrInvalid
		}
		contextBody, contextSHA, project, err := CompileSessionPlanContext(frozen, snapshot)
		if err != nil {
			return err
		}
		var storedPolicy json.RawMessage
		if err := tx.QueryRow(ctx, `SELECT policy FROM photo.shoot_sessions
		 WHERE tenant_id=$1 AND id=$2 AND project_id=$3 FOR SHARE`,
			selection.Tenant, selection.Session, project).Scan(&storedPolicy); err != nil {
			return err
		}
		storedSHA, err := canonical.Hash(json.RawMessage(storedPolicy))
		if err != nil || storedSHA != policySHA {
			return ErrInvalid
		}
		var qualified bool
		if err := tx.QueryRow(ctx, `SELECT photo.v22_session_policy_ready($1,$2,$3::jsonb,$4)`,
			selection.Tenant, project, string(request.Policy),
			request.ModelLockSHA).Scan(&qualified); err != nil {
			return err
		}
		if !qualified {
			return ErrInvalid
		}
		configRow := sessionAnalysisConfig{ProjectID: project, SessionID: selection.Session}
		configRow.Plan.SchemaVersion = 1
		configRow.Plan.ItemCount = len(snapshot.Items)
		configRow.Plan.SnapshotDigest = snapshot.SnapshotDigest
		configRow.Plan.Policy = request.Policy
		configRow.Plan.ContextSHA = contextSHA
		config, err := canonical.Marshal(configRow)
		if err != nil {
			return err
		}
		configSHA, err := canonical.Hash(json.RawMessage(config))
		if err != nil {
			return err
		}
		recovery, err := canonical.Marshal(sessionAnalysisRecovery{SchemaVersion: 1,
			Snapshot: snapshot, Context: contextBody, ContextSHA: contextSHA,
			PolicySHA: policySHA, ModelLockSHA: request.ModelLockSHA,
			ReservedBytes: request.ReservedBytes})
		if err != nil || len(recovery) > 10485760 {
			return ErrInvalid
		}
		var outcome string
		var admitted *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT outcome,run_id FROM
		 photo.begin_v22_analysis_draft($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,
		 $10,$11,$12,$13,$14::jsonb,$15,$16,$17)`,
			selection.Tenant, selection.Run, workflow, project,
			snapshot.SnapshotID, selection.Session, selection.Snapshot,
			selection.ExpectedRevision, string(selection.ManualReferences),
			selection.Actor, selection.AuthSession, policySHA, configSHA,
			string(config), request.ReservedBytes, request.Key,
			requestSHA).Scan(&outcome, &admitted); err != nil {
			return err
		}
		switch outcome {
		case "conflict":
			return ErrConflict
		case "expired":
			return ErrExpired
		case "created", "pending", "replayed":
			if admitted == nil || *admitted != selection.Run {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		if outcome != "replayed" {
			var saved bool
			if err := tx.QueryRow(ctx, `SELECT photo.save_v22_analysis_recovery(
			 $1,$2,$3,$4,$5,$6,$7::jsonb)`, selection.Tenant, selection.Actor,
				selection.AuthSession, selection.Run, request.Key, requestSHA,
				string(recovery)).Scan(&saved); err != nil || !saved {
				if err != nil {
					return err
				}
				return ErrInvalid
			}
		}
		result = SessionAnalysisBeginResult{Outcome: outcome,
			Run: selection.Run, Workflow: workflow, RequestSHA: requestSHA,
			Snapshot:    snapshot,
			ContextBody: contextBody, ContextSHA: contextSHA,
			PolicySHA: policySHA, ModelLockSHA: request.ModelLockSHA,
			Project: project}
		return nil
	})
	if err != nil {
		return SessionAnalysisBeginResult{}, err
	}
	return result, nil
}
