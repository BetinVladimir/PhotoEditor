package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"testing"
)

type noAnalysisDraftDB struct{ calls int }

func (db *noAnalysisDraftDB) Begin(context.Context) (pgx.Tx, error) {
	db.calls++
	panic("unexpected database access")
}
func TestAnalysisDraftRefusesChangedIdentityBeforeDatabase(t *testing.T) {
	request := analysisPlanFixture(t)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	db := &noAnalysisDraftDB{}
	for _, who := range []auth.Identity{{}, {UserID: uuid.New(), SessionID: request.Session}, {UserID: request.Actor, SessionID: uuid.New()}} {
		if err := SaveRelightAnalysisDraft(context.Background(), db, who, request.Tenant, plan); err == nil {
			t.Fatal("identity mismatch accepted")
		}
	}
	if db.calls != 0 {
		t.Fatal("database accessed")
	}
	if err := SaveRelightAnalysisDraft(context.Background(), db, auth.Identity{UserID: request.Actor, SessionID: request.Session}, uuid.New(), plan); err == nil {
		t.Fatal("wrong tenant")
	}
}
