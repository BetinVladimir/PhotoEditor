package v2render

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// ResumeEvidence reads an actual canonical adopted result and publishes only
// after the mandatory physical verifier succeeds. A missing canonical result is
// not completion; the caller keeps the consumer BLOCKED and polls normally.
func (s SQLAuthoredNativeReplayService) ResumeEvidence(ctx context.Context, requestRaw []byte) (v2input.Ref, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, false, err
	}
	requestRaw = append([]byte(nil), requestRaw...)
	request, err := v2input.DecodeAuthoredNativeReplayRequest(requestRaw)
	if err != nil {
		return v2input.Ref{}, false, err
	}
	_, _, task, _, err := request.IDs()
	if err != nil {
		return v2input.Ref{}, false, err
	}
	read := func() ([]byte, error) {
		var candidate *string
		err := database.TenantTransaction(ctx, s.Scheduler, request.Consumer.Tenant, func(tx pgx.Tx) error {
			var role string
			if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
				return err
			}
			if role != "photo_scheduler" {
				return v2input.ErrInvalid
			}
			return tx.QueryRow(ctx, `SELECT photo.read_authored_native_replay_observation($1,$2)`, request.Consumer.Tenant, task).Scan(&candidate)
		})
		if err != nil {
			return nil, err
		}
		if candidate == nil {
			return nil, nil
		}
		return []byte(*candidate), nil
	}
	raw, err := read()
	if err != nil {
		return v2input.Ref{}, false, err
	}
	if len(raw) == 0 {
		return v2input.Ref{}, false, nil
	}
	p := AuthoredNativeReplayPublication{Scheduler: s.Scheduler, Objects: s.Objects, Bucket: s.Bucket}
	ref, err := p.Publish(ctx, request.Consumer.Tenant, requestRaw, "evidence", raw)
	if err != nil {
		return v2input.Ref{}, false, err
	}
	after, err := read()
	if err != nil {
		return v2input.Ref{}, false, err
	}
	if !bytes.Equal(raw, after) {
		return v2input.Ref{}, false, v2input.ErrConflict
	}
	if ref.ArtifactID == uuid.Nil {
		return v2input.Ref{}, false, v2input.ErrInvalid
	}
	return ref, true, ctx.Err()
}
