-- +goose Up
-- Pure batch cardinality/identity binding. Live canonical decision, receipt,
-- complete document checks, quota and activation remain separate mandatory gates.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_batch_nodes_bound(p_child uuid,p_decisions jsonb,p_tasks jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE row jsonb;decision jsonb;e jsonb;task uuid;n integer;
BEGIN
 IF p_child IS NULL OR p_child='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(p_decisions) IS DISTINCT FROM 'array' OR jsonb_typeof(p_tasks) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 n:=jsonb_array_length(p_tasks);
 IF n NOT BETWEEN 1 AND 100 OR jsonb_array_length(p_decisions) NOT BETWEEN 1 AND 100
 OR n<>(SELECT count(*) FROM jsonb_array_elements(p_decisions) d WHERE d->>'decision'='approve')
 OR n<>(SELECT count(DISTINCT t->>'target_id') FROM jsonb_array_elements(p_tasks) t)
 OR jsonb_array_length(p_decisions)<>(SELECT count(DISTINCT d->>'target_id') FROM jsonb_array_elements(p_decisions) d)
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(p_decisions) d WHERE d->>'decision' NOT IN('approve','reject')
 OR jsonb_typeof(d->'decision') IS DISTINCT FROM 'string') THEN RETURN false;END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
 IF jsonb_typeof(row) IS DISTINCT FROM 'object'
 OR NOT(row ?& ARRAY['target_id','params_artifact_id','params_sha','params_body','node','transport','inventory','documents'])
 OR (row-ARRAY['target_id','params_artifact_id','params_sha','params_body','node','transport','inventory','documents'])<>'{}'::jsonb
 OR jsonb_typeof(row->'target_id') IS DISTINCT FROM 'string'
 OR coalesce(row->>'target_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR row->>'target_id'='00000000-0000-0000-0000-000000000000'
 OR jsonb_typeof(row->'params_body') IS DISTINCT FROM 'string'
 OR octet_length(row->>'params_body') NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(row->'params_sha') IS DISTINCT FROM 'string'
 OR coalesce(row->>'params_sha','') !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(row->>'params_body','UTF8'),'sha256'),'hex') IS DISTINCT FROM row->>'params_sha'
 OR jsonb_typeof(row->'node') IS DISTINCT FROM 'object'
 OR jsonb_typeof(row->'transport') IS DISTINCT FROM 'object'
 OR jsonb_typeof(row->'inventory') IS DISTINCT FROM 'object'
 OR jsonb_typeof(row->'documents') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 SELECT d INTO decision FROM jsonb_array_elements(p_decisions) d WHERE d->>'target_id'=row->>'target_id' AND d->>'decision'='approve';
 IF NOT FOUND THEN RETURN false;END IF;
 e:=(row->>'params_body')::jsonb;
 task:=photo.semantic_uuid_v5(p_child,'semantic.adopt_shown_exposure:'||(row->>'target_id'));
 IF e->>'target_id' IS DISTINCT FROM row->>'target_id' OR e->>'child_run_id' IS DISTINCT FROM p_child::text
 OR e->'working_image' IS DISTINCT FROM decision->'preview'->'render'
 OR e->'presentation' IS DISTINCT FROM decision->'preview'->'evidence'
 OR jsonb_typeof(e->'working_image') IS DISTINCT FROM 'object' OR jsonb_typeof(e->'presentation') IS DISTINCT FROM 'object'
 OR row->>'params_artifact_id' IS DISTINCT FROM photo.semantic_uuid_v5(task,'Params')::text
 OR row->'node'->>'id' IS DISTINCT FROM task::text
 OR row->'node'->>'params_artifact_id' IS DISTINCT FROM row->>'params_artifact_id'
 OR row->'node'->>'params_sha' IS DISTINCT FROM row->>'params_sha'
 OR photo.semantic_shown_exposure_transport_ready(p_child,e,row->'node',row->'transport',row->'inventory') IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_batch_nodes_bound(uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_batch_nodes_bound(uuid,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
