package semanticreview

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/canonical"
	"reflect"
)

type exposureScopeQuery struct {
	sql          string
	args, values []any
}
type exposureScopeRecorder struct {
	db     ApprovedProtectionQueryer
	checks []exposureScopeQuery
	err    error
}
type exposureScopeRow struct {
	pgx.Row
	recorder *exposureScopeRecorder
	sql      string
	args     []any
}

func (r exposureScopeRow) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	values := make([]any, len(dest))
	for i, item := range dest {
		switch value := item.(type) {
		case *string:
			values[i] = *value
		case *int64:
			values[i] = *value
		case *uuid.UUID:
			values[i] = *value
		case *bool:
			values[i] = *value
		default:
			r.recorder.err = ErrInvalid
			return ErrInvalid
		}
	}
	r.recorder.checks = append(r.recorder.checks, exposureScopeQuery{r.sql, r.args, values})
	return nil
}
func (r *exposureScopeRecorder) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	owned := append([]any(nil), args...)
	for _, arg := range owned {
		switch arg.(type) {
		case string, int64, int, bool, uuid.UUID:
		default:
			r.err = ErrInvalid
		}
	}
	return exposureScopeRow{r.db.QueryRow(ctx, sql, owned...), r, sql, owned}
}

// PreparedExposureSourceScope owns the physically verified batch projection
// and its exact successful API scope queries. It cannot be constructed by API
// JSON. It is not permission to activate tasks or bypass final qualification.
type exposureSourceBinding struct {
	tenant, intent, actor, session, parent uuid.UUID
	requestSHA                             string
}

type PreparedExposureSourceScope struct {
	inputs  []byte
	checks  []exposureScopeQuery
	child   uuid.UUID
	binding exposureSourceBinding
}

// PrepareApprovedExposureSourceScope retains the preflight's exact metadata
// proofs for a final no-object-IO transaction check. Receipt locking must precede
// RecheckInTransaction; tenant/quota locks must precede source locks. Failures
// require rolling that transaction back.
func PrepareApprovedExposureSourceScope(ctx context.Context, db ApprovedProtectionQueryer, objects planObjectStore, bucket string,
	tenant, intent, actor, session, child uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	budgets []ExposureInputBudget, color ExposureColorProfile) (PreparedExposureSourceScope, error) {
	empty := PreparedExposureSourceScope{}
	if db == nil {
		return empty, ErrInvalid
	}
	recorder := &exposureScopeRecorder{db: db}
	inputs, err := ReadApprovedExposureInputBatch(ctx, recorder, objects, bucket, tenant, intent, actor, session, child, prepared, targets, request, budgets, color)
	if err != nil {
		return empty, err
	}
	if recorder.err != nil {
		return empty, recorder.err
	}
	if len(inputs) == 0 || len(recorder.checks) == 0 {
		return empty, ErrInvalid
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		return empty, ErrInvalid
	}
	fingerprint, err := exposureSourceRequestSHA(prepared.Sources.RunID, request)
	if err != nil {
		return empty, err
	}
	return PreparedExposureSourceScope{inputs: raw, checks: recorder.checks, child: child, binding: exposureSourceBinding{tenant, intent, actor, session, prepared.Sources.RunID, fingerprint}}, nil
}

// Inputs returns an independent copy, preserving the immutable scope proof.
func (s PreparedExposureSourceScope) Inputs() ([]ExposureTaskInputs, error) {
	if s.child == uuid.Nil || len(s.inputs) == 0 || len(s.checks) == 0 {
		return nil, ErrInvalid
	}
	var inputs []ExposureTaskInputs
	if json.Unmarshal(s.inputs, &inputs) != nil {
		return nil, ErrInvalid
	}
	return inputs, nil
}

// RecheckInTransaction repeats every recorded authority with original args and
// requires exact row equality. Held source/task/artifact locks last until commit.
// It must run while original targets are proposed, before draft state changes.
func (s PreparedExposureSourceScope) RecheckInTransaction(ctx context.Context, tx pgx.Tx, child uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || child == uuid.Nil || child != s.child || len(s.inputs) == 0 || len(s.checks) == 0 {
		return ErrInvalid
	}
	for _, check := range s.checks {
		if err := ctx.Err(); err != nil {
			return err
		}
		dest := make([]any, len(check.values))
		for i, value := range check.values {
			dest[i] = reflect.New(reflect.TypeOf(value)).Interface()
		}
		if err := tx.QueryRow(ctx, check.sql, check.args...).Scan(dest...); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errors.Join(ErrConflict, err)
			}
			var stale *pgconn.PgError
			if errors.As(err, &stale) && stale.Code == "40001" {
				return errors.Join(ErrConflict, err)
			}
			return err
		}
		for i, value := range check.values {
			if !reflect.DeepEqual(reflect.ValueOf(dest[i]).Elem().Interface(), value) {
				return ErrConflict
			}
		}
	}
	return ctx.Err()
}

func exposureSourceRequestSHA(parent uuid.UUID, request ApplyDecision) (string, error) {
	return canonical.Hash(struct {
		RunID   uuid.UUID     `json:"run_id"`
		Request ApplyDecision `json:"request"`
	}{parent, request})
}
func (s PreparedExposureSourceScope) matches(tenant, intent, actor, session, child uuid.UUID, prepared PreparedReview, request ApplyDecision) bool {
	fingerprint, err := exposureSourceRequestSHA(prepared.Sources.RunID, request)
	return err == nil && tenant != uuid.Nil && intent != uuid.Nil && actor != uuid.Nil && session != uuid.Nil && child != uuid.Nil && child == s.child && s.binding == (exposureSourceBinding{tenant, intent, actor, session, prepared.Sources.RunID, fingerprint})
}
