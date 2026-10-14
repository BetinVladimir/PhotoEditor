package workflow

import (
 "context"
 "errors"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Check publication and execution together: admission alone cannot establish
// that the installed worker/adoption/readers understand replacement identities.
const multiReplacementInstallationSQL = `
WITH functions(signature,exposed) AS (VALUES
 ('photo.v29_multi_applied_originals_current(uuid,uuid,jsonb)',false),
 ('photo.v29_multi_applied_witnesses_current(uuid,uuid,text,text,jsonb)',false),
 ('photo.apply_v29_multi_retained(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb)',false),
 ('photo.apply_v29_multi_version(uuid,uuid,text,uuid,uuid,uuid,jsonb,text)',false),
 ('photo.v29_accepted_multi_parent(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text)',false),
 ('photo.lock_v29_multi_replacement_identity(uuid,uuid,uuid,uuid,uuid,text,jsonb,text)',true),
 ('photo.lock_v29_multi_replacement_inventory(uuid,uuid,uuid,uuid,uuid,text,jsonb,text)',true),
 ('photo.save_v29_multi_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text)',true),
 ('photo.read_v29_multi_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text)',true),
 ('photo.reserve_v29_multi_replacement_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb)',true),
 ('photo.complete_v29_multi_replacement_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb)',true),
 ('photo.stage_v29_multi_replacement_task(uuid,uuid,text,jsonb)',true),
 ('photo.reserve_v29_multi_replacement_transport_document(uuid,uuid,uuid,text,text,bigint,text,jsonb,text)',true),
 ('photo.complete_v29_multi_replacement_transport_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb,text)',true),
 ('photo.read_v29_multi_replacement_published_transport(uuid,uuid,uuid,text,jsonb)',true),
 ('photo.register_v29_multi_replacement_transport(uuid,uuid,text,jsonb,jsonb,text)',true),
 ('photo.seal_v29_multi_replacement_graph(uuid,uuid,uuid,text,jsonb)',true),
 ('photo.read_v29_multi_replacement_queued_receipt(uuid,uuid,uuid,uuid,text)',true),
 ('photo.probe_v29_multi_replacement_publication(uuid,uuid,uuid,uuid,text)',true),
 ('photo.read_v29_multi_replacement_identity(uuid,uuid,uuid,uuid,uuid)',true),
 ('photo.v29_multi_replacement_inventory_valid(text)',false),
 ('photo.v29_multi_replacement_params_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb)',false),
 ('photo.v29_multi_replacement_transport_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb,text)',false),
 ('photo.v29_multi_execution_plan(uuid,uuid)',false),
 ('photo.v29_multi_execution_session(uuid,uuid,jsonb)',false),
 ('photo.v29_multi_replacement_source(uuid,uuid,uuid,uuid,uuid,jsonb)',false),
 ('photo.v29_multi_execution_source_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb)',false),
 ('photo.v29_multi_adopted_replacement_frame_ready(uuid,uuid,uuid,jsonb,integer,jsonb,text,jsonb)',false)
), routes(signature,needle) AS (VALUES
 ('photo.apply_v29_multi_complete(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb,jsonb)','apply_v29_multi_retained'),
 ('photo.apply_v29_multi_complete(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb,jsonb)','v29_multi_applied_originals_current'),
 ('photo.apply_v29_multi_retained(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb)','v29_multi_applied_witnesses_current'),
 ('photo.apply_v29_multi_retained(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb)','apply_v29_multi_version'),
 ('photo.v29_multi_applied_originals_current(uuid,uuid,jsonb)','v29_adopted_multi_original_inventory'),
 ('photo.v29_multi_applied_witnesses_current(uuid,uuid,text,text,jsonb)','v29_multi_preview_source'),
 ('photo.read_v29_adopted_optical_parent(uuid,uuid,uuid,uuid,uuid,uuid)','v29_accepted_multi_parent'),
 ('photo.v29_worker_multi_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)','v29_multi_execution_session'),
 ('photo.v29_worker_multi_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)','v29_multi_execution_source_scope'),
 ('photo.v29_worker_multi_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)','v29_multi_execution_session'),
 ('photo.v29_multi_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)','v29_multi_execution_session'),
 ('photo.v29_multi_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)','v29_multi_execution_source_scope'),
 ('photo.v29_adopted_multi_originals_ready(uuid,uuid,uuid,uuid,uuid,jsonb)','v29_multi_adopted_replacement_frame_ready'),
 ('photo.v29_adopted_multi_binding(uuid,uuid,uuid,uuid,uuid,text)','v29_multi_execution_plan'),
 ('photo.v29_adopted_multi_binding(uuid,uuid,uuid,uuid,uuid,text)','v29_negative_replacement_queued_receipt'),
 ('photo.v29_multi_preview_source(uuid,uuid,text)','v29_multi_execution_plan'),
 ('photo.v29_adopted_multi_original_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text,integer,uuid,text,text)','v29_multi_execution_plan'),
 ('photo.v29_adopted_multi_original_inventory(uuid,uuid,uuid,uuid,uuid,text,uuid,text)','v29_multi_execution_plan')
)
SELECT current_user='photo_scheduler'
AND (SELECT coalesce(bool_and(coalesce(p.prosecdef=(f.signature<>'photo.v29_multi_replacement_inventory_valid(text)') AND owner.rolname='photo_fn_owner'
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 AND CASE WHEN f.exposed THEN has_function_privilege(current_user,p.oid,'EXECUTE')
 ELSE NOT EXISTS(SELECT FROM unnest(ARRAY['photo_scheduler','photo_worker','photo_api','photo_gateway','photo_portal','photo_auth','photo_publisher','photo_maintenance']) role_name WHERE has_function_privilege(role_name,p.oid,'EXECUTE')) END,false)),false)
 FROM functions f LEFT JOIN pg_proc p ON p.oid=to_regprocedure(f.signature) LEFT JOIN pg_roles owner ON owner.oid=p.proowner)
AND (SELECT coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner'
 AND strpos(pg_get_functiondef(p.oid),route.needle)>0,false)),false)
 FROM routes route LEFT JOIN pg_proc p ON p.oid=to_regprocedure(route.signature) LEFT JOIN pg_roles owner ON owner.oid=p.proowner)`

func verifyMultiReplacementInstallation(ctx context.Context,pool *pgxpool.Pool) error {
 if err:=ctx.Err();err!=nil{return err}
 if pool==nil{return errors.New("multi replacement database unavailable")}
 bounded,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 var installed bool
 if err:=pool.QueryRow(bounded,multiReplacementInstallationSQL).Scan(&installed);err!=nil||!installed{return errors.New("multi replacement SQL installation unavailable")}
 return ctx.Err()
}
