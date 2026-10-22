package v2region

import (
 "context"
 "github.com/jackc/pgx/v5/pgxpool"
)

type authoredSchemaFunction struct{Role,Signature,Result,Language,BodySHA string;Args []string;Callers []string;Volatility string}
var authoredSchemaFunctions=[]authoredSchemaFunction{
{"photo_api","photo.authored_region_current_request(uuid,uuid,uuid,uuid,bigint,uuid,text,uuid,uuid,text,uuid,uuid)","boolean","sql","ef31738f31e8acdceeaeac5a99d26c488cbe74c540b538149ba56421d327ac89",[]string{"p_tenant","p_asset","p_frozen","p_draft","p_revision","p_recipe","p_recipe_sha","p_version","p_source","p_source_sha","p_actor","p_session"},[]string{"photo_api"},"v"},
{"photo_scheduler","photo.begin_authored_region(uuid,uuid,text,text,text,uuid,uuid)","text","plpgsql","452c0154808cd2aaa8780ff87c12e17d38a4c8b8f65c1aebe9fe265e9d69fda2",[]string{"p_tenant","p_id","p_key","p_raw","p_sha","p_render","p_crop"},[]string{"photo_scheduler"},"v"},
{"photo_api","photo.read_authored_region(uuid,uuid,uuid,uuid,uuid)","TABLE(intent_jcs text, intent_sha text, render_batch_id uuid, crop_run_id uuid, cancel_requested boolean, revision bigint, is_current boolean)","sql","5ee7b4d868faa169d6ebd062e01abf75876ebc6e5f8cc77db2a051064af482d4",[]string{"p_tenant","p_id","p_asset","p_actor","p_session"},[]string{"photo_api","photo_scheduler"},"v"},
{"photo_api","photo.cancel_authored_region(uuid,uuid,uuid,uuid,uuid)","boolean","plpgsql","04edbddb17ba43237b4abdee470210fea847ee4c26518a5efd865485a5fa50f3",[]string{"p_tenant","p_id","p_asset","p_actor","p_session"},[]string{"photo_api"},"v"},
{"photo_api","photo.authored_region_render_object(uuid,uuid,uuid,uuid,uuid,text)","TABLE(project_id uuid, artifact_id uuid, sha256 text, bucket text, object_key text, object_version text, bytes bigint, media_type text)","sql","f7445efb0019570355b83201c65c59d00a2b776d85e093dbac6b8a8afbc44485",[]string{"p_tenant","p_request","p_actor","p_session","p_artifact","p_sha"},[]string{"photo_api"},"v"},
{"photo_api","photo.authored_region_crop_media(uuid,uuid,uuid,uuid)","TABLE(run_id uuid, task_id uuid, result_id uuid, attempt integer, fence bigint, root_id uuid, root_sha text, root_bucket text, root_key text, root_version text, root_bytes bigint, manifest_jcs text, derivative_id uuid, derivative_sha text, derivative_bucket text, derivative_key text, derivative_version text, derivative_bytes bigint, derivative_media text)","sql","71a7ee15e08f4c33c550d45984362154fa6572060e05fdcba397005925323eac",[]string{"p_tenant","p_request","p_actor","p_session"},[]string{"photo_api"},"v"},
{"photo_scheduler","photo.freeze_authored_region_render_snapshot(uuid,uuid,text)","uuid","plpgsql","c0cba3138631d9d9cf382382f5638145ddaa4faa9ecc86bbb33521ffe050cc9b",[]string{"p_tenant","p_request","p_intent_sha"},[]string{"photo_scheduler"},"v"},
{"photo_scheduler","photo.authored_region_crop_admission_scope(uuid,uuid,uuid,jsonb)","boolean","plpgsql","3ec32516ad94361fb74b65b1e45671432fb95e10847384a1ac604767600267c6",[]string{"p_tenant","p_request","p_run","p_node"},[]string{"photo_scheduler"},"v"},
{"photo_scheduler","photo.authored_region_crop_seal_scope(uuid,uuid,uuid,jsonb)","boolean","plpgsql","0d50ad2a28894411b0f058d0b4be9d2b7477c952c35d5ffc12dc465e03794e23",[]string{"p_tenant","p_request","p_run","p_node"},[]string{"photo_scheduler"},"v"},
{"photo_scheduler","photo.authored_region_crop_identity_current(uuid,uuid,text,uuid,uuid,integer,bigint,uuid,text)","boolean","sql","391fdf4e0fbbfe59b4db56b69b9298209235662a3ec25149845df07f1be2864f",[]string{"p_tenant","p_request","p_sha","p_task","p_result","p_attempt","p_fence","p_root","p_root_sha"},[]string{"photo_scheduler"},"v"},
{"photo_scheduler","photo.inspect_authored_region_children(uuid,uuid,text)","TABLE(render_exists boolean, render_terminal boolean, render_canonical boolean, crop_exists boolean, crop_terminal boolean, crop_canonical boolean, writers_closed boolean, quota_settled boolean, crop_task uuid, crop_result uuid, crop_attempt integer, crop_fence bigint, crop_root_id uuid, crop_root_sha text)","plpgsql","2b35f5eb3a8109f8b00783bb317ca82d75db60aed31e0114141857678a41f39e",[]string{"p_tenant","p_request","p_sha"},[]string{"photo_scheduler"},"v"},
{"photo_scheduler","photo.cancel_authored_region_children(uuid,uuid,text)","boolean","plpgsql","7befffd0b5ab3b1c0a85f1227e46dd23cb56160325083fb95b411815d1b3bc6a",[]string{"p_tenant","p_request","p_sha"},[]string{"photo_scheduler"},"v"},
{"photo_scheduler","photo.claim_authored_region_recovery(uuid)","TABLE(tenant_id uuid, request_id uuid, intent_jcs text, intent_sha text, render_batch_id uuid, crop_run_id uuid, cancel_requested boolean, revision bigint, is_current boolean)","plpgsql","c0b57b50c2f9e4e845ac48cd171f8d060a4ce65a528fd28f6f00ff5037021bff",[]string{"p_token"},[]string{"photo_scheduler"},"v"},
{"photo_scheduler","photo.finish_authored_region_recovery(uuid,uuid,uuid,boolean)","boolean","plpgsql","afb82bdb27f1bac374791f98cd7ddd415e554674e2a5fe3caf35bbfa13297fae",[]string{"p_tenant","p_request","p_token","p_failed"},[]string{"photo_scheduler"},"v"},
}

