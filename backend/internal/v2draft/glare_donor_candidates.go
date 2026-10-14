package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type GlareDonorCandidateQuery struct {
	Shoot             uuid.UUID                     `json:"shoot_session_id"`
	Snapshot          uuid.UUID                     `json:"snapshot_id"`
	Base              v2input.SessionVersionBinding `json:"base"`
	SourceObservation uuid.UUID                     `json:"source_observation_id"`
	After             *uuid.UUID                    `json:"after"`
}

type GlareSourceCandidateQuery struct {
	Shoot    uuid.UUID                     `json:"shoot_session_id"`
	Snapshot uuid.UUID                     `json:"snapshot_id"`
	Base     v2input.SessionVersionBinding `json:"base"`
	After    *uuid.UUID                    `json:"after"`
}

type GlareMaskPairQuery struct {
	Snapshot uuid.UUID                     `json:"snapshot_id"`
	Base     v2input.SessionVersionBinding `json:"base"`
	After    *uuid.UUID                    `json:"after"`
}

type GlareMaskPair struct {
	GlassesMask    v2input.Ref `json:"glasses_mask"`
	Protected      v2input.Ref `json:"protected"`
	TargetPixel    v2input.Ref `json:"target_pixel"`
	ProtectedPixel v2input.Ref `json:"protected_pixel"`
}

type GlareMaskPairPage struct {
	Items []GlareMaskPair `json:"items"`
	Next  *uuid.UUID      `json:"next_cursor"`
}

func DecodeGlareMaskPairQuery(raw []byte) (GlareMaskPairQuery, error) {
	var zero GlareMaskPairQuery
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var query GlareMaskPairQuery
	if json.Unmarshal(raw, &query) != nil || query.Snapshot == uuid.Nil ||
		query.Base.AssetVersionID == uuid.Nil || query.Base.Source.ArtifactID == uuid.Nil ||
		query.After != nil && *query.After == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{query.Base.Source.SHA256, query.Base.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	exact, err := canonical.Marshal(query)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	return query, nil
}

func decodeGlareMaskPairPage(raw []byte, query GlareMaskPairQuery) (GlareMaskPairPage, error) {
	var zero GlareMaskPairPage
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var page GlareMaskPairPage
	if json.Unmarshal(normal, &page) != nil || page.Items == nil || len(page.Items) > 100 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(page)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	previous := uuid.Nil
	if query.After != nil {
		previous = *query.After
	}
	for _, item := range page.Items {
		refs := []v2input.Ref{item.GlassesMask, item.Protected, item.TargetPixel, item.ProtectedPixel}
		seen := map[uuid.UUID]bool{}
		for _, ref := range refs {
			if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
				return zero, v2input.ErrInvalid
			}
			if _, err := domain.ParseDigest(ref.SHA256); err != nil {
				return zero, v2input.ErrInvalid
			}
			seen[ref.ArtifactID] = true
		}
		if bytes.Compare(item.GlassesMask.ArtifactID[:], previous[:]) <= 0 {
			return zero, v2input.ErrInvalid
		}
		previous = item.GlassesMask.ArtifactID
	}
	if page.Next != nil && (len(page.Items) != 100 || *page.Next != previous) {
		return zero, v2input.ErrInvalid
	}
	return page, nil
}

type GlareSourceCandidate struct {
	Observation uuid.UUID   `json:"observation_id"`
	Faces       v2input.Ref `json:"source_faces"`
}

type GlareSourceCandidatePage struct {
	Items []GlareSourceCandidate `json:"items"`
	Next  *uuid.UUID             `json:"next_cursor"`
}

