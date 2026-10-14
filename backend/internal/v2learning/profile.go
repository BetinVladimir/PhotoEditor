package v2learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type ProfileCreate struct {
	ProjectID uuid.UUID  `json:"project_id"`
	Name      string     `json:"name"`
	Mode      string     `json:"mode"`
	PersonID  *uuid.UUID `json:"person_id"`
}

func (r *ProfileCreate) UnmarshalJSON(body []byte) error {
	fields, err := closedLearningObject(body)
	if err != nil {
		return err
	}
	if len(fields) != 4 {
		return fmt.Errorf("ProfileCreate requires four exact fields")
	}
	for _, key := range []string{"project_id", "name", "mode", "person_id"} {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("ProfileCreate missing %s", key)
		}
	}
	type wire ProfileCreate
	var decoded wire
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	*r = ProfileCreate(decoded)
	return nil
}

type ProfileGet struct {
	ID              uuid.UUID  `json:"id"`
	Revision        string     `json:"revision"`
	Mode            string     `json:"mode"`
	PersonID        *uuid.UUID `json:"person_id"`
	ActiveVersionID *uuid.UUID `json:"active_version_id"`
	ConsentState    string     `json:"consent_state"`
}

type ProfileService struct {
	DB       *pgxpool.Pool
	Cursors  pagination.Codec
	Training TrainingAdmission
}

func validProfileCreate(req ProfileCreate) bool {
	if req.ProjectID == uuid.Nil || !utf8.ValidString(req.Name) ||
		len([]rune(req.Name)) < 1 || len([]rune(req.Name)) > 120 ||
		(req.PersonID != nil && *req.PersonID == uuid.Nil) {
		return false
	}
	return req.Mode == "photographer" && req.PersonID == nil ||
		req.Mode == "person" && req.PersonID != nil
}

func profileGet(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenantID, profileID uuid.UUID, role domain.Role) (ProfileGet, error) {
	var out ProfileGet
	var projectID uuid.UUID
	var revision int64
	err := tx.QueryRow(ctx, `SELECT project_id,revision,mode,person_id,
	 active_version_id FROM photo.learning_profiles
	 WHERE tenant_id=$1 AND id=$2`, tenantID, profileID).Scan(
		&projectID, &revision, &out.Mode, &out.PersonID, &out.ActiveVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return out, err
	}
	if _, err = project.Authorize(ctx, tx, tenantID, projectID,
		who.UserID, role, domain.Viewer, false); err != nil {
		return out, err
	}
	out.ID = profileID
	out.Revision = strconv.FormatInt(revision, 10)
	out.ConsentState = "disabled"
	purpose := "learning_photographer"
	if out.Mode == "person" {
		purpose = "learning_person"
	}
	var grantStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM photo.consent_grants
	 WHERE tenant_id=$1 AND project_id=$2 AND purpose=$3
	 AND person_id IS NOT DISTINCT FROM $4::uuid
	 ORDER BY created_at DESC,id DESC LIMIT 1`, tenantID, projectID,
		purpose, out.PersonID).Scan(&grantStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if grantStatus == "granted" {
		out.ConsentState = "enabled"
	} else {
		out.ConsentState = "revoked"
	}
	if out.PersonID != nil {
		var personState string
		err = tx.QueryRow(ctx, `SELECT state FROM photo.project_persons
		 WHERE tenant_id=$1 AND project_id=$2 AND id=$3`, tenantID,
			projectID, *out.PersonID).Scan(&personState)
		if err != nil {
			return out, err
		}
		if personState != "active" {
			out.ConsentState = "revoked"
		}
	}
	return out, nil
}

func (s ProfileService) GetProfile(ctx context.Context, who auth.Identity,
	tenantID, profileID uuid.UUID) (ProfileGet, error) {
	var out ProfileGet
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if profileID == uuid.Nil {
		return out, domain.NewError("NOT_FOUND", 404)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
		func(tx pgx.Tx, role domain.Role) error {
			var e error
			out, e = profileGet(ctx, tx, who, tenantID, profileID, role)
			return e
		})
	return out, correctionError(err)
}

func (s ProfileService) CreateProfile(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, key string, req ProfileCreate) (idempotency.Response, error) {
	var out idempotency.Response
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if !validProfileCreate(req) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			if _, e := project.Authorize(ctx, tx, tenantID, req.ProjectID,
				who.UserID, role, domain.Editor, false); e != nil {
				return e
			}
			var e error
			out, e = idempotency.Use(ctx, tx, idempotency.Scope{
				Tenant: tenantID, Principal: who.UserID, PrincipalType: "user",
				Route: "V23.CreateProfile", Key: key,
			}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				var profileID uuid.UUID
				if e := tx.QueryRow(ctx, `SELECT photo.create_v23_profile(
				 $1,$2,$3,$4,$5,$6,$7)`, tenantID, req.ProjectID,
					req.Name, req.Mode, req.PersonID, who.UserID,
					who.SessionID).Scan(&profileID); e != nil {
					return idempotency.Response{}, correctionError(e)
				}
				profile, e := profileGet(ctx, tx, who, tenantID, profileID, role)
				if e != nil {
					return idempotency.Response{}, e
				}
				body, e := json.Marshal(profile)
				return idempotency.Response{Status: 201, Body: body,
					ResourceID: &profileID}, e
			})
			return e
		})
	return out, correctionError(err)
}
