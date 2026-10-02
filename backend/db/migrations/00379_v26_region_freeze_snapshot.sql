-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.freeze_v26_region_snapshot(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_plan jsonb,p_json text,p_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE descriptor jsonb; target uuid; h bytea; existing photo.snapshots%ROWTYPE;
BEGIN
 IF p_run IS NULL OR p_json IS NULL OR octet_length(p_json)>16384 OR p_sha IS NULL
  OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false; END IF;
 descriptor:=p_json::jsonb;
 h:=public.digest(uuid_send(p_run)||convert_to('region.snapshot','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80); h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 target:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF descriptor->>'schema_version' IS DISTINCT FROM '1' OR descriptor->>'snapshot_id' IS DISTINCT FROM target::text
  OR descriptor->'binding' IS DISTINCT FROM p_plan->'frozen_recipe'->'source' THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT photo.v26_region_locked_scope(p_tenant,p_actor,p_session,p_plan) THEN RETURN false; END IF;
 SELECT * INTO existing FROM photo.snapshots WHERE tenant_id=p_tenant AND id=target FOR UPDATE;
 IF existing.id IS NOT NULL THEN
  RETURN existing.digest=p_sha AND photo.v26_region_snapshot_scope(p_tenant,target,p_actor,p_plan)
   AND EXISTS(SELECT 1 FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=target AND payload=descriptor);
 END IF;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE id=p_run) THEN RETURN false; END IF;
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at)
 VALUES(target,p_tenant,'v2','user',p_actor,p_sha,1,clock_timestamp()+interval '15 minutes');
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload)
 VALUES(p_tenant,target,0,(descriptor->'binding'->>'asset_version_id')::uuid,
  (descriptor->'binding'->'source'->>'artifact_id')::uuid,descriptor->'binding'->'source'->>'sha256',descriptor);
 UPDATE photo.snapshots SET expires_at=NULL WHERE tenant_id=p_tenant AND id=target;
 RETURN photo.v26_region_snapshot_scope(p_tenant,target,p_actor,p_plan);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.freeze_v26_region_snapshot(uuid,uuid,uuid,uuid,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.freeze_v26_region_snapshot(uuid,uuid,uuid,uuid,jsonb,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.freeze_v26_region_snapshot(uuid,uuid,uuid,uuid,jsonb,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
