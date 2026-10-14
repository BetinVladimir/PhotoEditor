package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"time"
)

type LocalPortraitSavedOwner struct {
	Request       json.RawMessage
	TaskPlan      json.RawMessage
	PolicySHA     string
	ReservedBytes int64
}

// Load reads bounded metadata with live caller scope. It never reads objects
// inside the transaction and does not restore authority from historical rows.
func (s SQLLocalPortraitOwnerScope) Load(ctx context.Context, who auth.Identity, tenant, run uuid.UUID) (LocalPortraitSavedOwner, error) {
	var result LocalPortraitSavedOwner
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return result, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := database.InTransaction(bounded, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		rows, err := tx.Query(bounded, "SELECT request,task_plan,policy_sha,reserved_bytes,config_sha FROM photo.read_v28_local_recovery_owner($1,$2,$3,$4)", tenant, run, who.UserID, who.SessionID)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return v2input.ErrExpired
		}
		var request, plan []byte
		var policy, configSHA string
		var reserved int64
		if err := rows.Scan(&request, &plan, &policy, &reserved, &configSHA); err != nil {
			return err
		}
		if rows.Next() {
			return v2input.ErrInvalid
		}
		if err := rows.Err(); err != nil {
			return err
		}
		result, err = decodeLocalPortraitSavedOwner(who, tenant, run, request, plan, policy, reserved, configSHA)
		return err
	})
	if err != nil {
		return LocalPortraitSavedOwner{}, err
	}
	return result, nil
}

func decodeLocalPortraitSavedOwner(who auth.Identity, tenant, run uuid.UUID, requestRaw, planRaw []byte, policy string, reserved int64, configSHA string) (LocalPortraitSavedOwner, error) {
	var zero LocalPortraitSavedOwner
	if len(requestRaw) < 2 || len(requestRaw) > 1048576 || len(planRaw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	var request LocalPortraitOwnerRequest
	if json.Unmarshal(requestRaw, &request) != nil || request.Tenant != tenant || request.Run != run || request.Actor != who.UserID || request.Session != who.SessionID {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(request)
	normalized, e := canonical.Bytes(requestRaw)
	if err != nil || e != nil || !bytes.Equal(exact, normalized) {
		return zero, v2input.ErrInvalid
	}
	config, err := localPortraitOwnerConfig(exact, policy, reserved)
	if err != nil {
		return zero, err
	}
	sum := sha256.Sum256(config)
	if hex.EncodeToString(sum[:]) != configSHA {
  // Recovery accepts only one of the two complete compiler outputs, never a
  // stripped/normalized config. SQL independently binds the saved full hash.
  queued,err:=localPortraitOwnerConfigForQueue(exact,policy,reserved,"interactive")
  if err!=nil{return zero,err}
  queuedSum:=sha256.Sum256(queued)
  if hex.EncodeToString(queuedSum[:])!=configSHA{return zero,v2input.ErrInvalid}
 }
	var saved json.RawMessage
	if len(planRaw) > 0 && string(planRaw) != "null" {
		var plan LocalPortraitDocumentPlan
		if json.Unmarshal(planRaw, &plan) != nil || plan.SchemaVersion != 1 {
			return zero, v2input.ErrInvalid
		}
		saved, err = canonical.Marshal(plan)
		normalized, e = canonical.Bytes(planRaw)
		if err != nil || e != nil || !bytes.Equal(saved, normalized) {
			return zero, v2input.ErrInvalid
		}
	}
	return LocalPortraitSavedOwner{Request: exact, TaskPlan: saved, PolicySHA: policy, ReservedBytes: reserved}, nil
}
