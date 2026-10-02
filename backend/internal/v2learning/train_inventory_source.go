package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
)

type TrainingInventory struct {
	ProjectID uuid.UUID
	Mode      string
	PersonID  *uuid.UUID
	Rows      []TrainingCandidate
}

type SQLTrainingInventorySource struct {
	DB *pgxpool.Pool // photo_scheduler
}

// Load acquires the tenant/profile locks through the trusted candidate
// function, copies the exact current inventory and releases locks before any
// object IO. Freeze rechecks the selected candidates under fresh locks.
func (s SQLTrainingInventorySource) Load(ctx context.Context,
	start TrainingStart) (TrainingInventory, error) {
	var result TrainingInventory
	if s.DB == nil || start.TenantID == uuid.Nil ||
		start.ActorID == uuid.Nil || start.AuthSessionID == uuid.Nil ||
		start.ProfileID == uuid.Nil || start.ProjectID == uuid.Nil {
		return result, ErrInvalidTrainingDraft
	}
	revision, err := ValidateTrainCreate(start.Request)
	if err != nil {
		return result, ErrInvalidTrainingDraft
	}
	err = database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		rows, err := LoadTrainingCandidates(ctx, tx, start.TenantID,
			start.ProfileID, revision, start.Request.SessionIDs,
			start.ActorID, start.AuthSessionID)
		if err != nil {
			return err
		}
		var profile TrainingInventory
		err = tx.QueryRow(ctx, `SELECT project_id,mode,person_id FROM
		 photo.v23_training_inventory_profile($1,$2,$3,$4)`,
			start.TenantID, start.ProfileID, start.ActorID,
			start.AuthSessionID).Scan(&profile.ProjectID,
			&profile.Mode, &profile.PersonID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTrainingDraftConflict
		}
		if err != nil {
			return err
		}
		if profile.ProjectID != start.ProjectID ||
			profile.Mode != start.Request.Policy.Mode ||
			(profile.Mode == "person") != (profile.PersonID != nil) {
			return ErrTrainingDraftConflict
		}
		profile.Rows = rows
		result = profile
		return nil
	})
	if err != nil {
		return TrainingInventory{}, err
	}
	return result, nil
}
