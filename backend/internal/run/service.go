package run

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type Service struct {
	DB           *pgxpool.Pool
	Cursors      pagination.Codec
	Objects      OutputSigner
	ReadObjects  OutputReader
	OutputKey    []byte
	PublicOrigin string
	Bucket       string
}

type OutputSigner interface {
	SignDownload(context.Context, objectstore.Object, string, time.Duration) (objectstore.ReadURL, error)
}

type Output struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	Role       string    `json:"role"`
	SHA256     string    `json:"sha256"`
	Bytes      int64     `json:"bytes"`
	MediaType  string    `json:"media_type"`
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type Counts struct {
	Total     int64 `json:"total"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
	Skipped   int64 `json:"skipped"`
	Review    int64 `json:"review"`
}

type PresentationStatus struct {
	Total       int64 `json:"total"`
	Pending     int64 `json:"pending"`
	Ready       int64 `json:"ready"`
	Failed      int64 `json:"failed"`
	Cancelled   int64 `json:"cancelled"`
	Unavailable int64 `json:"unavailable"`
}

type Status struct {
	ID                 uuid.UUID           `json:"id"`
	Kind               string              `json:"kind"`
	State              string              `json:"status"`
	Revision           string              `json:"revision"`
	Counts             Counts              `json:"counts"`
	OutputCollectionID *uuid.UUID          `json:"output_collection_id,omitempty"`
	Stage              string              `json:"stage"`
	Progress           float64             `json:"progress"`
	Errors             []string            `json:"errors"`
	Presentation       *PresentationStatus `json:"presentation,omitempty"`
	createdAt          time.Time
}
type RetryRequest struct {
	TaskIDs          []uuid.UUID `json:"task_ids,omitempty"`
	ExpectedRevision int64       `json:"expected_revision"`
}

const statusFields = `r.id,r.kind,r.status,r.revision,r.output_collection_id,r.created_at,
 count(t.id),count(t.id) FILTER(WHERE t.state='succeeded'),
 count(t.id) FILTER(WHERE t.state IN ('failed','cancelled')),
 count(t.id) FILTER(WHERE t.state='skipped'),
 (SELECT count(*) FROM photo.reviews v WHERE v.tenant_id=r.tenant_id AND v.run_id=r.id AND v.state='pending')`

type scanner interface{ Scan(...any) error }

func scanStatus(row scanner) (Status, error) {
	var out Status
	var revision int64
	err := row.Scan(&out.ID, &out.Kind, &out.State, &revision, &out.OutputCollectionID, &out.createdAt,
		&out.Counts.Total, &out.Counts.Succeeded, &out.Counts.Failed, &out.Counts.Skipped, &out.Counts.Review)
	if err != nil {
		return out, err
	}
	out.Revision = strconv.FormatInt(revision, 10)
	out.Stage = out.State
	out.Errors = []string{}
	updateProgress(&out)
	return out, nil
}

func updateProgress(out *Status) {
	out.Progress = 0
	terminal := out.Counts.Succeeded + out.Counts.Failed + out.Counts.Skipped
	if out.Counts.Total == 0 {
		if out.State == "completed" || out.State == "completed_with_errors" || out.State == "failed" || out.State == "cancelled" {
			out.Progress = 1
		}
	} else {
		out.Progress = float64(terminal) / float64(out.Counts.Total)
	}
}

func applyRenderBatchCounts(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, out *Status) error {
	if out.Kind != "bulk" {
		return nil
	}
	var counts Counts
	err := tx.QueryRow(ctx, `SELECT total,succeeded,failed,skipped,review FROM photo.v26_render_batch_counts($1,$2)`, tenantID, out.ID).Scan(&counts.Total, &counts.Succeeded, &counts.Failed, &counts.Skipped, &counts.Review)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	out.Counts = counts
	updateProgress(out)
	var stage *string
	if err := tx.QueryRow(ctx, `SELECT photo.v26_render_batch_stage($1,$2)`, tenantID, out.ID).Scan(&stage); err != nil {
		return err
	}
	if stage != nil {
		out.Stage = *stage
	}
	return nil
}

func readStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, lock bool) (Status, error) {
	var out Status
	if lock {
		var found uuid.UUID
		err := tx.QueryRow(ctx, "SELECT id FROM photo.runs WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenantID, id).Scan(&found)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return out, err
		}
	}
	out, err := scanStatus(tx.QueryRow(ctx, `SELECT `+statusFields+`
	 FROM photo.runs r LEFT JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
	 WHERE r.tenant_id=$1 AND r.id=$2 GROUP BY r.id`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return out, err
	}
	if err := applyRenderBatchCounts(ctx, tx, tenantID, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (s Service) List(ctx context.Context, who auth.Identity, tenantID uuid.UUID, limit int, raw, status string) (pagination.Page[Status], error) {
	out := pagination.Page[Status]{Items: []Status{}}
	allowed := status == "" || status == "draft" || status == "queued" || status == "running" || status == "waiting_review" || status == "cancelling" || status == "cancelled" || status == "completed" || status == "completed_with_errors" || status == "failed"
	if limit < 1 || limit > 100 || !allowed {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	filterHash, err := canonical.Hash(map[string]string{"status": status})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tenantID.String(), Principal: who.UserID.String(), FilterHash: filterHash, Sort: "run:created_at,id"}
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
		position, err := time.Parse(time.RFC3339Nano, cursor.LastPosition)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterID, afterTime = &id, &position
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT `+statusFields+` FROM photo.runs r
		 LEFT JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
		 WHERE r.tenant_id=$1 AND ($2='' OR r.status=$2)
		 AND ($3::uuid IS NULL OR (r.created_at,r.id)>($4::timestamptz,$3::uuid))
		 GROUP BY r.id ORDER BY r.created_at,r.id LIMIT $5`, tenantID, status, afterID, afterTime, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanStatus(rows)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		for i := range out.Items {
			if err := applyRenderBatchCounts(ctx, tx, tenantID, &out.Items[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID, scope.LastPosition = last.ID.String(), last.createdAt.Format(time.RFC3339Nano)
		cursor, err := s.Cursors.Encode(scope)
		if err != nil {
			return out, err
		}
		out.NextCursor = &cursor
	}
	return out, nil
}

func (s Service) Get(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID) (Status, error) {
	var out Status
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		// An admitted semantic Apply has a project even without an output collection.
		// Repeat project authority before exposing child progress or task counts.
		var semanticProject uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.semantic_targets WHERE tenant_id=$1 AND child_run_id=$2 LIMIT 1`, tenantID, id).Scan(&semanticProject)
		if e == nil {
			if _, e = project.Authorize(ctx, tx, tenantID, semanticProject, who.UserID, role, domain.Viewer, false); e != nil {
				return e
			}
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		var err error
		out, err = readStatus(ctx, tx, tenantID, id, false)
		if err != nil || out.Kind != "v2" {
			return err
		}
		var presentation PresentationStatus
		err = tx.QueryRow(ctx, `SELECT * FROM photo.v26_render_presentation_status($1,$2,$3,$4)`, who.UserID, who.SessionID, tenantID, id).Scan(&presentation.Total, &presentation.Pending, &presentation.Ready, &presentation.Failed, &presentation.Cancelled, &presentation.Unavailable)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err == nil {
			out.Presentation = &presentation
		}
		return err
	})
	return out, err
}

type pendingOutput struct {
	Output
	Object    objectstore.Object
	Revocable bool
}

func (s Service) readOutputCandidates(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID) ([]pendingOutput, error) {
	items := []pendingOutput{}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT COALESCE(st.project_id,w.project_id,c.project_id,a.project_id,sc.project_id) FROM photo.runs r LEFT JOIN photo.semantic_targets st ON st.tenant_id=r.tenant_id AND st.child_run_id=r.id AND r.kind='apply' LEFT JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id AND r.kind='v2' LEFT JOIN photo.collections c ON c.tenant_id=r.tenant_id AND c.id=r.output_collection_id LEFT JOIN photo.asset_versions v ON v.tenant_id=r.tenant_id AND v.id=NULLIF(r.config_snapshot->>'asset_version_id','')::uuid LEFT JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id LEFT JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id LEFT JOIN photo.collections sc ON sc.tenant_id=s.tenant_id AND sc.id=s.collection_id WHERE r.tenant_id=$1 AND r.id=$2`, tenantID, id).Scan(&projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil || projectID == uuid.Nil {
			if e == nil {
				e = domain.NewError("NOT_FOUND", 404)
			}
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT a.id,a.output_role,a.sha256,a.bytes,a.media_type,a.bucket,a.object_key,a.object_version,(t.operation IN ('render.compose','optical.negative') AND z.review_required) FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) WHERE t.tenant_id=$1 AND t.run_id=$2 AND t.state='succeeded' AND NOT t.cancel_requested AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.output_role IN('derivative','derivative_manifest','prompt_mask_png','prompt_mask_ref','working_image','generation_report','contact_page','contact_sheet') AND photo.v26_render_preview_output_allowed($3,$4,$1,t.run_id) AND photo.candidate_review_canonical_output_allowed($3,$4,$1,t.id) UNION ALL SELECT artifact_id,output_role,sha256,bytes,media_type,bucket,object_key,object_version,true FROM photo.v26_render_derivative_outputs($3,$4,$1,$2) UNION ALL SELECT artifact_id,output_role,sha256,bytes,media_type,bucket,object_key,object_version,false FROM photo.v30_interchange_export_download($3,$4,$1,$2)`, tenantID, id, who.UserID, who.SessionID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v pendingOutput
			var bucket string
			if e = rows.Scan(&v.ArtifactID, &v.Role, &v.SHA256, &v.Bytes, &v.MediaType, &bucket, &v.Object.Key, &v.Object.VersionID, &v.Revocable); e != nil {
				return e
			}
			if bucket != s.Bucket {
				return errors.New("run output bucket mismatch")
			}
			v.Object.SHA256, v.Object.Bytes, v.Object.ContentType = v.SHA256, v.Bytes, v.MediaType
			items = append(items, v)
		}
		return rows.Err()
	})
	return items, err
}

func (s Service) Outputs(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID) ([]Output, error) {
	if s.Objects == nil || s.Bucket == "" {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	items, err := s.readOutputCandidates(ctx, who, tenantID, id)
	if err != nil {
		return nil, err
	}
	result := make([]Output, 0, len(items))
	for _, item := range items {
		// Bucket is never exposed and must match the configured storage boundary.
		item.Object.SHA256, item.Object.Bytes, item.Object.ContentType = item.SHA256, item.Bytes, item.MediaType
		if item.Revocable {
			if s.ReadObjects == nil || len(s.OutputKey) < 32 || s.PublicOrigin == "" || item.Object.Key == "" {
				return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
			}
			now := time.Now()
			grant := OutputGrant{Version: 1, Tenant: tenantID, Run: id, User: who.UserID,
				Session: who.SessionID, Artifact: item.ArtifactID, Role: item.Role,
				SHA256: item.SHA256, ObjectKeySHA: outputObjectKeySHA(item.Object.Key), ObjectVersion: item.Object.VersionID,
				MediaType: item.MediaType, Bytes: item.Bytes, ExpiresUnix: now.Add(10 * time.Minute).Unix()}
			token, e := SealOutputGrant(s.OutputKey, grant, now)
			if e != nil {
				return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
			}
			item.Output.URL = s.PublicOrigin + "/api/v1/media/run-output?token=" + url.QueryEscape(token) + "&versionId=" + url.QueryEscape(item.Object.VersionID)
			item.Output.ExpiresAt = time.Unix(grant.ExpiresUnix, 0).UTC()
			result = append(result, item.Output)
			continue
		}
		name := item.ArtifactID.String()
		signed, e := s.Objects.SignDownload(ctx, item.Object, name, 10*time.Minute)
		if e != nil {
			return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		item.Output.URL, item.Output.ExpiresAt = signed.URL, signed.ExpiresAt
		result = append(result, item.Output)
	}
	// Signing performs external I/O after the SQL authority snapshot. Re-read the
	// exact authorized output set before returning any of those signed links.
	current, err := s.readOutputCandidates(ctx, who, tenantID, id)
	if err != nil {
		return nil, err
	}
	before := make(map[pendingOutput]int, len(items))
	for _, item := range items {
		before[item]++
	}
	for _, item := range current {
		before[item]--
	}
	for _, count := range before {
		if count != 0 {
			return nil, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) Cancel(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID) (Status, error) {
	var out Status
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		// Batch cancellation takes the tenant lock before parent/child locks.
		// Detect it without locking the parent through the generic run path.
		var renderBatch bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=$1 AND id=$2 AND kind='bulk'
		 AND config_snapshot->'schema_version'='1'::jsonb AND jsonb_typeof(config_snapshot->'parts')='array'
		 AND config_snapshot ?& ARRAY['policy_sha','auth_session_id'])`, tenantID, id).Scan(&renderBatch); err != nil {
			return err
		}
		if renderBatch {
			var accepted bool
			if err := tx.QueryRow(ctx, `SELECT photo.cancel_v26_render_batch($1,$2,$3,$4)`, tenantID, id, who.UserID, who.SessionID).Scan(&accepted); err != nil {
				return err
			}
			if !accepted {
				return domain.NewError("RUN_NOT_CANCELLABLE", 409)
			}
			var err error
			out, err = readStatus(ctx, tx, tenantID, id, false)
			return err
		}
		current, err := readStatus(ctx, tx, tenantID, id, true)
		if err != nil {
			return err
		}
		switch current.State {
		case "cancelled", "cancelling":
			out = current
			return nil
		case "completed", "completed_with_errors", "failed":
			return domain.NewError("RUN_NOT_CANCELLABLE", 409)
		case "draft", "queued", "running", "waiting_review":
		default:
			return domain.NewError("RUN_STATE_INVALID", 409)
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1
		 WHERE tenant_id=$1 AND id=$2`, tenantID, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
		 VALUES($1,'user',$2,'run.cancel_requested','run',$3,$4,'{}')`, tenantID, who.UserID, id, uuid.New()); err != nil {
			return err
		}
		out, err = readStatus(ctx, tx, tenantID, id, false)
		return err
	})
	return out, err
}

