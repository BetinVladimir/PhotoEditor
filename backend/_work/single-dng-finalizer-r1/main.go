package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
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
	tenant  uuid.UUID
}

func (s scopeLeases) Claim(c context.Context) (*workflow.IngestBuildLease, error) {
	l, e := s.SQLIngestLeases.Claim(c)
	if e != nil || l == nil {
		return l, e
	}
	if !s.allowed[l.RunID] || l.TenantID != s.tenant {
		_, _ = s.SQLIngestLeases.Release(c, *l)
		return nil, errors.New("unexpected ingest scope")
	}
	return l, nil
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var ids []uuid.UUID
	if json.Unmarshal([]byte(os.Getenv("EXPECTED_RUN_IDS")), &ids) != nil || len(ids) != 1 {
		return errors.New("single DNG run scope required")
	}
	tenant, e := uuid.Parse(os.Getenv("EXPECTED_TENANT_ID"))
	if e != nil {
		return e
	}
	if tenant != uuid.MustParse("5dbe7f17-b560-4696-9525-03959e843841") {
		return errors.New("wrong tenant scope")
	}
	if ids[0] != uuid.MustParse("bdcccfd3-75d6-4989-aed7-8ecb284bce1f") {
		return errors.New("wrong DNG scope")
	}
	allowed := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || allowed[id] {
			return errors.New("bad run scope")
		}
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

	for _, id := range ids {
		s, e := reader.Load(ctx, tenant, id)
		if e != nil {
			return e
		}
		plan, e := workflow.CompileIngest(s.Spec, registry)
		if e != nil {
			return e
		}
		_ = plan
		if s.Phase != "decode" && s.Phase != "catalog" {
			return errors.New("DNG not in catalog-eligible phase")
		}
	}
	originals := workflow.OriginalRepository{DB: pool, Bucket: config.Bucket}
	source := workflow.IngestCatalogLoader{DB: pool, Objects: store, Bucket: config.Bucket, WorkICCSHA: workSHA}
	snapshot, e := reader.Load(ctx, tenant, ids[0])
	if e != nil {
		return e
	}
	batch, e := source.Load(ctx, tenant, snapshot.Spec)
	if e != nil {
		return e
	}
	if !batch.Ready || len(batch.Failures) != 0 || len(batch.Images) != 1 {
		return errors.New("DNG canonical outcomes not ready")
	}
	image := batch.Images[0]
	if image.Source.ID != uuid.MustParse("51fe4f06-63be-4433-b21c-bc0b15ba4aba") || image.Source.SHA256 != "bf35a4da121f853cb644253b69515b441748f95fcbd6fdea2de025a76e6c24cd" || image.Outputs.Pixels.SHA256 != "ebd120cc28a855b2bebe827e1d98f5443dabf0cdd0488004f6c57c29fa4d13af" {
		return errors.New("DNG historical source/reference mismatch")
	}
	runner := workflow.IngestRunner{Runs: reader, Inspection: workflow.IngestInspectionCoordinator{Documents: workflow.IngestDocumentBuilder{Plans: plans, Publisher: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: pool, Bucket: config.Bucket}, Objects: store}}, Tasks: plans}, Decode: workflow.IngestDecodeAdvancer{Documents: workflow.IngestPublishedReader{DB: pool}, Outcomes: workflow.IngestOutcomeReader{DB: pool, Objects: store, Bucket: config.Bucket}, Builder: workflow.IngestDecodeCoordinator{Tasks: plans}}, Catalog: workflow.IngestCatalogRouter{DB: pool, Source: source, WorkICCSHA: workSHA, Legacy: workflow.IngestCatalogCoordinator{Source: source, Originals: workflow.OriginalCoordinator{Reservations: originals, Sources: originals, Objects: store}, Catalog: workflow.IngestCatalogRepository{DB: pool, WorkICCSHA: workSHA}}}}
	processor := workflow.IngestProcessor{Leases: scopeLeases{workflow.SQLIngestLeases{DB: pool}, allowed, tenant}, Runner: runner}
	// Exactly one normal leased transition. Re-running requires a fresh root review;
	// no global recovery, task activation or dispatcher is constructed.
	changed, e := processor.ProcessOne(ctx)
	if e != nil {
		return e
	}
	if !changed {
		return errors.New("no confirmed DNG transition")
	}
	fmt.Println("{\"status\":\"SINGLE_DNG_NORMAL_PROCESSOR_TRANSITION\",\"completion_requires_independent_postreview\":true}")

	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
