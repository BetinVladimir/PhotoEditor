package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

// Closed statement identity only. Never emit query text, args, server message,
// detail, relation IDs, DSN, paths, or arbitrary error strings.
func phaseForQuery(query string) string {
	for prefix, phase := range map[string]string{
		"SELECT photo.cancel_unsupported_apply_plans()": "CANCEL_UNSUPPORTED",
		"SELECT tenant_id,run_id,source_collection_id":  "CLAIM",
		"SELECT photo.apply_v4_frozen_plan(":            "FROZEN_PLAN",
		"SELECT photo.fail_apply_terminal_stage(":       "TERMINAL_FAILURE",
		"SELECT photo.request_apply_plan_restart(":      "PLAN_RESTART",
	} {
		if strings.HasPrefix(query, prefix) {
			return phase
		}
	}
	return "OTHER_DATABASE_CALL"
}

type diagnosticDB struct {
	*pgxpool.Pool
	phase *string
}

func (d diagnosticDB) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	*d.phase = phaseForQuery(q)
	return d.Pool.QueryRow(ctx, q, args...)
}
func safeFailure(phase string, err error) map[string]string {
	allowed := map[string]bool{"CONFIG": true, "POOL": true, "ROLE": true, "RECOVERY": true, "COORDINATOR": true, "CANCEL_UNSUPPORTED": true, "CLAIM": true, "FROZEN_PLAN": true, "TERMINAL_FAILURE": true, "PLAN_RESTART": true, "OTHER_DATABASE_CALL": true, "COMPONENT": true}
	if !allowed[phase] {
		phase = "COMPONENT"
	}
	out := map[string]string{"status": "OWNED_FIXTURE_REFUSED", "error_class": "error", "phase": phase}
	var p *pgconn.PgError
	if errors.As(err, &p) && len(p.Code) == 5 {
		valid := true
		for _, c := range p.Code {
			if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
				valid = false
			}
		}
		if valid {
			out["sqlstate"] = p.Code
		}
	}
	return out
}
func safeFailureJSON(phase string, err error) []byte {
	b, _ := json.Marshal(safeFailure(phase, err))
	return b
}
