package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

type SessionAnalysisBeginner interface {
	Begin(context.Context, SessionAnalysisBeginRequest) (SessionAnalysisBeginResult, error)
}

type SessionAnalysisPendingAdmitter interface {
	AdmitPending(context.Context, SessionAnalysisDraft) (JobTicket, error)
}

type SessionAnalysisProfileCompiler interface {
	Compile(context.Context, uuid.UUID, uuid.UUID, uuid.UUID,
		SessionInputSnapshot) ([]json.RawMessage, error)
}

type SessionAnalysisTicketReader interface {
	Ticket(context.Context, uuid.UUID, uuid.UUID, string, string) (JobTicket, error)
}

type SessionAnalysisReplayProber interface {
	Probe(context.Context, SessionAnalysisBeginRequest) (SessionAnalysisReplayResult, error)
}

// SessionAnalysisCoordinator is the complete two-phase V22 admission boundary
// for an already available draft recipe on every selected image. The first
// phase freezes/creates a draft transaction; the second publishes documents
// and seals the graph. An already sealed replay never publishes again.
type SessionAnalysisCoordinator struct {
	Beginner SessionAnalysisBeginner
	Prober   SessionAnalysisReplayProber
	Pending  SessionAnalysisPendingAdmitter
	Profiles SessionAnalysisProfileCompiler
	Tickets  SessionAnalysisTicketReader
}

func (c SessionAnalysisCoordinator) Probe(ctx context.Context,
	request SessionAnalysisBeginRequest) (SessionAnalysisReplayResult, error) {
	if c.Prober == nil {
		return SessionAnalysisReplayResult{}, ErrInvalid
	}
	return c.Prober.Probe(ctx, request)
}

func (c SessionAnalysisCoordinator) Admit(ctx context.Context,
	request SessionAnalysisBeginRequest) (JobTicket, error) {
	if c.Beginner == nil || c.Pending == nil || c.Profiles == nil || c.Tickets == nil {
		return JobTicket{}, ErrInvalid
	}
	begin, err := c.Beginner.Begin(ctx, request)
	if err != nil {
		return JobTicket{}, err
	}
	selection := request.Selection
	workflow, err := WorkflowID(selection.Run)
	if err != nil || begin.Run != selection.Run || begin.Workflow != workflow ||
		!digestPattern.MatchString(begin.RequestSHA) ||
		!digestPattern.MatchString(begin.PolicySHA) {
		return JobTicket{}, ErrInvalid
	}
	if begin.Outcome != "replayed" {
		if begin.Outcome != "created" && begin.Outcome != "pending" {
			return JobTicket{}, ErrInvalid
		}
		if !digestPattern.MatchString(begin.ModelLockSHA) {
			return JobTicket{}, ErrInvalid
		}
		var policy struct {
			Calibration Ref `json:"calibration"`
		}
		if json.Unmarshal(request.Policy, &policy) != nil ||
			!validRef(policy.Calibration) {
			return JobTicket{}, ErrInvalid
		}
		profiles, err := c.Profiles.Compile(ctx, selection.Tenant, begin.Run,
			begin.Workflow, begin.Snapshot)
		if err != nil {
			return JobTicket{}, err
		}
		draft := SessionAnalysisDraft{Tenant: selection.Tenant,
			Run: begin.Run, Workflow: begin.Workflow,
			Items: begin.Snapshot.Items, SnapshotDigest: begin.Snapshot.SnapshotDigest,
			ContextBody: begin.ContextBody, ContextSHA: begin.ContextSHA,
			Calibration: policy.Calibration, ModelLockSHA: begin.ModelLockSHA,
			PolicySHA: begin.PolicySHA, Profiles: profiles}
		if _, err := c.Pending.AdmitPending(ctx, draft); err != nil {
			return JobTicket{}, err
		}
	}
	ticket, err := c.Tickets.Ticket(ctx, selection.Tenant, selection.Actor,
		request.Key, begin.RequestSHA)
	if err != nil {
		return JobTicket{}, err
	}
	if ticket.RunID != begin.Run || ticket.WorkflowID != begin.Workflow ||
		ticket.State != "queued" {
		return JobTicket{}, ErrInvalid
	}
	return ticket, nil
}
