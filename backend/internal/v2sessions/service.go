package v2sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
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
	DB        *pgxpool.Pool
	Admission AnalysisAdmission
	Cursors   pagination.Codec
	Objects   ObjectReader
	Bucket    string
}
type Page struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}
type ArtifactRef struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}
type CameraOffset struct {
	CameraHash string `json:"camera_hash"`
	Seconds    int    `json:"seconds"`
}
type SessionPolicy struct {
	SchemaVersion      int            `json:"schema_version"`
	TimeGapSeconds     int            `json:"time_gap_seconds"`
	NeighborCount      int            `json:"neighbor_count"`
	ProtectManualPicks bool           `json:"protect_manual_picks"`
	CameraOffsets      []CameraOffset `json:"camera_offsets"`
	Calibration        ArtifactRef    `json:"calibration"`
}
type CapturePolicy struct {
	SchemaVersion      int         `json:"schema_version"`
	Checks             []string    `json:"checks"`
	MaxQueueAgeSeconds int         `json:"max_queue_age_seconds"`
	Calibration        ArtifactRef `json:"calibration"`
}
type CreateRequest struct {
	ProjectID     uuid.UUID       `json:"project_id"`
	CollectionID  uuid.UUID       `json:"collection_id"`
	Name          string          `json:"name"`
	Policy        SessionPolicy   `json:"policy"`
	CapturePolicy json.RawMessage `json:"capture_policy"`
}
type UpdateRequest struct {
	ExpectedRevision string          `json:"expected_revision"`
	Name             string          `json:"name"`
	State            string          `json:"state"`
	Policy           SessionPolicy   `json:"policy"`
	CapturePolicy    json.RawMessage `json:"capture_policy"`
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func refValid(ref ArtifactRef) bool {
	return ref.ArtifactID != uuid.Nil && digestPattern.MatchString(ref.SHA256)
}
func policyValid(p SessionPolicy) bool {
	if p.SchemaVersion != 1 || p.TimeGapSeconds < 30 || p.TimeGapSeconds > 7200 ||
		p.NeighborCount < 1 || p.NeighborCount > 64 || !p.ProtectManualPicks ||
		p.CameraOffsets == nil || len(p.CameraOffsets) > 32 || !refValid(p.Calibration) {
		return false
	}
	seen := map[string]bool{}
	for _, offset := range p.CameraOffsets {
		if !digestPattern.MatchString(offset.CameraHash) || offset.Seconds < -86400 ||
			offset.Seconds > 86400 || seen[offset.CameraHash] {
			return false
		}
		seen[offset.CameraHash] = true
	}
	return true
}
func parseCapture(raw json.RawMessage) (*CapturePolicy, error) {
	if len(raw) == 0 {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var p CapturePolicy
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || p.SchemaVersion != 1 || p.MaxQueueAgeSeconds != 30 ||
		len(p.Checks) < 1 || len(p.Checks) > 6 || !refValid(p.Calibration) {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	allowed := map[string]bool{"focus": true, "eyes": true, "clipping": true, "glare": true, "duplicate": true, "shot_list": true}
	seen := map[string]bool{}
	for _, check := range p.Checks {
		if !allowed[check] || seen[check] {
			return nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		seen[check] = true
	}
	return &p, nil
}
func nameValid(name string) bool {
	return utf8.ValidString(name) && len([]rune(name)) >= 1 && len([]rune(name)) <= 200
}
func revision(raw string) (int64, error) {
	if len(raw) == 0 || len(raw) > 19 || raw[0] == '0' {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	n, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || n < 1 || n > 9007199254740991 {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	return n, nil
}
func sqlError(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.Code {
	case "40001", "23505":
		return domain.NewError("REVISION_CONFLICT", 409)
	case "23514":
		return domain.NewError("SESSION_UNAVAILABLE", 503)
	case "42501":
		return domain.NewError("FORBIDDEN", 403)
	case "P0002":
		return domain.NewError("NOT_FOUND", 404)
	case "22023":
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return err
}

type qualificationLocks struct{ ModelSHA, DatasetSHA string }

func qualifiedLock(ctx context.Context, tx pgx.Tx, tenantID, projectID uuid.UUID) (qualificationLocks, error) {
	var locks qualificationLocks
	err := tx.QueryRow(ctx, `SELECT model_lock_sha,dataset_sha FROM
	 photo.resolve_v2_qualification($1,$2,'session.describe','1')`,
		tenantID, projectID).Scan(&locks.ModelSHA, &locks.DatasetSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return qualificationLocks{}, domain.NewError("SESSION_UNAVAILABLE", 503)
	}
	return locks, err
}
func (s Service) captureReady(ctx context.Context, tx pgx.Tx, tenantID, projectID uuid.UUID, p *CapturePolicy) error {
	if p == nil {
		return nil
	}
	var locks qualificationLocks
	err := tx.QueryRow(ctx, `SELECT model_lock_sha,dataset_sha FROM photo.resolve_v2_qualification($1,$2,'capture.assess','1')`, tenantID, projectID).Scan(&locks.ModelSHA, &locks.DatasetSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewError("CAPTURE_UNAVAILABLE", 503)
	}
	if err != nil {
		return err
	}
	// Metadata alone cannot prove calibration identity or qualified thresholds.
	// Read exact pinned bytes and match the operation's model/dataset locks.
	return s.verifyCalibration(ctx, tx, tenantID, p.Calibration, locks.ModelSHA, locks.DatasetSHA)
}

func (s Service) Create(ctx context.Context, who auth.Identity, tenantID uuid.UUID, key string,
	req CreateRequest) (idempotency.Response, error) {
	var out idempotency.Response
	capture, err := parseCapture(req.CapturePolicy)
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if err != nil || req.ProjectID == uuid.Nil || req.CollectionID == uuid.Nil ||
		!nameValid(req.Name) || !policyValid(req.Policy) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	policyJSON, _ := json.Marshal(req.Policy)
	var captureJSON any
	if capture != nil {
		raw, _ := json.Marshal(capture)
		captureJSON = string(raw)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, e := project.Authorize(ctx, tx, tenantID, req.ProjectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		var useErr error
		out, useErr = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID, Principal: who.UserID,
			PrincipalType: "user", Route: "V22.CreateSession", Key: key}, req, time.Now().Add(24*time.Hour),
			func() (idempotency.Response, error) {
				locks, e := qualifiedLock(ctx, tx, tenantID, req.ProjectID)
				if e != nil {
					return idempotency.Response{}, e
				}
				if e = s.verifyCalibration(ctx, tx, tenantID, req.Policy.Calibration, locks.ModelSHA, locks.DatasetSHA); e != nil {
					return idempotency.Response{}, e
				}
				if e = s.captureReady(ctx, tx, tenantID, req.ProjectID, capture); e != nil {
					return idempotency.Response{}, e
				}
				var raw json.RawMessage
				e = sqlError(tx.QueryRow(ctx, `SELECT photo.create_v22_session($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`,
					tenantID, req.ProjectID, req.CollectionID, req.Name, string(policyJSON), captureJSON,
					locks.ModelSHA, who.UserID, who.SessionID).Scan(&raw))
				if e != nil {
					return idempotency.Response{}, e
				}
				id, e := v2resource.ID(raw)
				if e != nil {
					return idempotency.Response{}, e
				}
				body, e := v2resource.Load(ctx, tx, tenantID, id, "shoot_sessions")
				return idempotency.Response{Status: 201, Body: body, ResourceID: &id}, e
			})
		return useErr
	})
	return out, err
}
func (s Service) Update(ctx context.Context, who auth.Identity, tenantID, sessionID uuid.UUID,
	key string, req UpdateRequest) (idempotency.Response, error) {
	var out idempotency.Response
	capture, err := parseCapture(req.CapturePolicy)
	expected, revErr := revision(req.ExpectedRevision)
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if err != nil || revErr != nil || sessionID == uuid.Nil || !nameValid(req.Name) ||
		!policyValid(req.Policy) || (req.State != "open" && req.State != "closed" && req.State != "archived") {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	policyJSON, _ := json.Marshal(req.Policy)
	var captureJSON any
	if capture != nil {
		raw, _ := json.Marshal(capture)
		captureJSON = string(raw)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.shoot_sessions WHERE tenant_id=$1 AND id=$2`, tenantID, sessionID).Scan(&projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		var useErr error
		out, useErr = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID, Principal: who.UserID,
			PrincipalType: "user", Route: "V22.UpdateSession:" + sessionID.String(), Key: key}, req,
			time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				locks, e := qualifiedLock(ctx, tx, tenantID, projectID)
				if e != nil {
					return idempotency.Response{}, e
				}
				if e = s.verifyCalibration(ctx, tx, tenantID, req.Policy.Calibration, locks.ModelSHA, locks.DatasetSHA); e != nil {
					return idempotency.Response{}, e
				}
				if e = s.captureReady(ctx, tx, tenantID, projectID, capture); e != nil {
					return idempotency.Response{}, e
				}
				var raw json.RawMessage
				e = sqlError(tx.QueryRow(ctx, `SELECT photo.update_v22_session($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,$11)`,
					tenantID, projectID, sessionID, expected, req.Name, req.State, string(policyJSON),
					captureJSON, locks.ModelSHA, who.UserID, who.SessionID).Scan(&raw))
				if e != nil {
					return idempotency.Response{}, e
				}
				body, e := v2resource.Load(ctx, tx, tenantID, sessionID, "shoot_sessions")
				return idempotency.Response{Status: 200, Body: body, ResourceID: &sessionID}, e
			})
		return useErr
	})
	return out, err
}

func (s Service) Get(ctx context.Context, who auth.Identity, tenantID,
	sessionID uuid.UUID) (json.RawMessage, error) {
	if s.DB == nil {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if sessionID == uuid.Nil {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	var out json.RawMessage
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.shoot_sessions WHERE tenant_id=$1 AND id=$2`, tenantID, sessionID).Scan(&projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		out, e = v2resource.Load(ctx, tx, tenantID, sessionID, "shoot_sessions")
		return e
	})
	return out, err
}

func (s Service) List(ctx context.Context, who auth.Identity, tenantID, projectID uuid.UUID,
	limit int, rawCursor string) (Page, error) {
	out := Page{Items: []json.RawMessage{}}
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if projectID == uuid.Nil || limit < 1 || limit > 100 || len(rawCursor) > 2048 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	filterHash, err := canonical.Hash(struct {
		ProjectID uuid.UUID `json:"project_id"`
	}{projectID})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tenantID.String(), Principal: who.UserID.String(),
		FilterHash: filterHash, Sort: "v2:shoot_sessions:created_at,id"}
	lastTime, lastID := time.Time{}, uuid.Nil
	if rawCursor != "" {
		decoded, e := s.Cursors.Decode(rawCursor, scope)
		if e != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		lastTime, e = time.Parse(time.RFC3339Nano, decoded.LastPosition)
		if e != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		lastID, e = uuid.Parse(decoded.LastID)
		if e != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	type position struct {
		id      uuid.UUID
		created time.Time
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		if _, e := project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id,created_at FROM photo.shoot_sessions
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
			body, e := v2resource.Load(ctx, tx, tenantID, p.id, "shoot_sessions")
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

func (s Service) ListAnalyses(ctx context.Context, who auth.Identity, tenantID, sessionID uuid.UUID,
	limit int, rawCursor string) (Page, error) {
	out := Page{Items: []json.RawMessage{}}
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if sessionID == uuid.Nil || limit < 1 || limit > 100 || len(rawCursor) > 2048 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	filterHash, err := canonical.Hash(struct {
		SessionID uuid.UUID `json:"session_id"`
	}{sessionID})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tenantID.String(), Principal: who.UserID.String(),
		FilterHash: filterHash, Sort: "v2:session_analyses:created_at,id"}
	lastTime, lastID := time.Time{}, uuid.Nil
	if rawCursor != "" {
		decoded, e := s.Cursors.Decode(rawCursor, scope)
		if e != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		lastTime, e = time.Parse(time.RFC3339Nano, decoded.LastPosition)
		if e != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		lastID, e = uuid.Parse(decoded.LastID)
		if e != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	type position struct {
		id      uuid.UUID
		created time.Time
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		if e := tx.QueryRow(ctx, `SELECT project_id FROM photo.shoot_sessions WHERE tenant_id=$1 AND id=$2`, tenantID, sessionID).Scan(&projectID); errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		} else if e != nil {
			return e
		}
		if _, e := project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT a.id,a.created_at FROM photo.session_analyses a JOIN photo.artifacts ar
         ON ar.tenant_id=a.tenant_id AND ar.id=a.plan_artifact_id
         WHERE a.tenant_id=$1 AND a.session_id=$2 AND (a.created_at,a.id)>($3,$4)
         AND ar.state='ready' AND ar.verified_at IS NOT NULL
         AND ar.object_version IS NOT NULL AND ar.object_version NOT IN ('','null')
         AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
         ORDER BY a.created_at,a.id LIMIT $5`, tenantID, sessionID, lastTime, lastID, limit+1)
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
			body, e := v2resource.Load(ctx, tx, tenantID, p.id, "session_analyses")
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
