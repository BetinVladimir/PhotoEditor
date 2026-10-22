package v2render

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Admit performs actual Params publication, exact transport publication and
// public registered-input graph seal. It grants no completion or photographer
// approval; the still-unmounted installed source/adoption wrapper must refuse
// until the dedicated namespace has been integrated under its exact predecessor.
func (p AuthoredSkinPreparation) Admit(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, selection CurrentAuthoredFaceSelection, expected v2qualification.Expected, report v2input.Ref, limits AuthoredFaceBudget) error {
	frozen, err := p.Prepare(ctx, who, source, image, selection, expected, report, limits)
	if err != nil {
		return err
	}
	prepared, err := p.publishTransport(ctx, source, frozen)
	if err != nil {
		return err
	}
	if err := RecheckCurrentAuthoredFaceSelection(ctx, p.Scheduler, who, source, selection); err != nil {
		return err
	}
	return p.seal(ctx, source, "mask.face_skin", prepared)
}