func (s Service) Retry(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID, expected int64, req RetryRequest) (Status, error) {
	var out Status
	if expected < 1 || req.ExpectedRevision != expected || len(req.TaskIDs) > 500 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	seen := map[uuid.UUID]bool{}
	for _, task := range req.TaskIDs {
		if task == uuid.Nil || seen[task] {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		seen[task] = true
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		current, e := readStatus(ctx, tx, tenantID, id, true)
		if e != nil {
			return e
		}
		if current.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if current.State != "failed" && current.State != "completed_with_errors" {
			return domain.NewError("RUN_NOT_RETRYABLE", 409)
		}
		rows, e := tx.Query(ctx, `SELECT t.id FROM photo.tasks t JOIN LATERAL (SELECT a.error FROM photo.task_attempts a WHERE a.tenant_id=t.tenant_id AND a.task_id=t.id ORDER BY a.attempt DESC LIMIT 1) last ON true WHERE t.tenant_id=$1 AND t.run_id=$2 AND t.state='failed' AND t.retry_count<3 AND t.canonical_result_id IS NULL AND last.error->'retryable'='true'::jsonb AND (cardinality($3::uuid[])=0 OR t.id=ANY($3::uuid[])) ORDER BY t.id FOR UPDATE OF t`, tenantID, id, req.TaskIDs)
		if e != nil {
			return e
		}
		var seeds []uuid.UUID
		for rows.Next() {
			var task uuid.UUID
			if e = rows.Scan(&task); e != nil {
				rows.Close()
				return e
			}
			seeds = append(seeds, task)
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return e
		}
		rows.Close()
		if len(seeds) == 0 || len(req.TaskIDs) > 0 && len(seeds) != len(req.TaskIDs) {
			return domain.NewError("RUN_NOT_RETRYABLE", 409)
		}
		_, e = tx.Exec(ctx, `WITH RECURSIVE affected(id,seed) AS (SELECT unnest($3::uuid[]),true UNION SELECT e.dependent_id,false FROM photo.task_edges e JOIN affected a ON a.id=e.prerequisite_id WHERE e.tenant_id=$1 AND e.run_id=$2), reset AS (SELECT id,bool_or(seed) seed FROM affected GROUP BY id) UPDATE photo.tasks t SET state=CASE WHEN r.seed AND NOT EXISTS(SELECT 1 FROM photo.task_edges e JOIN photo.tasks p ON p.tenant_id=e.tenant_id AND p.id=e.prerequisite_id WHERE e.tenant_id=t.tenant_id AND e.dependent_id=t.id AND e.condition='success' AND p.state<>'succeeded') THEN 'ready' ELSE 'blocked' END,next_attempt_at=NULL,terminal_reason=NULL,cancel_requested=false,worker_id=NULL,lease_until=NULL FROM reset r WHERE t.tenant_id=$1 AND t.run_id=$2 AND t.id=r.id AND t.state IN ('failed','skipped','cancelled')`, tenantID, id, seeds)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE photo.runs SET status='running',cancel_requested=false,revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tenantID, id)
		if e != nil {
			var guard *pgconn.PgError
			if errors.As(e, &guard) && guard.Code == "23514" && guard.Message == "V26_BATCH_ALREADY_TERMINAL" {
				return domain.NewError("RUN_NOT_RETRYABLE", 409)
			}
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES($1,'user',$2,'run.retry','run',$3,$4,jsonb_build_object('task_ids',$5::uuid[]))`, tenantID, who.UserID, id, uuid.New(), seeds)
		if e != nil {
			return e
		}
		out, e = readStatus(ctx, tx, tenantID, id, false)
		return e
	})
	return out, err
}
