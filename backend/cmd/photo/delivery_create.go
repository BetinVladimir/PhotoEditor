package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2render"
)

// Use an explicitly configured scheduler credential, never the API pool for
// transport registration. No runtime grants or migrations happen at startup.
func buildDeliveryCreate(ctx context.Context, api *pgxpool.Pool, objects *objectstore.Store, bucket string, signing ...delivery.DeliverySigningPolicyChecker) (httpapi.DeliveryCreator, func(), error) {
	noop := func() {}
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	metadataRunner, metadataEnabled, metadataErr := delivery.MetadataSourceFromEnvironment(os.Getenv)
	if metadataErr != nil {
		return nil, noop, errors.New("delivery metadata configuration invalid")
	}
	dsn := os.Getenv("V30_DELIVERY_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		if metadataEnabled {
			return nil, noop, errors.New("delivery metadata requires delivery scheduler")
		}
		return nil, noop, nil
	}
	if api == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, noop, errors.New("delivery dependencies unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("delivery scheduler configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	config.MaxConns = 4
	config.MinConns = 0
	scheduler, err := pgxpool.NewWithConfig(bounded, config)
	if err != nil {
		return nil, noop, errors.New("delivery scheduler unavailable")
	}
	fail := func() (httpapi.DeliveryCreator, func(), error) {
		scheduler.Close()
		return nil, noop, errors.New("delivery installation unavailable")
	}
	for _, check := range []struct {
		pool      *pgxpool.Pool
		role      string
		functions []string
	}{
		{api, "photo_api", []string{
			"photo.preflight_v30_delivery_create(uuid,uuid,uuid,uuid,uuid,uuid[])",
			"photo.v30_delivery_watermark_source(uuid,uuid,uuid,uuid,uuid,uuid)",
			"photo.select_v30_watermark_version(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text)",
			"photo.read_v30_watermark_pin_retry(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text)",
			"photo.pin_v30_watermark_source(uuid,uuid,uuid,uuid,text,text,text,text)",
			"photo.capture_v30_delivery_draft_selection(uuid,uuid,uuid,uuid,uuid)",
			"photo.resolve_v30_delivery_selection(uuid,uuid,uuid,uuid,uuid,jsonb)",
			"photo.reserve_v30_delivery_create_document(uuid,uuid,uuid,text,text,uuid,text,text,text)",
			"photo.complete_v30_delivery_create_document(uuid,uuid,uuid,text,text,uuid,text,text,text,uuid,text)",
			"photo.begin_v30_delivery_create_run_bound(uuid,uuid,uuid,text,text,text,text)",
			"photo.v30_delivery_runtime_ready(text)",
			"photo.materialize_v30_delivery_prepare(uuid,uuid,uuid,text,text,jsonb)",
			"photo.save_v30_delivery_prepare_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb)",
			"photo.activate_v30_delivery_create_bound(uuid,uuid,uuid,text,text,text)",
		}},
		{scheduler, "photo_scheduler", []string{
			"photo.v30_delivery_prepare_transport_ready(uuid,uuid,jsonb,jsonb)",
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
	var metadataPreparer *delivery.PythonMetadataSourcePreparer
	if metadataEnabled {
		if metadataRunner.Environment["S3_BUCKET"] != bucket {
			return fail()
		}
		signatures := []string{
			"photo.v30_delivery_metadata_original(uuid,uuid,uuid,uuid,text,uuid,uuid)",
			"photo.v30_metadata_source_authorized(uuid,uuid,uuid,uuid,jsonb,text,text,jsonb,text,text,text[])",
			"photo.reserve_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text)",
			"photo.complete_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text,uuid,text)",
			"photo.pin_v30_metadata_inventory(uuid,uuid,uuid,uuid,text,text,text,text,text)",
			"photo.read_v30_metadata_source_retry(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text,text[])",
		}
		var installed bool
		err = api.QueryRow(bounded, `SELECT current_user='photo_api' AND coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 AND NOT EXISTS(SELECT FROM pg_roles forbidden WHERE forbidden.rolname IN('photo_scheduler','photo_worker','photo_gateway','photo_portal') AND has_function_privilege(forbidden.oid,p.oid,'EXECUTE')),false)),false)
   FROM unnest($1::text[]) required(signature) LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature) LEFT JOIN pg_roles owner ON owner.oid=p.proowner`, signatures).Scan(&installed)
		if err != nil || !installed {
			return fail()
		}
		metadataPreparer = &metadataRunner
	}
	freezer := v2render.SQLRenderFreezeService{Drafts: v2draft.Service{DB: api, Objects: objects, Bucket: bucket}, Qualification: v2render.SQLRenderQualificationResolver{API: api}}
	publisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: bucket}, Objects: objects}
	service := httpapi.DeliveryCreateService{Service: delivery.Service{DB: api}, Dependencies: delivery.CreateDeliveryDependencies{
		MetadataPreparer: metadataPreparer, Scheduler: scheduler, Bucket: bucket, Objects: objects, Documents: publisher, Freezer: freezer,
		Limits: delivery.PrepareResourceLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 8 << 20},
	}}
	if len(signing) > 1 {
		return fail()
	}
	if len(signing) == 1 {
		service.Dependencies.Signing = signing[0]
	}
	return service, scheduler.Close, nil
}
