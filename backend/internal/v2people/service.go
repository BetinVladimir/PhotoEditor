package v2people

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2resource"
)

type Service struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
}

type Page struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

type CreateRequest struct {
	ProjectID uuid.UUID `json:"project_id"`
	Label     string    `json:"label"`
}
type CASRequest struct {
	ExpectedRevision string `json:"expected_revision"`
}
type ConsentRequest struct {
	ExpectedRevision string          `json:"expected_revision"`
	Purpose          string          `json:"purpose"`
	PersonID         json.RawMessage `json:"person_id"`
	Action           string          `json:"action"`
	PolicyVersion    string          `json:"policy_version"`
}

func revision(raw string) (int64, error) {
	if len(raw) == 0 || len(raw) > 19 || raw[0] == '0' {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > 9007199254740991 {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	return n, nil
}

func mutationError(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.Code {
	case "40001", "23505", "23514":
		return domain.NewError("RESOURCE_CONFLICT", 409)
	case "42501":
		return domain.NewError("FORBIDDEN", 403)
	case "P0002":
		return domain.NewError("NOT_FOUND", 404)
	case "22023":
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return err
}

func (s Service) Create(ctx context.Context, who auth.Identity, tenantID uuid.UUID,
	key string, req CreateRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if req.ProjectID == uuid.Nil || !utf8.ValidString(req.Label) ||
		len([]rune(req.Label)) < 1 || len([]rune(req.Label)) > 120 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, err := project.Authorize(ctx, tx, tenantID, req.ProjectID, who.UserID,
			role, domain.Editor, false); err != nil {
			return err
		}
		var err error
		out, err = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID,
			Principal: who.UserID, PrincipalType: "user", Route: "V22.CreatePerson", Key: key},
			req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				var body json.RawMessage
				e := mutationError(tx.QueryRow(ctx, `SELECT photo.create_v22_person($1,$2,$3,$4,$5)`,
					tenantID, req.ProjectID, req.Label, who.UserID, who.SessionID).Scan(&body))
				if e != nil {
					return idempotency.Response{}, e
				}
				id, e := v2resource.ID(body)
				if e != nil {
					return idempotency.Response{}, e
				}
				body, e = v2resource.Load(ctx, tx, tenantID, id, "project_persons")
				return idempotency.Response{Status: 201, Body: body, ResourceID: &id}, e
			})
		return err
	})
	return out, err
}

