-- +goose Up
-- Dedicated frozen admission; uses the shared shell/quota transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_uuid5(p_namespace uuid,p_name text) RETURNS uuid
LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
DECLARE h bytea;
BEGIN
 h:=public.digest(uuid_send(p_namespace)||convert_to(p_name,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 RETURN encode(substring(h FROM 1 FOR 16),'hex')::uuid;
END $$;
ALTER FUNCTION photo.v30_uuid5(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_uuid5(uuid,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;

CREATE FUNCTION photo.v30_delivery_child_request(p_expansion uuid,p_token uuid,p_slot integer) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE payload jsonb;e photo.graph_expansions%ROWTYPE;frozen jsonb;snapshot jsonb;preset jsonb;cfg jsonb;item jsonb;n integer;ordinal integer;
BEGIN
 IF p_slot IS NULL OR p_slot NOT BETWEEN 0 AND 79999 THEN RETURN NULL;END IF;
 payload:=photo.v30_delivery_expansion_plan(p_expansion,p_token);
 IF payload IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 IF p_slot>=e.expected_count OR p_slot>e.cursor::integer THEN RETURN NULL;END IF;
 frozen:=(payload->'proof'->>'frozen_input_jcs')::jsonb;snapshot:=(payload->'proof'->>'snapshot_jcs')::jsonb;
 n:=jsonb_array_length(frozen->'presets');ordinal:=p_slot/n;item:=snapshot->'items'->ordinal;
 SELECT value INTO preset FROM jsonb_array_elements(frozen->'presets') ORDER BY value->>'preset_id' OFFSET (p_slot%n) LIMIT 1;
 SELECT value INTO cfg FROM jsonb_array_elements(payload->'proof'->'configs_jcs') WHERE value->>'preset_id'=preset->>'preset_id';
 RETURN jsonb_build_object('schema_version',1,'parent_run_id',e.run_id,'child_run_id',photo.v30_uuid5(e.run_id,'child:export:'||p_slot::text),
 'slot_index',p_slot,'ordinal',ordinal,'binding',item->'binding','recipe',item->'recipe','snapshot_digest',frozen->>'snapshot_digest','preset',preset,'config_jcs',cfg->>'config_jcs');
END $$;
ALTER FUNCTION photo.v30_delivery_child_request(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_child_request(uuid,uuid,integer) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;

CREATE FUNCTION photo.create_v30_delivery_child_draft(p_expansion uuid,p_token uuid,p_slot integer,p_request text,
 p_export uuid,p_config text,p_task jsonb,p_params text,p_reserved bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;cfg jsonb;config jsonb;params jsonb;expected_params jsonb;operation text;workflow uuid;task_id uuid;child_id uuid;
 e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;x photo.exports%ROWTYPE;
 policy_sha text;config_sha text;params_sha text;tasks jsonb;reservation numeric;
BEGIN
 IF p_export IS NULL OR p_export='00000000-0000-0000-0000-000000000000'::uuid
 OR p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 2097152
 OR p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 4194304
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
 OR p_task IS NULL OR jsonb_typeof(p_task) IS DISTINCT FROM 'object'
 OR p_reserved IS NULL OR p_reserved NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL OR p_request::jsonb IS DISTINCT FROM expected THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=e.tenant_id AND run_id=e.run_id;
 child_id:=(expected->>'child_run_id')::uuid;workflow:=photo.v30_uuid5(child_id,'workflow');
 cfg:=(expected->>'config_jcs')::jsonb;policy_sha:=expected->'preset'->>'preset_sha';
 config:=jsonb_build_object('project_id',w.project_id,'v30_delivery_export',expected,'export_id',p_export);
 IF p_config::jsonb IS DISTINCT FROM config THEN RETURN false;END IF;
 config_sha:=encode(sha256(convert_to(p_config,'UTF8')),'hex');params_sha:=encode(sha256(convert_to(p_params,'UTF8')),'hex');params:=p_params::jsonb;
 IF cfg->'size' IS NOT NULL THEN
  operation:='derivative.resize';expected_params:=(cfg->'size')||jsonb_build_object('image',expected->'binding'->'source');
 ELSIF cfg->'sharpen' IS NOT NULL THEN
  operation:='derivative.sharpen';expected_params:=(cfg->'sharpen')||jsonb_build_object('image',expected->'binding'->'source','max_delta',0.03,'algorithm','luma_unsharp_v1');
 ELSE
  operation:='export.encode';
  IF coalesce(params->>'icc_sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  expected_params:=(cfg-'naming_template'-'watermark'-'sharpen')||jsonb_build_object('image',expected->'binding'->'source','icc_sha256',params->>'icc_sha256');
 END IF;
 task_id:=photo.v30_uuid5(workflow,operation||':0');
 IF params IS DISTINCT FROM expected_params OR p_task->>'id' IS DISTINCT FROM task_id::text
 OR p_task->>'node_key' IS DISTINCT FROM operation||':0' OR p_task->>'operation' IS DISTINCT FROM operation
 OR p_task->>'operation_version' IS DISTINCT FROM '1' OR p_task->>'resource_class' IS DISTINCT FROM 'cpu'
 OR p_task->>'params_sha' IS DISTINCT FROM params_sha
 OR p_task->'execution_profile'->>'backend' IS DISTINCT FROM 'cpu'
 OR p_task->'execution_profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR p_task->'execution_profile'->'resource_estimate'->'vram_bytes' IS DISTINCT FROM '0'::jsonb THEN RETURN false;END IF;
 reservation:=(p_task->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric;
 IF reservation IS NULL OR reservation<1 OR p_reserved<reservation+octet_length(p_config)+octet_length(p_params) THEN RETURN false;END IF;
 SELECT * INTO x FROM photo.exports WHERE tenant_id=e.tenant_id AND asset_version_id=(expected->'binding'->>'asset_version_id')::uuid AND preset_hash=policy_sha FOR UPDATE;
 IF FOUND AND (x.id<>p_export OR x.metadata_policy IS DISTINCT FROM jsonb_build_object('metadata_policy','strip')
 OR (x.task_id IS NOT NULL AND x.task_id<>task_id)) THEN RETURN false;END IF;
 tasks:=jsonb_build_array(p_task);
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=e.tenant_id AND id=child_id) THEN
  RETURN x.id IS NOT DISTINCT FROM p_export AND photo.v2_pending_shell_matches(e.tenant_id,child_id,workflow,w.project_id,w.input_snapshot_id,r.created_by,
   'delivery',policy_sha,config_sha,config,tasks,'[]'::jsonb,p_reserved);
 END IF;
 IF x.id IS NOT NULL THEN RETURN false;END IF; -- cache reuse requires its dedicated authority path
 IF NOT photo.create_v2_draft_shell(e.tenant_id,child_id,workflow,w.project_id,w.input_snapshot_id,r.created_by,
  'delivery',policy_sha,config_sha,config,tasks,'[]'::jsonb,p_reserved) THEN RETURN false;END IF;
 INSERT INTO photo.exports(id,tenant_id,asset_version_id,preset_hash,status,task_id,metadata_policy)
 VALUES(p_export,e.tenant_id,(expected->'binding'->>'asset_version_id')::uuid,policy_sha,'queued',
 CASE WHEN operation='export.encode' THEN task_id ELSE NULL END,jsonb_build_object('metadata_policy','strip'));
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_CHILD_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.create_v30_delivery_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_delivery_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v30_delivery_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
