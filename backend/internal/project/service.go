package project

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/tenant"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
}
type Project struct {
	ID              uuid.UUID `json:"id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Visibility      string    `json:"visibility"`
	DownloadAllowed bool      `json:"download_allowed"`
	Revision        string    `json:"revision"`
	CreatedAt       time.Time `json:"created_at"`
}
type Create struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	Visibility      string `json:"visibility"`
	DownloadAllowed bool   `json:"download_allowed"`
}
type Patch struct {
	Name            *string `json:"name,omitempty"`
	Description     *string `json:"description,omitempty"`
	Visibility      *string `json:"visibility,omitempty"`
	DownloadAllowed *bool   `json:"download_allowed,omitempty"`
}
type DeleteRequest struct {
	ExpectedRevision  int64     `json:"expected_revision"`
	ConfirmResourceID uuid.UUID `json:"confirm_resource_id"`
}
type JobAccepted struct {
	JobID  uuid.UUID `json:"job_id"`
	Status string    `json:"status"`
}
type GrantRequest struct {
	Role domain.Role `json:"role"`
}
type ProjectACL struct {
	ProjectID uuid.UUID   `json:"project_id"`
	UserID    uuid.UUID   `json:"user_id"`
	Role      domain.Role `json:"role"`
	Revision  string      `json:"revision"`
}

func valid(name, description, visibility string) bool {
	return strings.TrimSpace(name) != "" && len([]rune(name)) <= 120 && len([]rune(description)) <= 2000 && (visibility == "workspace" || visibility == "restricted")
}

const fields = "p.id,p.tenant_id,p.name,p.description,p.visibility,p.download_allowed,p.revision,p.created_at"

func scan(row pgx.Row) (Project, error) {
	var p Project
	var rev int64
	err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.Description, &p.Visibility, &p.DownloadAllowed, &rev, &p.CreatedAt)
	p.Revision = strconv.FormatInt(rev, 10)
	return p, err
}

// Authorize returns 404 for a project invisible to this principal and 403 only
// after its visibility is established. ACL permissions never elevate tenant roles.
func Authorize(ctx context.Context, tx pgx.Tx, tid, id, user uuid.UUID, role, required domain.Role, lock bool) (Project, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF p"
	}
	p, err := scan(tx.QueryRow(ctx, "SELECT "+fields+" FROM photo.projects p WHERE p.tenant_id=$1 AND p.id=$2 AND p.archived_at IS NULL"+suffix, tid, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return p, err
	}
	effective := role
	if !role.Allows(domain.Admin) && p.Visibility == "restricted" {
		var grant domain.Role
		err = tx.QueryRow(ctx, "SELECT role FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3", tid, id, user).Scan(&grant)
		if errors.Is(err, pgx.ErrNoRows) {
			return p, domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return p, err
		}
		if !grant.Allows(effective) {
			effective = grant
		}
	}
	if !effective.Allows(required) {
		return p, domain.NewError("FORBIDDEN", 403)
	}
	return p, nil
}
func (s *Service) Get(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (Project, error) {
	var out Project
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var e error
		out, e = Authorize(ctx, tx, tid, id, who.UserID, role, domain.Viewer, false)
		return e
	})
	return out, err
}
func (s *Service) Create(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req Create) (idempotency.Response, error) {
	var out idempotency.Response
	if !valid(req.Name, req.Description, req.Visibility) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var err error
		out, err = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Project.Create", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			p, e := scan(tx.QueryRow(ctx, `INSERT INTO photo.projects AS p(tenant_id,name,description,visibility,download_allowed) VALUES($1,$2,$3,$4,$5) RETURNING `+fields, tid, req.Name, req.Description, req.Visibility, req.DownloadAllowed))
			if e != nil {
				return idempotency.Response{}, e
			}
			if req.Visibility == "restricted" {
				if _, e = tx.Exec(ctx, "INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')", tid, p.ID, who.UserID); e != nil {
					return idempotency.Response{}, e
				}
			}
			if e = audit(ctx, tx, who, tid, p.ID, "project.create"); e != nil {
				return idempotency.Response{}, e
			}
			b, e := canonical.Marshal(p)
			return idempotency.Response{Status: 201, Body: b, ResourceID: &p.ID}, e
		})
		return err
	})
	return out, err
}
func (s *Service) Update(ctx context.Context, who auth.Identity, tid, id uuid.UUID, expected int64, patch Patch) (Project, error) {
	var out Project
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		p, err := Authorize(ctx, tx, tid, id, who.UserID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if p.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if patch.Name != nil {
			p.Name = *patch.Name
		}
		if patch.Description != nil {
			p.Description = *patch.Description
		}
		if patch.Visibility != nil {
			p.Visibility = *patch.Visibility
		}
		if patch.DownloadAllowed != nil {
			p.DownloadAllowed = *patch.DownloadAllowed
		}
		if !valid(p.Name, p.Description, p.Visibility) {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		out, err = scan(tx.QueryRow(ctx, `UPDATE photo.projects AS p SET name=$3,description=$4,visibility=$5,download_allowed=$6,revision=revision+1 WHERE tenant_id=$1 AND id=$2 RETURNING `+fields, tid, id, p.Name, p.Description, p.Visibility, p.DownloadAllowed))
		if err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, id, "project.update")
	})
	return out, err
}
func (s *Service) SetGrant(ctx context.Context, who auth.Identity, tid, id, user uuid.UUID, expected int64, grant domain.Role) (ProjectACL, error) {
	var out ProjectACL
	if grant != domain.Viewer && grant != domain.Editor {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Admin, func(tx pgx.Tx, role domain.Role) error {
		p, err := Authorize(ctx, tx, tid, id, who.UserID, role, domain.Admin, true)
		if err != nil {
			return err
		}
		if p.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		var member bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2 AND status='active')", tid, user).Scan(&member); err != nil {
			return err
		}
		if !member {
			return domain.NewError("NOT_FOUND", 404)
		}
		_, err = tx.Exec(ctx, `INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,project_id,user_id) DO UPDATE SET role=EXCLUDED.role`, tid, id, user, grant)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE photo.projects SET revision=revision+1 WHERE tenant_id=$1 AND id=$2", tid, id); err != nil {
			return err
		}
		out = ProjectACL{ProjectID: id, UserID: user, Role: grant, Revision: strconv.FormatInt(expected+1, 10)}
		return audit(ctx, tx, who, tid, id, "project.grant")
	})
	return out, err
}

func (s *Service) Grant(ctx context.Context, who auth.Identity, tid, id, user uuid.UUID, expected int64, grant domain.Role) error {
	_, err := s.SetGrant(ctx, who, tid, id, user, expected, grant)
	return err
}

func (s *Service) Archive(ctx context.Context, who auth.Identity, tid, id uuid.UUID, expected int64, req DeleteRequest) (JobAccepted, error) {
	var out JobAccepted
	if expected < 1 || req.ExpectedRevision != expected || req.ConfirmResourceID != id {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Admin, func(tx pgx.Tx, role domain.Role) error {
		current, err := Authorize(ctx, tx, tid, id, who.UserID, role, domain.Admin, true)
		if err != nil {
			return err
		}
		if current.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.projects SET archived_at=clock_timestamp(),revision=revision+1 WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL`, tid, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.share_links s SET revoked_at=coalesce(s.revoked_at,clock_timestamp()),policy_version=CASE WHEN s.revoked_at IS NULL THEN s.policy_version+1 ELSE s.policy_version END FROM photo.collections c WHERE s.tenant_id=$1 AND c.tenant_id=s.tenant_id AND c.id=s.collection_id AND c.project_id=$2`, tid, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.share_sessions ss SET revoked_at=coalesce(ss.revoked_at,clock_timestamp()) FROM photo.collections c WHERE ss.tenant_id=$1 AND c.tenant_id=ss.tenant_id AND c.id=ss.collection_id AND c.project_id=$2`, tid, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.runs r SET cancel_requested=true,status=CASE WHEN r.status IN ('draft','queued','running','waiting_review') THEN 'cancelling' ELSE r.status END,revision=r.revision+1 WHERE r.tenant_id=$1 AND r.status NOT IN ('completed','completed_with_errors','failed','cancelled') AND (EXISTS(SELECT FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id WHERE s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id AND c.project_id=$2) OR EXISTS(SELECT FROM photo.collections c WHERE c.tenant_id=r.tenant_id AND c.id=r.output_collection_id AND c.project_id=$2))`, tid, id); err != nil {
			return err
		}
		out = JobAccepted{JobID: uuid.New(), Status: "queued"}
		if _, err = tx.Exec(ctx, `INSERT INTO photo.deletion_jobs(id,tenant_id,resource_type,resource_id,status,requested_by) VALUES($1,$2,'project',$3,'queued',$4)`, out.JobID, tid, id, who.UserID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, id, "project.archive")
	})
	return out, err
}
func audit(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, id uuid.UUID, action string) error {
	_, err := tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES($1,'user',$2,$3,'project',$4,$5,'{}')`, tid, who.UserID, action, id, uuid.New())
	return err
}

