package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type AuthoredFaceSelectionView struct {
	CandidateSHA string                         `json:"candidate_sha256"`
	Candidate    AuthoredFaceSelectionCandidate `json:"candidate"`
	Revision     int                            `json:"revision"`
	Current      bool                           `json:"current"`
	State        string                         `json:"state"`
	Selected     []uuid.UUID                    `json:"selected_objects"`
}
type AuthoredFaceSelectionService struct{ API *pgxpool.Pool }

// Read returns persisted review/recovery state. Accepted historical display
// (Current=false) is never an input-authority proof for another source.
func (s AuthoredFaceSelectionService) Read(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID, ordinal int, sha string) (AuthoredFaceSelectionView, error) {
	empty := AuthoredFaceSelectionView{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.API == nil || tenantID == uuid.Nil || parent == uuid.Nil || ordinal < 1 || ordinal > 4998 || !authoredFinalDigest(sha) {
		return empty, v2input.ErrInvalid
	}
	var body []byte
	err := tenant.WithTenant(ctx, s.API, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.read_authored_face_selection($1,$2,$3,$4,$5,$6)", tenantID, parent, ordinal, sha, who.UserID, who.SessionID).Scan(&body)
	})
	if err != nil {
		return empty, err
	}
	if len(body) == 0 {
		return empty, v2input.ErrExpired
	}
	var view AuthoredFaceSelectionView
	if json.Unmarshal(body, &view) != nil || view.CandidateSHA != sha || view.Revision != 1 || view.Candidate.Tenant != tenantID || view.Candidate.Parent != parent || view.Candidate.Ordinal != ordinal {
		return empty, v2input.ErrInvalid
	}
	candidateBody, err := canonical.Marshal(view.Candidate)
	if err != nil || authoredFaceCandidateSHA(candidateBody) != sha {
		return empty, v2input.ErrInvalid
	}
	switch view.State {
	case "needs_review":
		if !view.Current || view.Selected != nil {
			return empty, v2input.ErrInvalid
		}
	case "stale":
		if view.Current || view.Selected != nil {
			return empty, v2input.ErrInvalid
		}
	case "accepted", "rejected":
		if view.Selected == nil {
			return empty, v2input.ErrInvalid
		}
	default:
		return empty, v2input.ErrInvalid
	}
	return view, ctx.Err()
}

// Decide is a one-time CAS against an immutable candidate revision. A repeat
// key may return only the identical decision; SQL refuses cross-candidate reuse.
func (s AuthoredFaceSelectionService) Decide(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID, ordinal, revision int, sha, key, decision string, selected []uuid.UUID) (AuthoredFaceSelectionView, error) {
	empty := AuthoredFaceSelectionView{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.API == nil || tenantID == uuid.Nil || parent == uuid.Nil || ordinal < 1 || ordinal > 4998 || revision != 1 || !authoredFinalDigest(sha) || !utf8.ValidString(key) || len(key) < 1 || len(key) > 128 || (decision != "accepted" && decision != "rejected") || selected == nil || len(selected) > 64 || (decision == "rejected" && len(selected) != 0) {
		return empty, v2input.ErrInvalid
	}
	owned := append([]uuid.UUID{}, selected...)
	seen := map[uuid.UUID]bool{}
	for _, id := range owned {
		if id == uuid.Nil || seen[id] {
			return empty, v2input.ErrInvalid
		}
		seen[id] = true
	}
	raw, err := canonical.Marshal(owned)
	if err != nil {
		return empty, err
	}
	var outcome string
	err = tenant.WithTenant(ctx, s.API, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.decide_authored_face_selection($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)", tenantID, parent, ordinal, sha, who.UserID, who.SessionID, key, decision, raw).Scan(&outcome)
	})
	if err != nil {
		return empty, err
	}
	switch outcome {
	case "accepted", "rejected":
		if outcome != decision {
			return empty, v2input.ErrConflict
		}
	case "stale":
		return empty, v2input.ErrExpired
	case "conflict":
		return empty, v2input.ErrConflict
	default:
		return empty, v2input.ErrInvalid
	}
	return s.Read(ctx, who, tenantID, parent, ordinal, sha)
}

// Discovery is bounded and contains only persisted candidates still bound to
// this actual parent. It does not derive a parent from an asset or request ID.
type AuthoredFaceSelectionPage struct {
	SchemaVersion int                         `json:"schema_version"`
	Parent        uuid.UUID                   `json:"parent_run_id"`
	Items         []AuthoredFaceSelectionView `json:"items"`
	Next          *int                        `json:"next_after_ordinal"`
}

func (s AuthoredFaceSelectionService) Discover(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID, after, limit int) (AuthoredFaceSelectionPage, error) {
	var zero AuthoredFaceSelectionPage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.API == nil || tenantID == uuid.Nil || parent == uuid.Nil || after < 0 || after > 4998 || limit < 1 || limit > 16 {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	err := tenant.WithTenant(ctx, s.API, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.list_authored_face_selections($1,$2,$3,$4,$5,$6)", tenantID, parent, who.UserID, who.SessionID, after, limit).Scan(&raw)
	})
	if err != nil {
		return zero, err
	}
	if len(raw) == 0 {
		return zero, v2input.ErrExpired
	}
	var page AuthoredFaceSelectionPage
	if json.Unmarshal(raw, &page) != nil || page.SchemaVersion != 1 || page.Parent != parent || page.Items == nil || len(page.Items) > limit {
		return zero, v2input.ErrInvalid
	}
	previous := after
	for _, view := range page.Items {
		body, err := canonical.Marshal(view.Candidate)
		if err != nil || authoredFaceCandidateSHA(body) != view.CandidateSHA || view.Candidate.Tenant != tenantID || view.Candidate.Parent != parent || view.Candidate.Ordinal <= previous || !view.Current || view.Revision != 1 {
			return zero, v2input.ErrInvalid
		}
		if view.State != "needs_review" && view.State != "accepted" && view.State != "rejected" {
			return zero, v2input.ErrInvalid
		}
		previous = view.Candidate.Ordinal
	}
	if page.Next != nil && (len(page.Items) != limit || *page.Next != previous) {
		return zero, v2input.ErrInvalid
	}
	return page, ctx.Err()
}

func authoredFaceCandidateSHA(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
