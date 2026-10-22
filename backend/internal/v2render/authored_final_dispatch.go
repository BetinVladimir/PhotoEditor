package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Final continuation authorizes no output by a state string: adopted only
// triggers independent physical readback, technical completion and ledger append.
type SQLAuthoredFinalContinuation struct {
	Preparation SQLAuthoredFinalPreparation
	Producers   AuthoredFinalProducerReader
	Objects     artifactjson.Objects
}

func (s SQLAuthoredFinalContinuation) Resume(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	db := s.Preparation.Params.Continuation.Scheduler
	if db == nil || db != s.Producers.Scheduler || s.Objects == nil {
		return false, v2input.ErrInvalid
	}
	state, err := s.Preparation.ResumeAdmission(ctx, who, tenant, parent, ordinal, s.Objects)
	if err != nil {
		return false, err
	}
	if state != "adopted" {
		return false, ErrAuthoredFinalPending
	}
	source, current, err := ReadAuthoredInstructionSource(ctx, db, tenant, parent, ordinal)
	if err != nil {
		return false, err
	}
	if !current {
		return false, v2input.ErrExpired
	}
	if source.Actor != who.UserID || source.Session != who.SessionID || source.Instruction.Kind != "finalize" {
		return false, v2input.ErrInvalid
	}
	return s.Producers.CommitCanonical(ctx, source)
}
