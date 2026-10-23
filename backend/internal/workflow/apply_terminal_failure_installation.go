package workflow

import (
	"context"
	"errors"
	"photoeditor/backend/internal/artifactjson"
	"time"
)

const applyTerminalFailureInstallationSQL = `SELECT current_user='photo_scheduler'
 AND to_regprocedure('photo.fail_apply_terminal_stage(uuid,uuid,uuid)') IS NOT NULL
 AND coalesce(has_function_privilege(to_regprocedure('photo.fail_apply_terminal_stage(uuid,uuid,uuid)'),'EXECUTE'),false)`

// Refuse an old/incomplete schema before any coordinator Claim can mutate/open
// an epoch. No private predicate EXECUTE or table privileges are required.
func requireApplyTerminalFailureInstallation(ctx context.Context, db artifactjson.RowQuerier) error {
	if db == nil {
		return errors.New("scheduler Apply terminal failure dependency unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := bounded.Err(); err != nil {
		return err
	}
	var installed bool
	if err := db.QueryRow(bounded, applyTerminalFailureInstallationSQL).Scan(&installed); err != nil || !installed {
		return errors.New("scheduler Apply terminal failure dependency unavailable")
	}
	return bounded.Err()
}