func (s Service) Revoke(ctx context.Context, who auth.Identity, tenantID,
	personID uuid.UUID, key string, req CASRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	expected, err := revision(req.ExpectedRevision)
	if err != nil || personID == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Owner, func(tx pgx.Tx, _ domain.Role) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT project_id FROM photo.project_persons
		 WHERE tenant_id=$1 AND id=$2`, tenantID, personID).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		var useErr error
		out, useErr = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID,
			Principal: who.UserID, PrincipalType: "user",
			Route: "V22.RevokePerson:" + personID.String(), Key: key},
			req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				var body json.RawMessage
				e := mutationError(tx.QueryRow(ctx, `SELECT photo.revoke_v22_person($1,$2,$3,$4,$5,$6)`,
					tenantID, projectID, personID, expected, who.UserID, who.SessionID).Scan(&body))
				if e != nil {
					return idempotency.Response{}, e
				}
				body, e = v2resource.Load(ctx, tx, tenantID, personID, "project_persons")
				return idempotency.Response{Status: 200, Body: body, ResourceID: &personID}, e
			})
		return useErr
	})
	return out, err
}

func (s Service) ChangeConsent(ctx context.Context, who auth.Identity, tenantID,
	projectID uuid.UUID, key string, req ConsentRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	expected, err := revision(req.ExpectedRevision)
	if err != nil || projectID == uuid.Nil || len(req.PersonID) == 0 ||
		len(req.PolicyVersion) < 1 || len([]rune(req.PolicyVersion)) > 80 ||
		(req.Action != "grant" && req.Action != "revoke") {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var personID *uuid.UUID
	if string(req.PersonID) != "null" {
		var parsed uuid.UUID
		if json.Unmarshal(req.PersonID, &parsed) != nil || parsed == uuid.Nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		personID = &parsed
	}
	if (req.Purpose == "learning_photographer") != (personID == nil) ||
		(req.Purpose != "identity_matching" && req.Purpose != "learning_person" &&
			req.Purpose != "learning_photographer") {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Owner, func(tx pgx.Tx, _ domain.Role) error {
		var consentID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM photo.consent_grants WHERE tenant_id=$1
		 AND project_id=$2 AND purpose=$3 AND person_id IS NOT DISTINCT FROM $4::uuid`,
			tenantID, projectID, req.Purpose, personID).Scan(&consentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		var useErr error
		out, useErr = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID,
			Principal: who.UserID, PrincipalType: "user",
			Route: "V22.ChangeConsent:" + projectID.String(), Key: key},
			req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				var body json.RawMessage
				e := mutationError(tx.QueryRow(ctx, `SELECT photo.change_v2_consent(
				 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenantID, projectID, consentID,
					expected, req.Purpose, personID, req.Action, req.PolicyVersion,
					who.UserID, who.SessionID).Scan(&body))
				if e != nil {
					return idempotency.Response{}, e
				}
				body, e = v2resource.Load(ctx, tx, tenantID, consentID, "consent_grants")
				return idempotency.Response{Status: 200, Body: body, ResourceID: &consentID}, e
			})
		return useErr
	})
	return out, err
}

// List uses a project-scoped, signed (created_at,id) cursor. No count or
// offset is exposed, so concurrent inserts cannot shift an existing page.
func (s Service) List(ctx context.Context, who auth.Identity, tenantID,
	projectID uuid.UUID, kind string, limit int, rawCursor string) (Page, error) {
	out := Page{Items: []json.RawMessage{}}
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if projectID == uuid.Nil || limit < 1 || limit > 100 || len(rawCursor) > 2048 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var table string
	var required domain.Role
	switch kind {
	case "project_persons":
		table, required = "photo.project_persons", domain.Editor
	case "consent_grants":
		table, required = "photo.consent_grants", domain.Owner
	default:
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	filterHash, err := canonical.Hash(struct {
		ProjectID uuid.UUID `json:"project_id"`
	}{projectID})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tenantID.String(), Principal: who.UserID.String(),
		FilterHash: filterHash, Sort: "v2:" + kind + ":created_at,id"}
	lastTime := time.Time{}
	lastID := uuid.Nil
	if rawCursor != "" {
		decoded, err := s.Cursors.Decode(rawCursor, scope)
		if err != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		lastTime, err = time.Parse(time.RFC3339Nano, decoded.LastPosition)
		if err != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		lastID, err = uuid.Parse(decoded.LastID)
		if err != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	type position struct {
		id      uuid.UUID
		created time.Time
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, required, func(tx pgx.Tx, role domain.Role) error {
		if _, e := project.Authorize(ctx, tx, tenantID, projectID, who.UserID,
			role, domain.Viewer, false); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id,created_at FROM `+table+`
		 WHERE tenant_id=$1 AND project_id=$2 AND (created_at,id)>($3,$4)
		 ORDER BY created_at,id LIMIT $5`, tenantID, projectID, lastTime, lastID, limit+1)
		if e != nil {
			return e
		}
		positions := make([]position, 0, limit+1)
		for rows.Next() {
			var p position
			if e = rows.Scan(&p.id, &p.created); e != nil {
				rows.Close()
				return e
			}
			positions = append(positions, p)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		hasNext := len(positions) > limit
		if hasNext {
			positions = positions[:limit]
		}
		for _, p := range positions {
			body, e := v2resource.Load(ctx, tx, tenantID, p.id, kind)
			if e != nil {
				return e
			}
			out.Items = append(out.Items, body)
		}
		if hasNext {
			last := positions[len(positions)-1]
			scope.LastPosition = last.created.UTC().Format(time.RFC3339Nano)
			scope.LastID = last.id.String()
			next, e := s.Cursors.Encode(scope)
			if e != nil {
				return e
			}
			if len(next) > 2048 {
				return domain.NewError("CURSOR_UNAVAILABLE", 503)
			}
			out.NextCursor = &next
		}
		return nil
	})
	return out, err
}
