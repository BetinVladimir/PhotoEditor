-- +goose Up
-- One bounded current-authority read for the complete original occurrence set.
-- Avoid N copies of the complete original-scope gate for N source children.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_adopted_multi_original_inventory(p_tenant uuid,p_project uuid,p_task uuid,
 p_actor uuid,p_session uuid,p_operation text,p_root uuid,p_root_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b record;plan jsonb;locations jsonb;expected bigint;actual bigint;root_object jsonb;
BEGIN
 SELECT * INTO b FROM photo.v29_adopted_multi_binding(p_tenant,p_project,p_task,p_actor,p_session,p_operation);
 IF NOT FOUND OR b.artifact_id IS DISTINCT FROM p_root OR b.sha256 IS DISTINCT FROM p_root_sha THEN RETURN NULL;END IF;
 SELECT r.config_snapshot->'v29_multi_plan' INTO plan FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=b.run_id;
 WITH sources AS (
 SELECT (ordinality-1)::integer AS ordinal,(value #>> '{}')::jsonb AS image,
 (b.params_jcs::jsonb)->'frames'->((ordinality-1)::integer)->'source' AS source
 FROM jsonb_array_elements(plan->'sources_jcs') WITH ORDINALITY
 ), edges AS (
 SELECT ordinal,'WorkingImage'::text AS role,source AS ref FROM sources
 UNION ALL SELECT ordinal,'WorkingPixels',image->'pixels' FROM sources
 UNION ALL SELECT ordinal,'WorkingAlpha',image->'alpha' FROM sources WHERE image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb
 UNION ALL SELECT s.ordinal,'InverseField/'||(step.ordinality-1)::text,step.value->'inverse_field'
 FROM sources s CROSS JOIN LATERAL jsonb_array_elements(s.image->'transform_chain'->'steps') WITH ORDINALITY step
 WHERE step.value->>'kind'='dense_warp'
 ), resolved AS (
 SELECT e.ordinal,e.role,e.ref,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM edges e JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(e.ref->>'artifact_id')::uuid AND a.sha256=e.ref->>'sha256'
 JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.task_id=p_task AND i.project_id=p_project
 AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract=CASE e.role WHEN 'WorkingImage' THEN 'WorkingImage' ELSE 'NpyArray' END
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.bytes>0
 AND ((e.role='WorkingImage' AND a.output_role='working_image' AND a.media_type='application/json' AND a.bytes<=1048576)
 OR (e.role='WorkingPixels' AND a.output_role IN('working_rgb','pixels') AND a.media_type IN('application/x-npy','application/octet-stream') AND a.bytes<=2400008192)
 OR (e.role='WorkingAlpha' AND a.output_role IN('working_alpha','alpha') AND a.media_type IN('application/x-npy','application/octet-stream') AND a.bytes<=800008192)
 OR (e.role LIKE 'InverseField/%' AND a.output_role='warp_field' AND a.media_type='application/x-npy' AND a.bytes<=1600008192))
 ORDER BY a.id FOR SHARE OF a
 )
 SELECT (SELECT count(*) FROM edges),count(*),jsonb_agg(jsonb_build_object('ordinal',ordinal,'role',role,
 'ref',ref,'bucket',bucket,'object_key',object_key,'object_version',object_version,'bytes',bytes,'media_type',media_type)
 ORDER BY ordinal,role COLLATE "C") INTO expected,actual,locations FROM resolved;
 IF expected NOT BETWEEN 4 AND 3930 OR actual IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type)
 INTO root_object FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=b.artifact_id AND a.sha256=b.sha256;
 IF root_object IS NULL OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'binding',jsonb_build_object('result_id',b.result_id,
 'result',jsonb_build_object('artifact_id',b.artifact_id,'sha256',b.sha256),'run_id',b.run_id,'workflow_id',b.workflow_id,
 'snapshot_id',b.snapshot_id,'operation',p_operation,'params_jcs',b.params_jcs),'root_object',root_object,'items',locations);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_adopted_multi_original_inventory(uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_adopted_multi_original_inventory(uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_adopted_multi_original_inventory(uuid,uuid,uuid,uuid,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
