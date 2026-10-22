package v2sessions

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2resource"
)

type ReferenceChoice struct {
	PreviousAnalysisID uuid.UUID `json:"previous_analysis_id"`
	GroupID            uuid.UUID `json:"group_id"`
	AssetVersionID     uuid.UUID `json:"asset_version_id"`
}
type AnalysisRequest struct {
	ExpectedRevision    string            `json:"expected_revision"`
	SelectionSnapshotID uuid.UUID         `json:"selection_snapshot_id"`
	Policy              SessionPolicy     `json:"policy"`
	ManualReferences    []ReferenceChoice `json:"manual_references"`
}
type AnalysisPreflight struct {
	Context      json.RawMessage
	PolicySHA    string
	ModelLockSHA string
}

type AnalysisAdmission interface {
	Probe(context.Context, v2input.SessionAnalysisBeginRequest) (v2input.SessionAnalysisReplayResult, error)
	Admit(context.Context, v2input.SessionAnalysisBeginRequest) (v2input.JobTicket, error)
}

// Analyze returns a ticket only after graph publication and seal. A sealed
// replay is checked before the temporary selection or current recipes are
// read, but SQL still reauthorizes the current authenticated session.
func (s Service) Analyze(ctx context.Context, who auth.Identity, tenantID,
	sessionID uuid.UUID, key string, req AnalysisRequest) (v2input.JobTicket, bool, error) {
	var empty v2input.JobTicket
	if err := ctx.Err(); err != nil { return empty, false, err }
	if s.Admission == nil || s.DB == nil {
		return empty, false, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	expected, err := revision(req.ExpectedRevision)
	if err != nil || sessionID == uuid.Nil || req.SelectionSnapshotID == uuid.Nil ||
		!policyValid(req.Policy) || req.ManualReferences == nil ||
		len(req.ManualReferences) > 5000 {
		return empty, false, domain.NewError("VALIDATION_FAILED", 422)
	}
	run, err := v2input.RunID(tenantID, who.UserID, key)
	if err != nil {
		return empty, false, domain.NewError("VALIDATION_FAILED", 422)
	}
	policy, err := canonical.Marshal(req.Policy)
	if err != nil {
		return empty, false, err
	}
	manual, err := canonical.Marshal(req.ManualReferences)
	if err != nil {
		return empty, false, err
	}
	start := v2input.SessionAnalysisBeginRequest{Selection: v2input.SessionAnalysisSelection{
		Tenant: tenantID, Run: run, Session: sessionID,
		Snapshot: req.SelectionSnapshotID, Actor: who.UserID,
		AuthSession: who.SessionID, ExpectedRevision: expected,
		ManualReferences: manual}, Policy: policy, Key: key,
		ModelLockSHA:  "0000000000000000000000000000000000000000000000000000000000000000",
		ReservedBytes: 1}
	probe, err := s.Admission.Probe(ctx, start)
	if err != nil {
		return empty, false, analysisError(err)
	}
	if err := ctx.Err(); err != nil { return empty, false, err }
	switch probe.Outcome {
	case "conflict":
		return empty, false, domain.NewError("ANALYSIS_CONFLICT", 409)
	case "expired":
		return empty, false, domain.NewError("ANALYSIS_EXPIRED", 409)
	case "replayed":
		ticket, err := s.admitAnalysis(ctx, start)
		return ticket, true, err
	case "pending":
		ticket, err := s.admitAnalysis(ctx, start)
		return ticket, false, err
	case "missing":
	default:
		return empty, false, domain.NewError("INTERNAL_ERROR", 500)
	}
	preflight, err := s.Preflight(ctx, who, tenantID, sessionID, req)
	if err != nil {
		return empty, false, err
	}
	var contextBody struct {
		ItemCount string `json:"item_count"`
	}
	if json.Unmarshal(preflight.Context, &contextBody) != nil {
		return empty, false, domain.NewError("INTERNAL_ERROR", 500)
	}
	count, err := strconv.ParseInt(contextBody.ItemCount, 10, 64)
	if err != nil || count < 1 || count > 5000 {
		return empty, false, domain.NewError("INTERNAL_ERROR", 500)
	}
	start.ModelLockSHA = preflight.ModelLockSHA
	start.ReservedBytes = 128<<20 + count*(4<<20)
	ticket, err := s.admitAnalysis(ctx, start)
	return ticket, false, err
}

// Fresh, pending and sealed replay must acknowledge the same deterministic graph.
func (s Service) admitAnalysis(ctx context.Context, start v2input.SessionAnalysisBeginRequest) (v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil { return v2input.JobTicket{}, err }
	ticket, err := s.Admission.Admit(ctx, start)
	if err != nil { return v2input.JobTicket{}, analysisAdmissionError(err) }
	if err := ctx.Err(); err != nil { return v2input.JobTicket{}, err }
	workflow, err := v2input.WorkflowID(start.Selection.Run)
	if err != nil || ticket.RunID != start.Selection.Run || ticket.WorkflowID != workflow || ticket.State != "queued" {
		return v2input.JobTicket{}, domain.NewError("ANALYSIS_TICKET_INVALID", 500)
	}
	return ticket, nil
}

func analysisAdmissionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, v2input.ErrConflict) {
		return domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	if errors.Is(err, v2input.ErrExpired) {
		return domain.NewError("ANALYSIS_EXPIRED", 409)
	}
	if errors.Is(err, v2input.ErrInvalid) {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return analysisError(err)
}

// GetAnalysis returns an admitted analysis only when its plan document is
// ready. Project authorization is checked before loading its resource body.
func (s Service) GetAnalysis(ctx context.Context, who auth.Identity, tenantID,
	analysisID uuid.UUID) (json.RawMessage, error) {
	if s.DB == nil {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if analysisID == uuid.Nil {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	var out json.RawMessage
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.session_analyses
		 WHERE tenant_id=$1 AND id=$2`, tenantID, analysisID).Scan(&projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		out, e = v2resource.Load(ctx, tx, tenantID, analysisID, "session_analyses")
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		return e
	})
	return out, err
}

func analysisError(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.Code {
	case "40001", "23514":
		return domain.NewError("ANALYSIS_CONFLICT", 409)
	case "42501":
		return domain.NewError("FORBIDDEN", 403)
	case "22023":
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return err
}

// Preflight freezes the relational facts a later V21 compiler needs. It is
// also used by the read-only capability advisory. Advisory success is not
// admission: no draft shell, transport, seal or queued run exists until AdmitV2
// completes its full boundary.
func (s Service) Preflight(ctx context.Context, who auth.Identity, tenantID, sessionID uuid.UUID,
	req AnalysisRequest) (AnalysisPreflight, error) {
	var out AnalysisPreflight
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	expected, err := revision(req.ExpectedRevision)
	if err != nil || sessionID == uuid.Nil || req.SelectionSnapshotID == uuid.Nil ||
		!policyValid(req.Policy) || req.ManualReferences == nil || len(req.ManualReferences) > 5000 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	groups := make(map[uuid.UUID]bool, len(req.ManualReferences))
	for _, choice := range req.ManualReferences {
		if choice.PreviousAnalysisID == uuid.Nil || choice.GroupID == uuid.Nil ||
			choice.AssetVersionID == uuid.Nil || groups[choice.GroupID] {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		groups[choice.GroupID] = true
	}
	out.PolicySHA, err = canonical.Hash(req.Policy)
	if err != nil {
		return AnalysisPreflight{}, err
	}
	manualJSON, err := canonical.Marshal(req.ManualReferences)
	if err != nil {
		return AnalysisPreflight{}, err
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.shoot_sessions
		 WHERE tenant_id=$1 AND id=$2`, tenantID, sessionID).Scan(&projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		locks, e := qualifiedLock(ctx, tx, tenantID, projectID)
		if e != nil {
			return e
		}
		out.ModelLockSHA = locks.ModelSHA
		if e = s.verifyCalibration(ctx, tx, tenantID, req.Policy.Calibration, locks.ModelSHA, locks.DatasetSHA); e != nil {
			return e
		}
		return analysisError(tx.QueryRow(ctx, `SELECT photo.preflight_v22_analysis(
		 $1,$2,$3,$4,$5::jsonb,$6,$7)`, tenantID, sessionID, expected,
			req.SelectionSnapshotID, string(manualJSON), who.UserID, who.SessionID).Scan(&out.Context))
	})
	if err != nil {
		return AnalysisPreflight{}, err
	}
	return out, nil
}
