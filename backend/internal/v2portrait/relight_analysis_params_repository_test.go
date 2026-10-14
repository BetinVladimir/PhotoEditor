package v2portrait

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

func TestAnalysisParamsProofRejectsForeignScopeBeforeDatabase(t *testing.T) {
	request := analysisPlanFixture(t)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	p := plan.Snapshot()
	db := &noAnalysisDraftDB{}
	repository := relightAnalysisParamsRepository{db: db, bucket: "photos", tenant: request.Tenant, who: auth.Identity{UserID: request.Actor, SessionID: request.Session}, plan: plan}
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: p.Run, Owner: p.Task, Purpose: "task_params"}
	if _, err := repository.proof(scope, p.ParamsSHA, int64(len(p.Params))); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"tenant", "run", "owner", "purpose", "donor", "local", "lease", "hash", "size"} {
		changed := scope
		sha := p.ParamsSHA
		size := int64(len(p.Params))
		switch fault {
		case "tenant":
			changed.Tenant = uuid.New()
		case "run":
			changed.Run = uuid.New()
		case "owner":
			changed.Owner = uuid.New()
		case "purpose":
			changed.Purpose = "input_closure_manifest"
		case "donor":
			changed.DonorPortrait = true
		case "local":
			changed.LocalPortrait = true
		case "lease":
			changed.AssemblyLease = uuid.New()
		case "hash":
			sha = "invalid"
		case "size":
			size++
		}
		if _, err := repository.Reserve(context.Background(), changed, sha, size); err == nil {
			t.Fatal("accepted", fault)
		}
	}
	if db.calls != 0 {
		t.Fatal("database accessed")
	}
}

// Beginning a transaction must receive a bounded context without extending a
// shorter caller deadline. No object IO is performed inside this boundary.
type analysisParamsDeadlineDB struct {
	deadline time.Time
	calls    int
}

func (db *analysisParamsDeadlineDB) Begin(ctx context.Context) (pgx.Tx, error) {
	db.calls++
	var ok bool
	db.deadline, ok = ctx.Deadline()
	if !ok {
		panic("unbounded publication transaction")
	}
	return nil, context.DeadlineExceeded
}
func TestAnalysisParamsTransactionDeadlines(t *testing.T) {
	request := analysisPlanFixture(t)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	p := plan.Snapshot()
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: p.Run, Owner: p.Task, Purpose: "task_params"}
	for _, complete := range []bool{false, true} {
		for _, short := range []bool{false, true} {
			db := &analysisParamsDeadlineDB{}
			repository := relightAnalysisParamsRepository{db: db, bucket: "photos", tenant: request.Tenant, who: auth.Identity{UserID: request.Actor, SessionID: request.Session}, plan: plan}
			ctx := context.Background()
			cancel := func() {}
			bound := time.Now().Add(15 * time.Second)
			if short {
				bound = time.Now().Add(time.Second)
				ctx, cancel = context.WithDeadline(ctx, bound)
			}
			if complete {
				ticket := artifactjson.Reservation{ArtifactID: p.ParamsDocument, Token: uuid.New()}
				key, keyErr := artifactjson.DocumentKey(scope, ticket.ArtifactID)
				if keyErr != nil {
					t.Fatal(keyErr)
				}
				_, err = repository.Complete(ctx, scope, ticket, objectstore.Object{Key: key, SHA256: p.ParamsSHA, Bytes: int64(len(p.Params)), ContentType: "application/json", VersionID: "immutable-v1"})
			} else {
				_, err = repository.Reserve(ctx, scope, p.ParamsSHA, int64(len(p.Params)))
			}
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) || db.calls != 1 {
				t.Fatalf("unexpected begin result: %v calls=%d", err, db.calls)
			}
			if short && !db.deadline.Equal(bound) {
				t.Fatal("caller deadline extended")
			}
			if !short && db.deadline.After(bound.Add(time.Second)) {
				t.Fatal("transaction budget exceeded")
			}
		}
	}
}
func TestAnalysisParamsCancelledReserveDoesNotBegin(t *testing.T) {
	request := analysisPlanFixture(t)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	p := plan.Snapshot()
	db := &noAnalysisDraftDB{}
	repository := relightAnalysisParamsRepository{db: db, bucket: "photos", tenant: request.Tenant, who: auth.Identity{UserID: request.Actor, SessionID: request.Session}, plan: plan}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = repository.Reserve(ctx, artifactjson.Scope{Tenant: request.Tenant, Run: p.Run, Owner: p.Task, Purpose: "task_params"}, p.ParamsSHA, int64(len(p.Params)))
	if !errors.Is(err, context.Canceled) || db.calls != 0 {
		t.Fatal("cancelled request accessed database", err)
	}
}
