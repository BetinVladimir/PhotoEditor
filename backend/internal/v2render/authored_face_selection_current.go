package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Unexported fields prevent a historical API response from constructing an
// execution selection. Every read/recheck reacquires current SQL authority.
type CurrentAuthoredFaceSelection struct {
	candidate AuthoredFaceSelectionCandidate
	selected  []uuid.UUID
	sha       string
	proof     string
}

func (p CurrentAuthoredFaceSelection) SelectedObjects() []uuid.UUID {
	return append([]uuid.UUID{}, p.selected...)
}
func ReadCurrentAuthoredFaceSelection(ctx context.Context, db *pgxpool.Pool, who auth.Identity, source AuthoredInstructionSource, sha string) (CurrentAuthoredFaceSelection, error) {
	var zero CurrentAuthoredFaceSelection
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || !authoredFinalDigest(sha) {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	err := database.TenantTransaction(ctx, db, source.Tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, "SELECT photo.read_authored_render_selected_faces_current($1,$2,$3,$4)", source.Tenant, source.Parent, source.Instruction.Ordinal, sha).Scan(&raw)
	})
	if err != nil {
		return zero, err
	}
	if len(raw) == 0 {
		return zero, v2input.ErrExpired
	}
	var body struct {
		SHA       string                         `json:"candidate_sha256"`
		Candidate AuthoredFaceSelectionCandidate `json:"candidate"`
		Selected  []uuid.UUID                    `json:"selected_objects"`
		Actor     uuid.UUID                      `json:"actor"`
		Session   uuid.UUID                      `json:"session"`
	}
	if json.Unmarshal(raw, &body) != nil || body.SHA != sha || body.Actor != who.UserID || body.Session != who.SessionID || body.Selected == nil || body.Candidate.Source != source.Source || body.Candidate.Tenant != source.Tenant || body.Candidate.Parent != source.Parent || body.Candidate.Instruction != source.Instruction.Identity || body.Candidate.Ordinal != source.Instruction.Ordinal || body.Candidate.ProgramSHA != source.ProgramSHA || body.Candidate.PredecessorSHA != source.PredecessorSHA {
		return zero, v2input.ErrInvalid
	}
	candidateRaw, err := canonical.Marshal(body.Candidate)
	if err != nil || authoredFaceCandidateSHA(candidateRaw) != sha {
		return zero, v2input.ErrInvalid
	}
	allowed := map[uuid.UUID]bool{}
	for _, id := range body.Candidate.Objects {
		allowed[id] = true
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range body.Selected {
		if id == uuid.Nil || !allowed[id] || seen[id] {
			return zero, v2input.ErrInvalid
		}
		seen[id] = true
	}
	canonicalProof, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
	}
	return CurrentAuthoredFaceSelection{body.Candidate, append([]uuid.UUID{}, body.Selected...), sha, string(canonicalProof)}, ctx.Err()
}
func RecheckCurrentAuthoredFaceSelection(ctx context.Context, db *pgxpool.Pool, who auth.Identity, source AuthoredInstructionSource, proof CurrentAuthoredFaceSelection) error {
	if proof.proof == "" {
		return v2input.ErrInvalid
	}
	fresh, err := ReadCurrentAuthoredFaceSelection(ctx, db, who, source, proof.sha)
	if err != nil {
		return err
	}
	if fresh.proof != proof.proof {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
