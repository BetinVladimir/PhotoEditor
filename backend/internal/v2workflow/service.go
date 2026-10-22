package v2workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type Service struct{ DB *pgxpool.Pool }

type Ref struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}

type Status struct {
	RunID      uuid.UUID `json:"run_id"`
	WorkflowID uuid.UUID `json:"workflow_id"`
	Revision   string    `json:"revision"`
	State      string    `json:"state"`
	Outputs    []Ref     `json:"outputs"`
	ErrorCode  *string   `json:"error_code"`
}

type CancelRequest struct {
	ExpectedRevision string `json:"expected_revision"`
}

func expectedRevision(raw string) (int64, error) {
	if raw == "" || len(raw) > 19 || raw[0] == '0' {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	for _, digit := range raw {
		if digit < '0' || digit > '9' {
			return 0, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	return value, nil
}

func unavailable(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "42P01" {
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return err
}

func (s Service) authorized(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenantID, workflowID uuid.UUID, role domain.Role, required domain.Role) (uuid.UUID, error) {
	var projectID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT project_id FROM photo.v2_workflows
	 WHERE tenant_id=$1 AND id=$2`, tenantID, workflowID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return uuid.Nil, err
	}
	role,err=tenant.AuthorizeInTransaction(ctx,tx,who,tenantID,required)
	if err!=nil{return uuid.Nil,err}
	_, err = project.Authorize(ctx, tx, tenantID, projectID,
		who.UserID, role, required, false)
	return projectID, err
}

func readStatus(ctx context.Context, tx pgx.Tx, tenantID, workflowID uuid.UUID,
	lock bool) (Status, error) {
	out := Status{Outputs: []Ref{}}
	var revision int64
	var query = `SELECT w.run_id,r.revision,r.status FROM photo.v2_workflows w
	 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
	 WHERE w.tenant_id=$1 AND w.id=$2 AND r.kind='v2'`
	if lock {
		query += ` FOR UPDATE OF r`
	}
	err := tx.QueryRow(ctx, query, tenantID, workflowID).Scan(&out.RunID, &revision, &out.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{}, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return Status{}, err
	}
	if out.State == "draft" {
		return Status{}, domain.NewError("WORKFLOW_NOT_READY", 409)
	}
	switch out.State {
	case "queued", "running", "waiting_review", "cancelling", "cancelled", "completed", "failed":
	default:
		return Status{}, domain.NewError("WORKFLOW_STATE_INVALID", 503)
	}
	out.WorkflowID, out.Revision = workflowID, strconv.FormatInt(revision, 10)
	rows, err := tx.Query(ctx, `SELECT a.id,a.sha256 FROM photo.tasks t
	 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
	  AND z.task_id=t.id
	 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
	  AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
	  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
	 WHERE t.tenant_id=$1 AND t.run_id=$2 AND t.state='succeeded'
	  AND a.state='ready' AND a.output_role='document' AND a.verified_at IS NOT NULL
	  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
	  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
	  AND NOT EXISTS(SELECT 1 FROM photo.task_edges e WHERE e.tenant_id=t.tenant_id
	   AND e.run_id=t.run_id AND e.prerequisite_id=t.id)
	 ORDER BY t.node_key,a.id LIMIT 129`, tenantID, out.RunID)
	if err != nil {
		return Status{}, err
	}
	for rows.Next() {
		var ref Ref
		if err = rows.Scan(&ref.ArtifactID, &ref.SHA256); err != nil {
			rows.Close()
			return Status{}, err
		}
		out.Outputs = append(out.Outputs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Status{}, err
	}
	if len(out.Outputs) > 128 {
		return Status{}, domain.NewError("WORKFLOW_OUTPUT_LIMIT", 503)
	}
	if out.State == "failed" {
		var reason string
		err = tx.QueryRow(ctx, `SELECT terminal_reason FROM photo.tasks
		 WHERE tenant_id=$1 AND run_id=$2 AND state='failed' AND terminal_reason IS NOT NULL
		 ORDER BY node_key,id LIMIT 1`, tenantID, out.RunID).Scan(&reason)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Status{}, err
		}
		if err == nil && len(reason) > 0 && len(reason) <= 80 {
			out.ErrorCode = &reason
		}
	}
	return out, nil
}

func (s Service) Get(ctx context.Context, who auth.Identity, tenantID,
	workflowID uuid.UUID) (Status, error) {
	if s.DB == nil {
		return Status{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if workflowID == uuid.Nil {
		return Status{}, domain.NewError("NOT_FOUND", 404)
	}
	var out Status
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
		func(tx pgx.Tx, role domain.Role) error {
			if _, e := s.authorized(ctx, tx, who, tenantID, workflowID,
				role, domain.Viewer); e != nil {
				return e
			}
			var e error
			out, e = readStatus(ctx, tx, tenantID, workflowID, false)
			if e!=nil{return e}
			_,e=s.authorized(ctx,tx,who,tenantID,workflowID,role,domain.Viewer)
			return e
		})
	return out, unavailable(err)
}

func (s Service) Cancel(ctx context.Context, who auth.Identity, tenantID,
	workflowID uuid.UUID, key string, req CancelRequest) (idempotency.Response, error) {
	if s.DB == nil {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if workflowID == uuid.Nil {
		return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
	}
	if !idempotency.ValidKey(key) {
		return idempotency.Response{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	expected, err := expectedRevision(req.ExpectedRevision)
	if err != nil {
		return idempotency.Response{}, err
	}
	var out idempotency.Response
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			if _, e := s.authorized(ctx, tx, who, tenantID, workflowID,
				role, domain.Editor); e != nil {
				return e
			}
			var e error
			out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID,
				Principal: who.UserID, PrincipalType: "user",
				Route: "Workflow.Cancel." + workflowID.String(), Key: key},
				req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
					current, e := readStatus(ctx, tx, tenantID, workflowID, true)
					if e != nil {
						return idempotency.Response{}, e
					}
					if _,e=s.authorized(ctx,tx,who,tenantID,workflowID,role,domain.Editor);e!=nil{return idempotency.Response{},e}
					if current.Revision != strconv.FormatInt(expected, 10) {
						return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
					}
					if current.State == "queued" || current.State == "running" ||
						current.State == "waiting_review" {
						_, e = tx.Exec(ctx, `UPDATE photo.runs SET cancel_requested=true,
						 status='cancelling',revision=revision+1 WHERE tenant_id=$1 AND id=$2
						 AND revision=$3`, tenantID, current.RunID, expected)
						if e != nil {
							return idempotency.Response{}, e
						}
						_, e = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,
						 actor_id,action,resource_type,resource_id,request_id,details)
						 VALUES($1,'user',$2,'workflow.cancel_requested','workflow',$3,$4,
						 jsonb_build_object('run_id',$5::uuid))`, tenantID, who.UserID,
							workflowID, uuid.New(), current.RunID)
						if e != nil {
							return idempotency.Response{}, e
						}
						current, e = readStatus(ctx, tx, tenantID, workflowID, false)
						if e != nil {
							return idempotency.Response{}, e
						}
					}
					body, e := json.Marshal(current)
					return idempotency.Response{Status: 200, Body: body,
						ResourceID: &workflowID}, e
				})
			if e!=nil{return e}
			if e=validateCancelReceipt(out,workflowID);e!=nil{return e}
			_,e=s.authorized(ctx,tx,who,tenantID,workflowID,role,domain.Editor)
			return e
		})
	return out, unavailable(err)
}
