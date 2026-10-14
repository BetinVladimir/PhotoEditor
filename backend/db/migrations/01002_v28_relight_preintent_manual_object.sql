-- +goose Up
-- Original manual dependencies before intent creation; no publication/admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_preintent_manual_object(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,p_selected jsonb,p_selection text,p_mask_body text,p_root_body text,p_kind text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE origin jsonb;doc jsonb;ref jsonb;child jsonb;role text;producer uuid;pixel photo.artifacts%ROWTYPE;object_row photo.artifacts%ROWTYPE;original jsonb;
BEGIN
 IF p_selection IS NULL OR p_selection NOT IN('manual_target','manual_guard') OR p_kind IS NULL OR p_kind NOT IN('target','guard','vectors')
 OR p_mask_body IS NULL OR octet_length(p_mask_body) NOT BETWEEN 2 AND 1048576
 OR p_root_body IS NULL OR octet_length(p_root_body) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 origin:=photo.v28_relight_preintent_producer(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base,p_selected,p_selection);
 IF origin IS NULL OR (origin->'root'->>'bytes')::bigint IS DISTINCT FROM octet_length(p_root_body)::bigint
 OR origin->'root'->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_root_body,'UTF8')),'hex')
 OR (origin->'selected'->>'bytes')::bigint IS DISTINCT FROM octet_length(p_mask_body)::bigint
 OR origin->'selected'->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_mask_body,'UTF8')),'hex') THEN RETURN NULL;END IF;
 doc:=p_root_body::jsonb;
 IF doc->'image' IS DISTINCT FROM p_base->'source'
 OR doc->(CASE WHEN p_selection='manual_guard' THEN 'protected' ELSE 'target' END) IS DISTINCT FROM p_mask_body::jsonb THEN RETURN NULL;END IF;
 SELECT created_by_task_id INTO producer FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(origin->'root'->>'artifact_id')::uuid;
 IF photo.v28_manual_mask_document_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base,(p_selected->>'artifact_id')::uuid,p_selected->>'sha256',
 (p_mask_body::jsonb->'artifact'->>'artifact_id')::uuid,p_mask_body::jsonb->'artifact'->>'sha256',p_selection='manual_guard') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 FOR child,role IN SELECT doc->'target'->'artifact','mask_target'::text UNION ALL SELECT doc->'protected'->'artifact','mask_protected'::text LOOP
 SELECT a.* INTO pixel FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=producer
 JOIN photo.task_results r ON r.tenant_id=t.tenant_id AND r.id=t.canonical_result_id
 WHERE a.tenant_id=p_tenant AND a.id=(child->>'artifact_id')::uuid AND a.sha256=child->>'sha256'
 AND a.created_by_task_id=t.id AND a.output_role=role AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/octet-stream' AND a.bytes BETWEEN 32 AND 800065536
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND r.outputs @> jsonb_build_array(child) FOR SHARE OF a;
 IF pixel.id IS NULL THEN RETURN NULL;END IF;
 END LOOP;
 IF p_kind IN('target','guard') THEN ref:=doc->CASE WHEN p_kind='target' THEN 'target' ELSE 'protected' END->'artifact';
 ELSE
 SELECT i.params INTO original FROM photo.interactive_tasks i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 WHERE i.tenant_id=p_tenant AND i.task_id=producer AND i.params_sha256=origin->'params'->>'sha256'
 AND i.declared_inputs @> jsonb_build_array(i.params->'vectors') AND t.inputs @> jsonb_build_array(i.params->'vectors') FOR SHARE OF i;
 ref:=original->'vectors';
 IF ref IS NULL OR ref='null'::jsonb OR ref IS DISTINCT FROM doc->'vectors' THEN RETURN NULL;END IF;
 END IF;
 SELECT * INTO object_row FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' FOR SHARE;
 IF object_row.id IS NULL OR object_row.state IS DISTINCT FROM 'ready' OR object_row.verified_at IS NULL
 OR object_row.object_version IS NULL OR object_row.object_version IN('','null') OR (object_row.expires_at IS NOT NULL AND object_row.expires_at<=clock_timestamp())
 OR (p_kind='vectors' AND (object_row.media_type IS DISTINCT FROM 'application/json' OR object_row.bytes NOT BETWEEN 2 AND 1048576)) THEN RETURN NULL;END IF;
 IF photo.v28_relight_preintent_producer(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base,p_selected,p_selection) IS DISTINCT FROM origin THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',object_row.id,'sha256',object_row.sha256,'bucket',object_row.bucket,'object_key',object_row.object_key,'object_version',object_row.object_version,'bytes',object_row.bytes,'media_type',object_row.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_relight_preintent_manual_object(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_preintent_manual_object(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_preintent_manual_object(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,text,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
