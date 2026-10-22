package v2input

import (
	"context"
	"github.com/google/uuid"
)

type LensSelectionLoader interface {
	Load(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) ([]LensSelectedSource, error)
}
type LensProfileLoader interface {
	Load(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, Ref) ([]byte, error)
}

type LensGraphSealer interface {
	Seal(context.Context, uuid.UUID, PreparedLensDraft) error
}
type LensAdmissionRequest struct {
	Tenant, Actor, Project, Snapshot, Session uuid.UUID
	Key, PolicySHA                            string
	Choices                                   []LensProfileChoice
	Limits                                    LensAdmissionLimits
}

// LensAdmissionCoordinator joins API-authorized source reads and scheduler-only
// draft preparation. SQL sealing repeats scope, closure and qualification checks
// and is the only transition making the tasks dispatchable. Failed phases leave
// a resumable blocked draft; no success ticket is returned before sealing.
type LensAdmissionCoordinator struct {
	Selection         LensSelectionLoader
	Profiles          LensProfileLoader
	Drafts            LensDraftCoordinator
	Sealer            LensGraphSealer
	RuntimeLockSHA    string
	QualificationPins []LensQualificationPin
}

func (c LensAdmissionCoordinator) Admit(ctx context.Context, request LensAdmissionRequest) (JobTicket, error) {
	if c.Selection == nil || c.Profiles == nil || c.Sealer == nil || request.Project == uuid.Nil || request.Snapshot == uuid.Nil || request.Session == uuid.Nil || !digestPattern.MatchString(request.PolicySHA) {
		return JobTicket{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return JobTicket{}, err
	}
	run, err := RunID(request.Tenant, request.Actor, request.Key)
	if err != nil {
		return JobTicket{}, err
	}
	workflow, err := WorkflowID(run)
	if err != nil {
		return JobTicket{}, err
	}
	// Deployment pins are checked before catalog reads or draft writes.
	if !digestPattern.MatchString(c.RuntimeLockSHA) || len(c.QualificationPins) < 1 {
		return JobTicket{}, ErrInvalid
	}
	if len(request.Choices) < 1 || len(request.Choices) > 5000 {
		return JobTicket{}, ErrInvalid
	}
	request.Choices = append([]LensProfileChoice(nil), request.Choices...)
	for i, choice := range request.Choices {
		raw, err := c.Profiles.Load(ctx, request.Tenant, request.Project, request.Actor, request.Session, choice.Profile)
		if err != nil {
			return JobTicket{}, err
		}
		choice.ProfileJSON = append([]byte(nil), raw...)
		request.Choices[i] = choice
		if _, err := VerifyQualifiedLensProfile(choice.ProfileJSON, choice.Profile, c.RuntimeLockSHA, c.QualificationPins); err != nil {
			return JobTicket{}, err
		}
	}
	sources, err := c.Selection.Load(ctx, request.Tenant, request.Project, request.Snapshot, request.Actor, request.Session)
	if err != nil {
		return JobTicket{}, err
	}
	tasks, err := CompileLensSelection(workflow, sources, request.Choices, request.Limits)
	if err != nil {
		return JobTicket{}, err
	}
	prepared, err := c.Drafts.Prepare(ctx, request.Tenant, request.Actor, request.Project, request.Snapshot, request.Session, request.Key, request.PolicySHA, tasks)
	if err != nil {
		return JobTicket{}, err
	}
	if prepared.RunID != run || prepared.WorkflowID != workflow {
		return JobTicket{}, ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return JobTicket{}, err
	}
	if !prepared.Replayed {
 if err = c.Sealer.Seal(ctx, request.Tenant, prepared); err != nil {
		return JobTicket{}, err
	}
	}
	// A committed seal is recovered by the same immutable key. Cancellation must
	// never expose a late success ticket to a caller that lost its scope.
	if err = ctx.Err(); err != nil {
		return JobTicket{}, err
	}
	return JobTicket{RunID: run, WorkflowID: workflow, State: "queued"}, nil
}
