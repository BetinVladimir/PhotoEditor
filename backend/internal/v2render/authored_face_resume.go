package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

var ErrAuthoredFacePending = errors.New("AUTHORED_FACE_PENDING")

// ResumePrerequisite never restages a sealed task. A warning-bearing canonical
// observation is retained intact and may feed the explicitly reviewed candidate.
func (p AuthoredFacePreparation) ResumePrerequisite(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, expected v2qualification.Expected, report v2input.Ref, score float64, maxFaces int, limits AuthoredFaceBudget) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.Scheduler == nil || who.UserID != source.Actor || who.SessionID != source.Session {
		return false, v2input.ErrInvalid
	}
	if _, _, err := AuthoredFacePrerequisiteIDs(source, expected.Operation); err != nil {
		return false, err
	}
	var state *string
	err := database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.authored_render_face_preparation_state($1,$2,$3,$4)", source.Tenant, source.Parent, source.Instruction.Ordinal, expected.Operation).Scan(&state)
	})
	if err != nil {
		return false, err
	}
	if state == nil {
		return false, v2input.ErrExpired
	}
	switch *state {
	case "adopted":
		_, err = p.Producers.read(ctx, source, expected.Operation)
		return false, err
	case "pending":
		return false, ErrAuthoredFacePending
	case "absent", "draft":
		switch expected.Operation {
		case "face.detect":
			err = p.AdmitDetector(ctx, who, source, image, expected, report, score, maxFaces, limits)
		case "face.landmarks":
			err = p.AdmitLandmarks(ctx, who, source, image, expected, report, limits)
		case "face.parse":
			err = p.AdmitParser(ctx, who, source, image, expected, report, limits)
		default:
			return false, v2input.ErrInvalid
		}
		if err != nil {
			return false, err
		}
		return true, ErrAuthoredFacePending
	default:
		return false, v2input.ErrInvalid
	}
}
