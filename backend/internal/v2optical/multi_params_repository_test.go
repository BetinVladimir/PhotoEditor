package v2optical

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
)

type multiParamsNoIO struct{}

func (multiParamsNoIO) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected database IO")
}

func TestMultiParamsProofBindsImmutableDraft(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
			qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			capacity, err := MultiResources(operation, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant := uuid.New()
			draft, err := CompileMultiDraft(context.Background(), who, tenant, uuid.New(), uuid.New(), uuid.NewString(), operation, params, originals, qualified, capacity)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := draft.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			repo := multiParamsRepository{db: multiParamsNoIO{}, bucket: "photos", tenant: tenant, who: who, plan: draft}
			scope := artifactjson.Scope{Tenant: tenant, Run: snapshot.Run, Owner: snapshot.Task, Purpose: "task_params"}
			proof, err := repo.proof(scope, snapshot.ParamsSHA, int64(len(snapshot.ParamsJCS)))
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Plan   MultiDraftSnapshot `json:"plan"`
				Params string             `json:"params_jcs"`
			}
			if err := json.Unmarshal([]byte(proof), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Params != snapshot.ParamsJCS || decoded.Plan.Operation != operation || decoded.Plan.ParamsDocument != snapshot.ParamsDocument {
				t.Fatal("proof lost immutable operation or document binding")
			}
			for _, mutate := range []func(*artifactjson.Scope){
				func(s *artifactjson.Scope) { s.Tenant = uuid.New() },
				func(s *artifactjson.Scope) { s.Run = uuid.New() },
				func(s *artifactjson.Scope) { s.Owner = uuid.New() },
				func(s *artifactjson.Scope) { s.Purpose = "result" },
				func(s *artifactjson.Scope) { s.AssemblyLease = uuid.New() },
				func(s *artifactjson.Scope) { s.LocalPortrait = true },
				func(s *artifactjson.Scope) { s.DonorPortrait = true },
			} {
				changed := scope
				mutate(&changed)
				if _, err := repo.proof(changed, snapshot.ParamsSHA, int64(len(snapshot.ParamsJCS))); err == nil {
					t.Fatal("accepted foreign publication scope")
				}
			}
			if _, err := repo.proof(scope, snapshot.ParamsSHA, int64(len(snapshot.ParamsJCS))+1); err == nil {
				t.Fatal("accepted wrong byte count")
			}
			repo.who.SessionID = uuid.New()
			if _, err := repo.proof(scope, snapshot.ParamsSHA, int64(len(snapshot.ParamsJCS))); err == nil {
				t.Fatal("accepted foreign session")
			}
		})
	}
}
