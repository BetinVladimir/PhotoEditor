package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"time"
)

type RelightCandidateQuery struct {
	Snapshot uuid.UUID                     `json:"snapshot_id"`
	Base     v2input.SessionVersionBinding `json:"base"`
	Kind     string                        `json:"kind"`
	After    *uuid.UUID                    `json:"after"`
}
type RelightCandidate struct {
	Ref  v2input.Ref `json:"ref"`
	Kind string      `json:"kind"`
}
type RelightCandidatePage struct {
	Items []RelightCandidate `json:"items"`
	Next  *uuid.UUID         `json:"next_cursor"`
}

func (q RelightCandidateQuery) validate() error {
	if q.Snapshot == uuid.Nil || q.Base.AssetVersionID == uuid.Nil || q.Base.Source.ArtifactID == uuid.Nil || q.After != nil && *q.After == uuid.Nil {
		return v2input.ErrInvalid
	}
	if q.Kind != "depth" && q.Kind != "person" && q.Kind != "manual_target" && q.Kind != "manual_guard" {
		return v2input.ErrInvalid
	}
	for _, sha := range []string{q.Base.Source.SHA256, q.Base.CanvasHash} {
		if _, err := domain.ParseDigest(sha); err != nil {
			return err
		}
	}
	return nil
}
func decodeRelightCandidatePage(raw []byte, q RelightCandidateQuery) (RelightCandidatePage, error) {
	var zero RelightCandidatePage
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
	}
	var page RelightCandidatePage
	if json.Unmarshal(normal, &page) != nil || page.Items == nil || len(page.Items) > 100 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(page)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	var previous uuid.UUID
	if q.After != nil {
		previous = *q.After
	}
	for _, item := range page.Items {
		if item.Kind != q.Kind || item.Ref.ArtifactID == uuid.Nil || bytes.Compare(item.Ref.ArtifactID[:], previous[:]) <= 0 {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(item.Ref.SHA256); err != nil {
			return zero, err
		}
		previous = item.Ref.ArtifactID
	}
	if page.Next != nil && (*page.Next == uuid.Nil || q.After != nil && bytes.Compare(page.Next[:], q.After[:]) <= 0 || len(page.Items) > 0 && bytes.Compare(page.Next[:], previous[:]) < 0) {
		return zero, v2input.ErrInvalid
	}
	return page, nil
}

// ListCandidates discovers current canonical producer references only. Results
// are choices, not qualification/byte verification or admission capabilities.
func (s *RelightSubmissionService) ListCandidates(ctx context.Context, who auth.Identity, query RelightCandidateQuery) (RelightCandidatePage, error) {
	var zero RelightCandidatePage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if err := query.validate(); err != nil {
		return zero, err
	}
	base, err := canonical.Marshal(query.Base)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.coordinator.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.Background())
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", s.tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.list_v28_relight_candidates($1,$2,$3,$4,$5,$6::jsonb,$7,$8)", s.tenant, s.project, query.Snapshot, who.UserID, who.SessionID, string(base), query.Kind, query.After).Scan(&raw); err != nil {
		return zero, err
	}
	page, err := decodeRelightCandidatePage(raw, query)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return page, nil
}

func DecodeRelightCandidateQuery(raw []byte) (RelightCandidateQuery, error) {
	var zero RelightCandidateQuery
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var query RelightCandidateQuery
	if json.Unmarshal(normal, &query) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(query)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	if err = query.validate(); err != nil {
		return zero, v2input.ErrInvalid
	}
	return query, nil
}
func (s *RelightProjectServices) ListProjectCandidates(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, query RelightCandidateQuery) (RelightCandidatePage, error) {
	var zero RelightCandidatePage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil {
		return zero, v2input.ErrExpired
	}
	service := s.services[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return zero, v2input.ErrExpired
	}
	return service.ListCandidates(ctx, who, query)
}
