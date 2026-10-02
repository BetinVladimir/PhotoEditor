package v2learning

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/tenant"
)

func learningVersionGet(ctx context.Context, tx pgx.Tx, tenantID,
	versionID uuid.UUID) (LearningVersionView, error) {
	var out LearningVersionView
	var revision, versionNo int64
	err := tx.QueryRow(ctx, `SELECT v.id,v.profile_id,v.version_no,
	 e.revision,e.status,a.id,a.sha256,r.id
	 FROM photo.learning_versions v
	 JOIN photo.version_eligibility e ON e.tenant_id=v.tenant_id
	  AND e.project_id=v.project_id AND e.version_id=v.id
	 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id
	  AND a.id=v.evaluation_artifact_id AND a.state='ready'
	  AND a.output_role='document' AND a.verified_at IS NOT NULL
	  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
	  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
	 LEFT JOIN LATERAL (SELECT x.id FROM photo.learning_safety_reviews x
	  WHERE x.tenant_id=v.tenant_id AND x.project_id=v.project_id
	   AND x.version_id=v.id ORDER BY x.created_at DESC,x.id DESC
	  LIMIT 1) r ON true
	 WHERE v.tenant_id=$1 AND v.id=$2`, tenantID, versionID).Scan(
		&out.ID, &out.ProfileID, &versionNo, &revision, &out.Eligibility,
		&out.Evaluation.ArtifactID, &out.Evaluation.SHA256, &out.ReviewID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if err != nil {
		return out, err
	}
	out.Revision = strconv.FormatInt(revision, 10)
	out.VersionNo = strconv.FormatInt(versionNo, 10)
	return out, nil
}

type EvaluationRef struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}

type LearningVersionView struct {
	ID          uuid.UUID     `json:"id"`
	Revision    string        `json:"revision"`
	ProfileID   uuid.UUID     `json:"profile_id"`
	VersionNo   string        `json:"version_no"`
	Evaluation  EvaluationRef `json:"evaluation"`
	Eligibility string        `json:"eligibility"`
	ReviewID    *uuid.UUID    `json:"review_id"`
}

type LearningVersionPage struct {
	Items      []LearningVersionView `json:"items"`
	NextCursor *string               `json:"next_cursor"`
}

// ListVersions pages immutable versions by creation tuple, while reading the
// mutable eligibility revision and current safety decision in the same TX.
func (s ProfileService) ListVersions(ctx context.Context, who auth.Identity,
	tenantID, profileID uuid.UUID, limit int, rawCursor string) (LearningVersionPage, error) {
	out := LearningVersionPage{Items: []LearningVersionView{}}
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if profileID == uuid.Nil || limit < 1 || limit > 100 || len(rawCursor) > 2048 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	filterSHA, err := canonical.Hash(struct {
		ProfileID uuid.UUID `json:"profile_id"`
	}{profileID})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tenantID.String(),
		Principal: who.UserID.String(), FilterHash: filterSHA,
		Sort: "v23:learning_versions:created_at,id"}
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
	type versionRow struct {
		view    LearningVersionView
		created time.Time
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
		func(tx pgx.Tx, role domain.Role) error {
			if _, e := profileGet(ctx, tx, who, tenantID, profileID, role); e != nil {
				return e
			}
			rows, e := tx.Query(ctx, `SELECT v.id,v.created_at,v.version_no,
			 e.revision,e.status,a.id,a.sha256,r.id
			 FROM photo.learning_versions v
			 LEFT JOIN photo.version_eligibility e ON e.tenant_id=v.tenant_id
			  AND e.project_id=v.project_id AND e.version_id=v.id
			 LEFT JOIN photo.artifacts a ON a.tenant_id=v.tenant_id
			  AND a.id=v.evaluation_artifact_id AND a.state='ready'
			  AND a.output_role='document' AND a.verified_at IS NOT NULL
			  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
			  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
			 LEFT JOIN LATERAL (SELECT x.id FROM photo.learning_safety_reviews x
			  WHERE x.tenant_id=v.tenant_id AND x.project_id=v.project_id
			   AND x.version_id=v.id ORDER BY x.created_at DESC,x.id DESC
			  LIMIT 1) r ON true
			 WHERE v.tenant_id=$1 AND v.profile_id=$2
			  AND (v.created_at,v.id)>($3,$4)
			 ORDER BY v.created_at,v.id LIMIT $5`, tenantID, profileID,
				lastTime, lastID, limit+1)
			if e != nil {
				return e
			}
			positions := make([]versionRow, 0, limit+1)
			for rows.Next() {
				var item versionRow
				var versionNo, revision *int64
				var eligibility *string
				var evaluationID *uuid.UUID
				var evaluationSHA *string
				e = rows.Scan(&item.view.ID, &item.created, &versionNo,
					&revision, &eligibility, &evaluationID, &evaluationSHA,
					&item.view.ReviewID)
				if e != nil {
					rows.Close()
					return e
				}
				if versionNo == nil || revision == nil || eligibility == nil ||
					evaluationID == nil || evaluationSHA == nil {
					rows.Close()
					return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
				}
				item.view.ProfileID = profileID
				item.view.Revision = strconv.FormatInt(*revision, 10)
				item.view.VersionNo = strconv.FormatInt(*versionNo, 10)
				item.view.Eligibility = *eligibility
				item.view.Evaluation = EvaluationRef{
					ArtifactID: *evaluationID, SHA256: *evaluationSHA}
				positions = append(positions, item)
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
			for _, item := range positions {
				out.Items = append(out.Items, item.view)
			}
			if hasNext {
				last := positions[len(positions)-1]
				scope.LastPosition = last.created.UTC().Format(time.RFC3339Nano)
				scope.LastID = last.view.ID.String()
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
	return out, correctionError(err)
}
