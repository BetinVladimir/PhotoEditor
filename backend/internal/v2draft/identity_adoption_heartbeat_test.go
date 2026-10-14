package v2draft

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type identityHeartbeatDB struct {
	live    bool
	queries int
}

func (db *identityHeartbeatDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	db.queries++
	if query != "SELECT photo.v23_identity_adopter_heartbeat()" {
		return identityHeartbeatRow{err: errors.New("candidate query must not run")}
	}
	return identityHeartbeatRow{live: db.live}
}

type identityHeartbeatRow struct {
	live bool
	err  error
}

func (r identityHeartbeatRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.live
	return nil
}
func TestIdentityQueueRefusesMissingAdopterHeartbeat(t *testing.T) {
	db := &identityHeartbeatDB{}
	_, err := (SQLIdentityAdoptionQueue{DB: db}).Next(context.Background(), uuid.Nil)
	if err != v2input.ErrExpired || db.queries != 1 {
		t.Fatalf("missing heartbeat: %v, %d queries", err, db.queries)
	}
}