// RequireAuthoredRegionSchema rejects partial, stale or widened authority before
// API/recovery start. Every listed source body is pinned to the forward install.
func RequireAuthoredRegionSchema(ctx context.Context,api,scheduler *pgxpool.Pool)error{
 for _,check:=range []struct{role string;pool *pgxpool.Pool}{{"photo_api",api},{"photo_scheduler",scheduler}}{
  if err:=requireAuthoredPoolRole(ctx,check.pool,check.role);err!=nil{return err}
  for _,spec:=range authoredSchemaFunctions{if spec.Role!=check.role{continue};var valid bool
   err:=check.pool.QueryRow(ctx,`SELECT p.prokind='f' AND p.prosecdef AND NOT p.proleakproof AND NOT p.proisstrict AND p.proparallel='u' AND p.probin IS NULL AND p.prosqlbody IS NULL
    AND p.provolatile::text=$8 AND p.proretset=($2 LIKE 'TABLE(%')
    AND p.pronargdefaults=0 AND p.proargdefaults IS NULL AND p.provariadic=0
    AND p.pronargs=cardinality($6::text[])
    AND p.proargnames[1:p.pronargs]=$6::text[]
    AND pg_get_function_result(p.oid)=$2 AND l.lanname=$3
    AND pg_get_userbyid(p.proowner)='photo_fn_owner' AND NOT owner.rolcanlogin
    AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
    AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')=$4
    AND has_function_privilege(current_user,p.oid,'EXECUTE')
    AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
      WHERE acl.grantee=0 OR acl.is_grantable OR acl.privilege_type<>'EXECUTE'
       OR (acl.grantee<>p.proowner AND acl.grantee NOT IN(SELECT oid FROM pg_roles WHERE rolname IN('photo_api','photo_scheduler'))))
    AND NOT EXISTS(SELECT 1 FROM pg_roles role WHERE role.rolname IN('photo_api','photo_scheduler','photo_worker','photo_auth','photo_gateway','photo_portal','photo_maintenance')
      AND has_function_privilege(role.oid,p.oid,'EXECUTE') IS DISTINCT FROM (role.rolname=ANY($7::text[])))
    FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang JOIN pg_roles owner ON owner.oid=p.proowner
    WHERE p.oid=to_regprocedure($1) AND current_user=$5`,spec.Signature,spec.Result,spec.Language,spec.BodySHA,spec.Role,spec.Args,spec.Callers,spec.Volatility).Scan(&valid)
   if err!=nil{return err};if !valid{return ErrRegionUnavailable}
  }
 }
 return nil
}
