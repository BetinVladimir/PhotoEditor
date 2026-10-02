package admin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
)

type Service struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
}

type DeadLetter struct {
	ID            uuid.UUID       `json:"id"`
	CreatedAt     time.Time       `json:"created_at"`
	TenantID      *uuid.UUID      `json:"tenant_id,omitempty"`
	TaskID        *uuid.UUID      `json:"task_id,omitempty"`
	EventID       *uuid.UUID      `json:"event_id,omitempty"`
	WorkerClass   string          `json:"worker_class"`
	RoutingKey    string          `json:"routing_key"`
	PayloadSHA256 string          `json:"payload_sha256"`
	Metadata      json.RawMessage `json:"metadata"`
	State         string          `json:"state"`
	SeenCount     int64           `json:"seen_count"`
	LastSeenAt    time.Time       `json:"last_seen_at"`
	ReplayedAt    *time.Time      `json:"replayed_at,omitempty"`
}

type ReplayRequest struct {
	Reason string `json:"reason,omitempty"`
}
type JobAccepted struct {
	JobID  uuid.UUID `json:"job_id"`
	Status string    `json:"status"`
}

func (s Service) authorize(ctx context.Context, who auth.Identity) error {
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return domain.NewError("FORBIDDEN", 403)
	}
	var allowed bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.authorize_system_admin($1,$2)`, who.UserID, who.SessionID).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return domain.NewError("FORBIDDEN", 403)
	}
	return nil
}

func (s Service) ListDeadLetters(ctx context.Context, who auth.Identity, limit int, raw string) (pagination.Page[DeadLetter], error) {
	out := pagination.Page[DeadLetter]{Items: []DeadLetter{}}
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := s.authorize(ctx, who); err != nil {
		return out, err
	}
	scope := pagination.Cursor{Principal: who.UserID.String(), FilterHash: "all", Sort: "dead-letter:created"}
	var afterTime *time.Time
	var afterID *uuid.UUID
	if raw != "" {
		cursor, err := s.Cursors.Decode(raw, scope)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		parsedTime, err := time.Parse(time.RFC3339Nano, cursor.LastPosition)
		id, idErr := uuid.Parse(cursor.LastID)
		if err != nil || idErr != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterTime, afterID = &parsedTime, &id
	}
	rows, err := s.DB.Query(ctx, `SELECT id,created_at,tenant_id,task_id,event_id,worker_class,routing_key,payload_sha256,metadata,state,seen_count,last_seen_at,replayed_at FROM photo.list_dead_letters($1,$2,$3,$4)`, who.UserID, afterTime, afterID, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item DeadLetter
		if err = rows.Scan(&item.ID, &item.CreatedAt, &item.TenantID, &item.TaskID, &item.EventID, &item.WorkerClass, &item.RoutingKey, &item.PayloadSHA256, &item.Metadata, &item.State, &item.SeenCount, &item.LastSeenAt, &item.ReplayedAt); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastPosition, scope.LastID = last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID.String()
		next, encodeErr := s.Cursors.Encode(scope)
		if encodeErr != nil {
			return out, encodeErr
		}
		out.NextCursor = &next
	}
	return out, nil
}

func (s Service) Replay(ctx context.Context, who auth.Identity, id uuid.UUID, key string, request ReplayRequest) (JobAccepted, error) {
	var out JobAccepted
	if id == uuid.Nil || !idempotency.ValidKey(key) || len(request.Reason) > 200 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := s.authorize(ctx, who); err != nil {
		return out, err
	}
	hash, err := canonical.Hash(struct {
		ID      uuid.UUID     `json:"dead_letter_id"`
		Request ReplayRequest `json:"request"`
	}{id, request})
	if err != nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var outcome, status string
	var task *uuid.UUID
	err = s.DB.QueryRow(ctx, `SELECT outcome,task_id,status FROM photo.replay_dead_letter($1,$2,$3,$4)`, who.UserID, id, key, hash).Scan(&outcome, &task, &status)
	if err != nil {
		return out, err
	}
	switch outcome {
	case "accepted", "replayed":
		if task == nil || *task == uuid.Nil || status == "" {
			return out, errors.New("incomplete dead-letter replay")
		}
		return JobAccepted{JobID: *task, Status: status}, nil
	case "conflict":
		return out, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "not_found":
		return out, domain.NewError("NOT_FOUND", 404)
	case "not_replayable":
		return out, domain.NewError("DEAD_LETTER_NOT_REPLAYABLE", 409)
	default:
		return out, domain.NewError("FORBIDDEN", 403)
	}
}
