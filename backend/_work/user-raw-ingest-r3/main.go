package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	retryscope "photoeditor/backend/_work/user-raw-retry-scope-r3"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/workflow"
	"time"
)

type scopeLeases struct {
	workflow.SQLIngestLeases
	allowed map[uuid.UUID]bool
	tenants map[uuid.UUID]uuid.UUID
	pool    *pgxpool.Pool
}

func (s scopeLeases) Claim(c context.Context) (*workflow.IngestBuildLease, error) {
	return retryscope.CheckedClaim(c, func(ctx context.Context) (retryscope.Transaction, error) { return s.pool.Begin(ctx) }, func(t retryscope.Transaction) (*workflow.IngestBuildLease, error) {
		return (workflow.SQLIngestLeases{DB: t.(pgx.Tx)}).Claim(c)
	}, func(l *workflow.IngestBuildLease) bool { return s.allowed[l.RunID] && l.TenantID == s.tenants[l.RunID] })
}

type activationScope struct {
	workflow.IngestActivationSource
	tasks map[uuid.UUID]bool
}

func (s activationScope) Pending(c context.Context, n int) ([]workflow.BoundTaskActivation, error) {
	rows, e := s.IngestActivationSource.Pending(c, n)
	if e != nil {
		return nil, e
	}
	for _, row := range rows {
		if !s.tasks[row.TaskID] {
			return nil, errors.New("unexpected activation scope")
		}
	}
	return rows, nil
}
func activeScopeFromEnvironment() ([]uuid.UUID, map[uuid.UUID]uuid.UUID, error) {
	_, superset, scopeError := retryscope.Scope()
	if scopeError != nil {
		return nil, nil, scopeError
	}
	var ids []uuid.UUID
	if json.Unmarshal([]byte(os.Getenv("EXPECTED_RUN_IDS")), &ids) != nil || len(ids) < 1 || len(ids) > 8 {
		return nil, nil, errors.New("one to eight active ingest runs required")
	}
	var tenants map[uuid.UUID]uuid.UUID
	if json.Unmarshal([]byte(os.Getenv("EXPECTED_RUN_TENANTS")), &tenants) != nil || len(tenants) != len(ids) {
		return nil, nil, errors.New("exact active run tenant bindings required")
	}
	allowed := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || allowed[id] || tenants[id] == uuid.Nil || superset[id] != tenants[id] {
			return nil, nil, errors.New("bad run scope")
		}
		allowed[id] = true
	}
	return ids, tenants, nil
}
func run() error {
	if os.Getenv("CONFIRM_EXACT_TWENTYONE_INGEST") != "reviewed-twenty-one-run-superset" {
		return errors.New("root review required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ids, tenants, e := activeScopeFromEnvironment()
	if e != nil {
		return e
	}
	allowed := map[uuid.UUID]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if e != nil {
		return e
	}
	cfg.MaxConns = 3
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	database.GuardApplicationRole(cfg)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return e
	}
	defer pool.Close()
	if e = database.RequireApplicationRole(ctx, pool, "photo_scheduler"); e != nil {
		return e
	}
	config := objectstore.ConfigFromEnv(true)
	store, e := objectstore.New(config)
	if e != nil {
		return e
	}
	workSHA, e := colorprofile.LoadWorkSHA(os.Getenv("ICC_DIRECTORY"))
	if e != nil {
		return e
	}
	plans := workflow.IngestPlanRepository{DB: pool}
	reader := workflow.IngestRunReader{DB: pool}
	registry, e := workflow.LoadRegistry()
	if e != nil {
		return e
	}
	taskScope := map[uuid.UUID]bool{}
	for _, id := range ids {
		s, e := reader.Load(ctx, tenants[id], id)
		if e != nil {
			return e
		}
		plan, e := workflow.CompileIngest(s.Spec, registry)
		if e != nil {
			return e
		}
		for _, n := range plan.Nodes {
			taskScope[n.ID] = true
		}
	}
	originals := workflow.OriginalRepository{DB: pool, Bucket: config.Bucket}
	source := workflow.IngestCatalogLoader{DB: pool, Objects: store, Bucket: config.Bucket, WorkICCSHA: workSHA}
	runner := workflow.IngestRunner{Runs: reader, Inspection: workflow.IngestInspectionCoordinator{Documents: workflow.IngestDocumentBuilder{Plans: plans, Publisher: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: pool, Bucket: config.Bucket}, Objects: store}}, Tasks: plans}, Decode: workflow.IngestDecodeAdvancer{Documents: workflow.IngestPublishedReader{DB: pool}, Outcomes: workflow.IngestOutcomeReader{DB: pool, Objects: store, Bucket: config.Bucket}, Builder: workflow.IngestDecodeCoordinator{Tasks: plans}}, Catalog: workflow.IngestCatalogRouter{DB: pool, Source: source, WorkICCSHA: workSHA, Legacy: workflow.IngestCatalogCoordinator{Source: source, Originals: workflow.OriginalCoordinator{Reservations: originals, Sources: originals, Objects: store}, Catalog: workflow.IngestCatalogRepository{DB: pool, WorkICCSHA: workSHA}}}}
	processor := workflow.IngestProcessor{Leases: scopeLeases{workflow.SQLIngestLeases{DB: pool}, allowed, tenants, pool}, Runner: runner}
	activator := workflow.IngestActivator{Source: activationScope{workflow.IngestActivationSource{DB: pool}, taskScope}, Tasks: workflow.AdvancementRepository{DB: pool}}
	dispatcher := workflow.Dispatcher{DB: pool}
	total := 0
	for cycle := 0; cycle < 1; cycle++ {
		recovered, e := (workflow.Recovery{DB: pool}).Recover(ctx, 20)
		if e != nil {
			return e
		}
		fmt.Printf("{\"recovered\":%d}\n", recovered)
		skipped, e := plans.SkipFailedInspection(ctx, 20)
		if e != nil {
			return e
		}
		if skipped > 0 {
			fmt.Printf("{\"skipped_failed_inspection\":%d}\n", skipped)
		}
		for range ids {
			_, e = processor.ProcessOne(ctx)
			if e != nil {
				return e
			}
		}
		if _, e = activator.Advance(ctx, 100); e != nil {
			return e
		}
		n, e := dispatcher.Dispatch(ctx, 20)
		if e != nil {
			return e
		}
		total += n
		if n > 0 || cycle%10 == 0 {
			fmt.Printf("{\"cycle\":%d,\"dispatched\":%d}\n", cycle, n)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	fmt.Printf("{\"status\":\"BOUNDED_INGEST_COMPONENT_CYCLES_FINISHED\",\"dispatch_confirmations\":%d,\"completion_requires_independent_postreview\":true}\n", total)
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintf(os.Stderr, "INGEST_FAILURE_CLASS=%T\n", e)
		os.Exit(1)
	}
}
