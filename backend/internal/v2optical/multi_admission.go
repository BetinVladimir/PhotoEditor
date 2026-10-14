package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// AdmitMultiTask revalidates qualified content and exact registered transport.
// Live source/qualification/resource authority is repeated under SQL locks.
// Graph activation and historical queued receipt commit atomically. Commit
// errors are ambiguous: callers must recover the stored receipt before retrying.
func AdmitMultiTask(ctx context.Context, db PerspectiveTransactions, bucket string, who auth.Identity, tenant uuid.UUID, draft MultiDraft, qualified VerifiedMultiQualification, params v2input.Ref, located ...LocatedMultiQualification) error {
	if err := validateMultiLocated(qualified, located); err != nil {
		return err
	}
	if _, err := RegisterMultiInputs(ctx, db, bucket, who, tenant, draft, qualified, params); err != nil {
		return err
	}
	descriptor, err := draft.Descriptor(ctx, qualified, params)
	if err != nil {
		return err
	}

	s, err := draft.Snapshot()
	if err != nil {
		return err
	}
	scope := artifactjson.Scope{Tenant: tenant, Run: s.Run, Owner: s.Task, Purpose: "task_params"}
	repository := multiParamsRepository{db: db, bucket: bucket, tenant: tenant, who: who, plan: draft}
	proof, err := repository.proof(scope, s.ParamsSHA, int64(len(s.ParamsJCS)))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = repository.authorize(ctx, tx, scope); err != nil {
		return err
	}
	var admitted bool
	if err = tx.QueryRow(ctx, "SELECT photo.seal_v29_multi_graph($1,$2,$3,$4,$5::jsonb)", tenant, s.Run, s.Task, string(descriptor), proof).Scan(&admitted); err != nil {
		return err
	}
	if !admitted {
		return v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if len(located) == 1 {
		snapshot, err := draft.Snapshot()
		if err != nil {
			return err
		}
		if err := LockLocatedMultiQualification(ctx, tx, who, tenant, snapshot.Project, located[0]); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
