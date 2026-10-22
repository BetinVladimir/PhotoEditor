package semanticreview

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

// CommitExposurePreviewRun admits and activates an independently prepared preview.
// A timeout can leave a committed run; callers must inspect its authoritative
// state, never infer that the transaction rolled back or submit a new identity.
func CommitExposurePreviewRun(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	run uuid.UUID, params ExposurePreviewParams, commit PreviewCommitInputs,
	candidate, intent, transform []byte, docs ExposureSourceDocuments) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || run == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	body, sha, err := FreezeExposurePreviewParams(params)
	if err != nil {
		return uuid.Nil, err
	}
	if !bytes.Equal(body, commit.ParamsJCS) {
		return uuid.Nil, ErrInvalid
	}
	for _, raw := range [][]byte{candidate, intent, transform, docs.Source, docs.Reference, docs.Mask, docs.Protection,
		commit.Node, commit.Transport, commit.Inventory} {
		if len(raw) < 2 || len(raw) > 1048576 {
			return uuid.Nil, ErrInvalid
		}
	}
	if _, _, _, err := ResolveExposurePreviewDocuments(params, candidate, intent, docs); err != nil {
		return uuid.Nil, err
	}
	// Own all arguments before database IO; SQL independently binds raw SHA,
	// producer provenance, transport, current authority, qualification and quota.
	var task uuid.UUID
	err = db.QueryRow(ctx, "SELECT photo.commit_semantic_preapply_preview($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::jsonb,$17::jsonb,$18::jsonb)",
		params.TenantID, params.IntentID, who.UserID, who.SessionID, params.TargetID, run, string(body), sha,
		string(candidate), string(intent), string(docs.Source), string(docs.Reference), string(docs.Mask), string(transform),
		string(docs.Protection), string(commit.Node), string(commit.Transport), string(commit.Inventory)).Scan(&task)
	if err != nil {
		return uuid.Nil, err
	}
	expected := uuid.NewSHA1(run, []byte("semantic.preview_local_exposure:"+params.TargetID.String()))
	if task != expected {
		return uuid.Nil, ErrInvalid
	}
	return task, nil
}
