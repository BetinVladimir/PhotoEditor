package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/interchange"
	"photoeditor/backend/internal/objectstore"
)

// Use an explicitly configured scheduler credential, never the API pool for
// transport registration. No runtime grants or migrations happen at startup.
func buildInterchangeCreate(ctx context.Context, api *pgxpool.Pool, objects *objectstore.Store, bucket string, exportPreparers ...interchange.ExportCreatePreparer) (httpapi.InterchangeCreator, func(), error) {
	return buildInterchangeCreateWithFactory(ctx, api, objects, bucket, nil, exportPreparers...)
}

// The factory receives the actual process pools and object store after role and
// SQL contract validation. It must construct the qualified native preparer;
// request bodies and environment variables cannot select an implementation.
type interchangeExportFactory func(context.Context, *pgxpool.Pool, *pgxpool.Pool, *objectstore.Store, string) (interchange.ExportCreatePreparer, func(), error)

func buildInterchangeCreateWithFactory(ctx context.Context, api *pgxpool.Pool, objects *objectstore.Store, bucket string, factory interchangeExportFactory, exportPreparers ...interchange.ExportCreatePreparer) (httpapi.InterchangeCreator, func(), error) {
	noop := func() {}
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	if (factory != nil && len(exportPreparers) != 0) || len(exportPreparers) > 1 || (len(exportPreparers) == 1 && exportPreparers[0] == nil) {
		return nil, noop, errors.New("interchange export installation invalid")
	}
	var export interchange.ExportCreatePreparer
	if len(exportPreparers) == 1 {
		export = exportPreparers[0]
	}
	dsn := os.Getenv("V30_INTERCHANGE_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		if export != nil || factory != nil {
			return nil, noop, errors.New("interchange export requires scheduler configuration")
		}
		return nil, noop, nil
	}
	if api == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, noop, errors.New("interchange dependencies unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("interchange scheduler configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	config.MaxConns = 4
	config.MinConns = 0
	scheduler, err := pgxpool.NewWithConfig(bounded, config)
	if err != nil {
		return nil, noop, errors.New("interchange scheduler unavailable")
	}
	fail := func() (httpapi.InterchangeCreator, func(), error) {
		scheduler.Close()
		return nil, noop, errors.New("interchange installation unavailable")
	}
	for _, check := range []struct {
		pool      *pgxpool.Pool
		role      string
		functions []string
	}{
		{api, "photo_api", []string{
			"photo.begin_v30_interchange_parse_run(uuid,uuid,uuid,text,text,text,text,text)",
			"photo.reserve_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text)",
			"photo.complete_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text,uuid,text)",
			"photo.attach_v30_interchange_snapshot(uuid,uuid,uuid,text,text,uuid,text)",
			"photo.materialize_v30_interchange_parse(uuid,uuid,uuid,text,text,text,jsonb)",
			"photo.save_v30_interchange_parse_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)",
			"photo.activate_v30_interchange_parse(uuid,uuid,uuid,text,text,text)",
		}},
		{scheduler, "photo_scheduler", []string{
			"photo.reserve_v2_input_document(uuid,uuid,uuid,text,text,bigint,text)",
			"photo.complete_v2_input_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)",
			"photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb)",
			"photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb)",
		}},
	} {
		var role string
		var installed bool
		err = check.pool.QueryRow(bounded, `SELECT current_user,
   (SELECT count(*)=cardinality($1::text[]) AND coalesce(bool_and(p.prosecdef AND owner.rolname='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE') AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')),false)
    FROM unnest($1::text[]) signatures(signature)
    JOIN pg_proc p ON p.oid=to_regprocedure(signatures.signature)
    JOIN pg_roles owner ON owner.oid=p.proowner)`, check.functions).Scan(&role, &installed)
		if err != nil || role != check.role || !installed {
			return fail()
		}
	}
	closeExport := func() {}
	if factory != nil {
		export, closeExport, err = installInterchangeExportFactory(bounded, api, scheduler, objects, bucket, factory)
		if err != nil {
			scheduler.Close()
			return nil, noop, err
		}
	}
	publisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: bucket}, Objects: objects}
	service := httpapi.InterchangeCreateService{Export: export, Service: interchange.Service{DB: api}, Dependencies: interchange.ParseDependencies{
		Scheduler: scheduler, Bucket: bucket, Objects: objects, Documents: publisher,
		Limits: interchange.ParseLimits{RAMBytes: 2 << 30, ScratchBytes: 8 << 30, OutputBytes: 2 << 30, ExpandedBytes: 1 << 30, Files: 30000},
	}}
	return service, func() { closeExport(); scheduler.Close() }, nil
}

func installInterchangeExportFactory(ctx context.Context, api, scheduler *pgxpool.Pool, objects *objectstore.Store, bucket string, factory interchangeExportFactory) (interchange.ExportCreatePreparer, func(), error) {
	noop := func() {}
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	if factory == nil || api == nil || scheduler == nil || api == scheduler || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, noop, errors.New("interchange export dependencies unavailable")
	}
	preparer, close, err := factory(ctx, api, scheduler, objects, bucket)
	if close == nil {
		close = noop
	}
	if err != nil || preparer == nil || ctx.Err() != nil {
		close()
		if ctx.Err() != nil {
			return nil, noop, ctx.Err()
		}
		return nil, noop, errors.New("interchange export installation unavailable")
	}
	// Process shutdown may be requested by more than one owner.
	var once sync.Once
	return preparer, func() { once.Do(close) }, nil
}