func (s *Service) List(ctx context.Context, who auth.Identity, tid uuid.UUID, limit int, raw, search string) (pagination.Page[Project], error) {
	out := pagination.Page[Project]{Items: []Project{}}
	if limit < 1 || limit > 100 || len([]rune(search)) > 200 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	filterHash, err := canonical.Hash(map[string]string{"search": search})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: filterHash, Sort: "project:created_at,id"}
	var afterID *uuid.UUID
	var afterTime *time.Time
	if raw != "" {
		cursor, err := s.Cursors.Decode(raw, scope)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, err := uuid.Parse(cursor.LastID)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		date, err := time.Parse(time.RFC3339Nano, cursor.LastPosition)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterID = &id
		afterTime = &date
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		out.Items = []Project{}
		rows, err := tx.Query(ctx, `SELECT `+fields+` FROM photo.projects p WHERE p.tenant_id=$1 AND p.archived_at IS NULL AND ($2::boolean OR p.visibility='workspace' OR EXISTS(SELECT 1 FROM photo.project_acl a WHERE a.tenant_id=p.tenant_id AND a.project_id=p.id AND a.user_id=$3)) AND ($4::uuid IS NULL OR (p.created_at,p.id)>($5::timestamptz,$4::uuid)) AND strpos(lower(p.name),lower($6))>0 ORDER BY p.created_at,p.id LIMIT $7`, tid, role.Allows(domain.Admin), who.UserID, afterID, afterTime, search, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scan(rows)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, p)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID = last.ID.String()
		scope.LastPosition = last.CreatedAt.Format(time.RFC3339Nano)
		cursor, err := s.Cursors.Encode(scope)
		if err != nil {
			return out, err
		}
		out.NextCursor = &cursor
	}
	return out, nil
}
