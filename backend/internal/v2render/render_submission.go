package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type RenderBatchSubmissionRequest struct {
	SnapshotID uuid.UUID             `json:"input_snapshot_id"`
	PolicySHA  string                `json:"policy_sha"`
	Selection  []RenderSelectionItem `json:"selection"`
}

// CompileRenderSubmissionIdentity binds only immutable submitted intent, never
// mutable server runtime/provider selection. Ordered frames remain significant.
func CompileRenderSubmissionIdentity(tenant, actor, project uuid.UUID, key string, request RenderBatchSubmissionRequest) (uuid.UUID, string, error) {
	run, err := v2input.RunID(tenant, actor, key)
	if err != nil || project == uuid.Nil || validateRenderSubmissionRequest(request) != nil {
		return uuid.Nil, "", v2input.ErrInvalid
	}
	owned := append([]RenderSelectionItem(nil), request.Selection...)
	digest, err := canonical.Hash(struct {
		Project uuid.UUID                    `json:"project_id"`
		Request RenderBatchSubmissionRequest `json:"request"`
	}{project, RenderBatchSubmissionRequest{request.SnapshotID, request.PolicySHA, owned}})
	if err != nil {
		return uuid.Nil, "", err
	}
	return run, digest, nil
}

type RenderSubmissionState struct {
	Found           bool
	Completed       bool
	State           string
	CancelRequested bool
}

// ProbeSubmission does not read source objects, recipes, profiles or selections.
// Missing scope reveals no parent; an authorized changed/unbound intent conflicts.
func (s SQLRenderDraftStore) ProbeSubmission(ctx context.Context, tenant, batch uuid.UUID, who auth.Identity, requestSHA string) (RenderSubmissionState, error) {
	if err := ctx.Err(); err != nil {
		return RenderSubmissionState{}, err
	}
	if s.DB == nil || tenant == uuid.Nil || batch == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return RenderSubmissionState{}, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(requestSHA); err != nil {
		return RenderSubmissionState{}, v2input.ErrInvalid
	}
	var role string
	if err := s.DB.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return RenderSubmissionState{}, err
	}
	if role != "photo_scheduler" {
		return RenderSubmissionState{}, v2input.ErrInvalid
	}
	var stored *string
	var out RenderSubmissionState
	err := s.DB.QueryRow(ctx, "SELECT request_sha,run_status,cancel_requested FROM photo.v26_render_submission_state($1,$2,$3,$4)", tenant, batch, who.UserID, who.SessionID).Scan(&stored, &out.State, &out.CancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return RenderSubmissionState{}, nil
	}
	if err != nil {
		return RenderSubmissionState{}, err
	}
	if stored == nil || *stored != requestSHA {
		return RenderSubmissionState{}, v2input.ErrConflict
	}
	out.Found = true
	return out, nil
}

// ProbeSubmissionKey checks the shared V2.Admit route before any source reads.
func (s SQLRenderDraftStore) ProbeSubmissionKey(ctx context.Context, tenant, project, batch uuid.UUID, who auth.Identity, key, requestSHA string) (RenderSubmissionState, error) {
	if err := ctx.Err(); err != nil {
		return RenderSubmissionState{}, err
	}
	expected, err := v2input.RunID(tenant, who.UserID, key)
	if s.DB == nil || err != nil || expected != batch || project == uuid.Nil || who.SessionID == uuid.Nil {
		return RenderSubmissionState{}, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(requestSHA); err != nil {
		return RenderSubmissionState{}, v2input.ErrInvalid
	}
	var role string
	if err := s.DB.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return RenderSubmissionState{}, err
	}
	if role != "photo_scheduler" {
		return RenderSubmissionState{}, v2input.ErrInvalid
	}
	var outcome string
	var state *string
	var cancelled bool
	err = s.DB.QueryRow(ctx, "SELECT outcome,run_status,cancel_requested FROM photo.probe_v26_render_submission($1,$2,$3,$4,$5,$6,$7)", tenant, project, who.UserID, who.SessionID, batch, key, requestSHA).Scan(&outcome, &state, &cancelled)
	if err != nil {
		return RenderSubmissionState{}, err
	}
	switch outcome {
	case "missing":
		return RenderSubmissionState{}, nil
	case "conflict":
		return RenderSubmissionState{}, v2input.ErrConflict
	case "expired":
		return RenderSubmissionState{}, v2input.ErrExpired
	case "pending", "replayed":
		if state == nil {
			return RenderSubmissionState{}, v2input.ErrInvalid
		}
		return RenderSubmissionState{Found: true, Completed: outcome == "replayed", State: *state, CancelRequested: cancelled}, nil
	default:
		return RenderSubmissionState{}, v2input.ErrInvalid
	}
}

func validateRenderSubmissionRequest(request RenderBatchSubmissionRequest) error {
	if request.SnapshotID == uuid.Nil || len(request.Selection) < 1 || len(request.Selection) > 5000 {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(request.PolicySHA); err != nil {
		return v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, item := range request.Selection {
		if validateRenderSelectionItem(item) != nil || seen[item.Binding.AssetVersionID] {
			return v2input.ErrInvalid
		}
		seen[item.Binding.AssetVersionID] = true
	}
	return nil
}