func DecodeGlareSourceCandidateQuery(raw []byte) (GlareSourceCandidateQuery, error) {
	var zero GlareSourceCandidateQuery
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var query GlareSourceCandidateQuery
	if json.Unmarshal(raw, &query) != nil || query.Shoot == uuid.Nil ||
		query.Snapshot == uuid.Nil || query.Base.AssetVersionID == uuid.Nil ||
		query.Base.Source.ArtifactID == uuid.Nil ||
		query.After != nil && *query.After == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{query.Base.Source.SHA256, query.Base.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	exact, err := canonical.Marshal(query)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	return query, nil
}

func decodeGlareSourceCandidatePage(raw []byte, query GlareSourceCandidateQuery) (GlareSourceCandidatePage, error) {
	var zero GlareSourceCandidatePage
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var page GlareSourceCandidatePage
	if json.Unmarshal(normal, &page) != nil || page.Items == nil || len(page.Items) > 100 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(page)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	previous := uuid.Nil
	if query.After != nil {
		previous = *query.After
	}
	for _, item := range page.Items {
		if item.Observation == uuid.Nil || item.Faces.ArtifactID == uuid.Nil ||
			bytes.Compare(item.Observation[:], previous[:]) <= 0 {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(item.Faces.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
		previous = item.Observation
	}
	if page.Next != nil && (len(page.Items) != 100 || *page.Next != previous) {
		return zero, v2input.ErrInvalid
	}
	return page, nil
}

type GlareDonorCandidate struct {
	Observation uuid.UUID   `json:"observation_id"`
	Asset       uuid.UUID   `json:"asset_id"`
	Version     uuid.UUID   `json:"asset_version_id"`
	Source      v2input.Ref `json:"source"`
	Faces       v2input.Ref `json:"donor_faces"`
}

type GlareDonorCandidatePage struct {
	SourceFaces v2input.Ref           `json:"source_faces"`
	Items       []GlareDonorCandidate `json:"items"`
	Next        *uuid.UUID            `json:"next_cursor"`
}

func DecodeGlareDonorCandidateQuery(raw []byte) (GlareDonorCandidateQuery, error) {
	var zero GlareDonorCandidateQuery
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var query GlareDonorCandidateQuery
	if json.Unmarshal(raw, &query) != nil || query.Shoot == uuid.Nil ||
		query.Snapshot == uuid.Nil || query.SourceObservation == uuid.Nil ||
		query.Base.AssetVersionID == uuid.Nil || query.Base.Source.ArtifactID == uuid.Nil ||
		query.After != nil && *query.After == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{query.Base.Source.SHA256, query.Base.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	exact, err := canonical.Marshal(query)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	return query, nil
}

func decodeGlareDonorCandidatePage(raw []byte, query GlareDonorCandidateQuery) (GlareDonorCandidatePage, error) {
	var zero GlareDonorCandidatePage
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var page GlareDonorCandidatePage
	if json.Unmarshal(normal, &page) != nil || page.Items == nil || len(page.Items) > 100 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(page)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	if page.SourceFaces.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(page.SourceFaces.SHA256); err != nil {
		return zero, v2input.ErrInvalid
	}
	previous := uuid.Nil
	if query.After != nil {
		previous = *query.After
	}
	for _, item := range page.Items {
		if item.Observation == uuid.Nil || item.Asset == uuid.Nil ||
			item.Version == uuid.Nil || item.Version == query.Base.AssetVersionID ||
			item.Source.ArtifactID == uuid.Nil || item.Faces.ArtifactID == uuid.Nil ||
			bytes.Compare(item.Observation[:], previous[:]) <= 0 {
			return zero, v2input.ErrInvalid
		}
		for _, digest := range []string{item.Source.SHA256, item.Faces.SHA256} {
			if _, err := domain.ParseDigest(digest); err != nil {
				return zero, v2input.ErrInvalid
			}
		}
		previous = item.Observation
	}
	if page.Next != nil && (len(page.Items) != 100 || *page.Next != previous) {
		return zero, v2input.ErrInvalid
	}
	return page, nil
}

func (s *GlareDonorSubmissionService) ListCandidates(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query GlareDonorCandidateQuery) (GlareDonorCandidatePage, error) {
	var zero GlareDonorCandidatePage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(query)
	if err != nil {
		return zero, err
	}
	query, err = DecodeGlareDonorCandidateQuery(body)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.scheduler.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Marshal(query.Base)
	if err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx,
		"SELECT photo.list_v28_glare_donor_candidates($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)",
		tenant, project, query.Shoot, query.Snapshot, who.UserID, who.SessionID,
		string(base), query.SourceObservation, query.After).Scan(&raw); err != nil {
		return zero, err
	}
	page, err := decodeGlareDonorCandidatePage(raw, query)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return page, nil
}

func (s *GlareDonorSubmissionService) ListSourceCandidates(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query GlareSourceCandidateQuery) (GlareSourceCandidatePage, error) {
	var zero GlareSourceCandidatePage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(query)
	if err != nil {
		return zero, err
	}
	query, err = DecodeGlareSourceCandidateQuery(body)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.scheduler.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Marshal(query.Base)
	if err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx,
		"SELECT photo.list_v28_glare_source_candidates($1,$2,$3,$4,$5,$6,$7::jsonb,$8)",
		tenant, project, query.Shoot, query.Snapshot, who.UserID, who.SessionID,
		string(base), query.After).Scan(&raw); err != nil {
		return zero, err
	}
	page, err := decodeGlareSourceCandidatePage(raw, query)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return page, nil
}

func (s *GlareDonorSubmissionService) ListMaskPairs(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query GlareMaskPairQuery) (GlareMaskPairPage, error) {
	var zero GlareMaskPairPage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(query)
	if err != nil {
		return zero, err
	}
	query, err = DecodeGlareMaskPairQuery(body)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.scheduler.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Marshal(query.Base)
	if err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx,
		"SELECT photo.list_v28_glare_mask_pairs($1,$2,$3,$4,$5,$6::jsonb,$7)",
		tenant, project, query.Snapshot, who.UserID, who.SessionID,
		string(base), query.After).Scan(&raw); err != nil {
		return zero, err
	}
	page, err := decodeGlareMaskPairPage(raw, query)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return page, nil
}
