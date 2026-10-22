package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// SQLAuthoredSegmentContinuation connects concrete qualified preparation,
// protected publication/public seal, current canonical adoption readback and
// immutable phase commit. It never manufactures layer materialization or marks
// the parent complete; the next instruction must come from the committed ledger.
type SQLAuthoredSegmentContinuation struct {
	Preparation SQLAuthoredSegmentPreparation
	Producers   AuthoredSegmentProducerReader
	Objects     artifactjson.Objects
}

func (s SQLAuthoredSegmentContinuation) Resume(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int, currentLayers, protected *v2input.Ref) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.Preparation.Params.Scheduler == nil || s.Preparation.Params.Scheduler != s.Producers.Scheduler || s.Producers.Objects == nil || s.Objects == nil || s.Preparation.Params.Bucket != s.Producers.Bucket {
		return false, v2input.ErrInvalid
	}
	state, err := s.Preparation.ResumeAdmission(ctx, who, tenant, parent, ordinal, currentLayers, protected, s.Objects)
	if err != nil {
		return false, err
	}
	if state != "adopted" {
		return false, ErrAuthoredSegmentPending
	}
	source, current, err := ReadAuthoredInstructionSource(ctx, s.Producers.Scheduler, tenant, parent, ordinal)
	if err != nil {
		return false, err
	}
	if !current {
		return false, v2input.ErrExpired
	}
	if source.Actor != who.UserID || source.Session != who.SessionID {
		return false, v2input.ErrInvalid
	}
	return s.Producers.CommitCanonical(ctx, source)
}
