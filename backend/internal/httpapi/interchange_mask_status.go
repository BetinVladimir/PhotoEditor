package httpapi

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
)

type importMaskConfirmationStatus struct {
	EntryID         uuid.UUID `json:"entry_id"`
	OverrideID      uuid.UUID `json:"override_artifact_id"`
	MaskID          uuid.UUID `json:"mask_artifact_id"`
	TargetVersionID uuid.UUID `json:"target_version_id"`
	Status          string    `json:"status"`
	Epoch           int64     `json:"epoch"`
}

func validateImportMaskStatuses(items []importMaskConfirmationStatus) error {
	if len(items) > 5000 {
		return domain.NewError("INTERCHANGE_MASK_STATUS_INVALID", 503)
	}
	seen := make(map[[4]uuid.UUID]bool, len(items))
	for _, item := range items {
		key := [4]uuid.UUID{item.EntryID, item.OverrideID, item.MaskID, item.TargetVersionID}
		if item.EntryID == uuid.Nil || item.OverrideID == uuid.Nil || item.MaskID == uuid.Nil || item.TargetVersionID == uuid.Nil || item.Epoch < 1 || item.Epoch > 9007199254740991 || (item.Status != "granted" && item.Status != "revoked") || seen[key] {
			return domain.NewError("INTERCHANGE_MASK_STATUS_INVALID", 503)
		}
		seen[key] = true
	}
	return nil
}

func (s *Server) serveInterchangeMaskStatus(w http.ResponseWriter, r *http.Request, who auth.Identity, tenantID, project, workflow uuid.UUID) {
	if s.DB == nil {
		writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
		return
	}
	items := make([]importMaskConfirmationStatus, 0)
	err := tenant.WithTenant(r.Context(), s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(r.Context(), `SELECT entry_id,override_id,mask_id,target_version_id,status,epoch FROM photo.v30_import_person_mask_confirmation_status($1,$2,$3,$4,$5)`, tenantID, who.UserID, who.SessionID, project, workflow)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item importMaskConfirmationStatus
			if err := rows.Scan(&item.EntryID, &item.OverrideID, &item.MaskID, &item.TargetVersionID, &item.Status, &item.Epoch); err != nil {
				return err
			}
			if len(items) >= 5000 || item.EntryID == uuid.Nil || item.OverrideID == uuid.Nil || item.MaskID == uuid.Nil || item.TargetVersionID == uuid.Nil || item.Epoch < 1 || (item.Status != "granted" && item.Status != "revoked") {
				return domain.NewError("INTERCHANGE_MASK_STATUS_INVALID", 503)
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := validateImportMaskStatuses(items); err != nil {
		writeError(w, r, err)
		return
	}
	if r.Context().Err()!=nil{return}
	writeJSON(w, http.StatusOK, struct {
		WorkflowID uuid.UUID                      `json:"workflow_id"`
		Items      []importMaskConfirmationStatus `json:"items"`
	}{workflow, items})
}
