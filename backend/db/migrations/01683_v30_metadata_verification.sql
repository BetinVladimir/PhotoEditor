-- +goose Up
-- Verification receipt only; does not publish an export or adopt a task.
-- +goose StatementBegin
CREATE FUNCTION photo.record_v30_metadata_verification(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_params text,p_before text,p_after text,p_report text,p_projection text,p_raster_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;e photo.graph_expansions%ROWTYPE;
 proof jsonb;params jsonb;before_doc jsonb;after_doc jsonb;report jsonb;projection jsonb;
 approved text[];expected_fields jsonb;expected_policy text;doc text;role_name text;ref jsonb;parent_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_raster_bytes IS NULL OR p_raster_bytes NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 FOREACH doc IN ARRAY ARRAY[p_params,p_before,p_after,p_report,p_projection] LOOP
  IF doc IS NULL OR octet_length(doc) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 END LOOP;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF r.id IS NULL THEN RETURN false;END IF;
 parent_id:=(r.config_snapshot->'v30_delivery_export'->>'parent_run_id')::uuid;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(r.id,parent_id) ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=r.id
 AND kind='v30.delivery.metadata/1' AND state='closed' FOR SHARE;
 IF NOT FOUND OR NOT photo.v30_metadata_result_ready(p_tenant,p_task,p_attempt,p_fence,p_result) THEN RETURN false;END IF;
 proof:=e.v30_metadata_proof_jcs::jsonb;
 IF p_params IS DISTINCT FROM proof->>'params_jcs' OR p_before IS DISTINCT FROM proof->>'encoded_jcs'
 OR p_projection IS DISTINCT FROM proof->>'projection_jcs' THEN RETURN false;END IF;
 params:=p_params::jsonb;before_doc:=p_before::jsonb;after_doc:=p_after::jsonb;report:=p_report::jsonb;projection:=p_projection::jsonb;
 expected_policy:=CASE proof->>'policy' WHEN 'copyright' THEN 'copyright_only' WHEN 'approved_exif' THEN 'safe_camera' ELSE '' END;
 IF after_doc->>'metadata_policy' IS DISTINCT FROM expected_policy
 OR (after_doc-'artifact'-'metadata_policy') IS DISTINCT FROM (before_doc-'artifact'-'metadata_policy')
 OR jsonb_typeof(report) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(report))<>7
 OR report->'encoded' IS DISTINCT FROM params->'encoded' OR report->'document' IS DISTINCT FROM params->'document'
 OR report->>'policy' IS DISTINCT FROM expected_policy OR report->'orientation' IS DISTINCT FROM '1'::jsonb
 OR report->'stale_previews_removed' IS DISTINCT FROM 'true'::jsonb
 OR report->'icc_preserved' IS DISTINCT FROM 'true'::jsonb THEN RETURN false;END IF;
 SELECT coalesce(jsonb_agg(name ORDER BY ordinal),'[]'::jsonb) INTO expected_fields
 FROM unnest(ARRAY['author','copyright','camera_make','camera_model','lens','datetime_original','gps']) WITH ORDINALITY fields(name,ordinal)
 WHERE projection->name IS NOT NULL AND projection->name<>'null'::jsonb;
 IF report->'copied_fields' IS DISTINCT FROM expected_fields THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(
 t.params_artifact_id,(params->'document'->>'artifact_id')::uuid,(params->'encoded'->>'artifact_id')::uuid,
 (before_doc->'artifact'->>'artifact_id')::uuid,(after_doc->'artifact'->>'artifact_id')::uuid) ORDER BY id FOR SHARE;
 PERFORM a.id FROM photo.artifacts a JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
 WHERE u.tenant_id=p_tenant AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence ORDER BY a.id FOR SHARE OF a;
 PERFORM id FROM photo.output_uploads WHERE tenant_id=p_tenant AND task_id=p_task AND attempt=p_attempt AND fence=p_fence ORDER BY id FOR SHARE;
 IF (after_doc->'artifact'->>'artifact_id')::uuid IN(t.params_artifact_id,(params->'document'->>'artifact_id')::uuid,
 (params->'encoded'->>'artifact_id')::uuid,(before_doc->'artifact'->>'artifact_id')::uuid) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v30_metadata_result_artifact(p_tenant,p_task,p_attempt,p_fence,p_result,
 (after_doc->'artifact'->>'artifact_id')::uuid) a WHERE a.source='output' AND a.role='encoded_image'
 AND a.sha256=after_doc->'artifact'->>'sha256' AND a.bytes=p_raster_bytes
 AND a.media_type=CASE after_doc->>'format' WHEN 'jpeg' THEN 'image/jpeg' WHEN 'png' THEN 'image/png' WHEN 'tiff' THEN 'image/tiff' ELSE '' END) THEN RETURN false;END IF;
 FOREACH role_name IN ARRAY ARRAY['encoded_manifest','metadata_report'] LOOP
  doc:=CASE role_name WHEN 'encoded_manifest' THEN p_after ELSE p_report END;
  IF NOT EXISTS(SELECT 1 FROM photo.output_uploads u
  CROSS JOIN LATERAL photo.v30_metadata_result_artifact(p_tenant,p_task,p_attempt,p_fence,p_result,u.artifact_id) a
  WHERE u.tenant_id=p_tenant AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence
  AND a.source='output' AND a.role=role_name AND a.media_type='application/json'
  AND a.sha256=encode(sha256(convert_to(doc,'UTF8')),'hex') AND a.bytes=octet_length(doc)) THEN RETURN false;END IF;
 END LOOP;
 IF NOT photo.v30_metadata_result_ready(p_tenant,p_task,p_attempt,p_fence,p_result) THEN RETURN false;END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
 VALUES(p_tenant,'v30-metadata-verified',photo.v2_adoption_event_id(p_task,p_result),clock_timestamp()) ON CONFLICT DO NOTHING;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.record_v30_metadata_verification(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v30_metadata_verification(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,bigint) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.record_v30_metadata_verification(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
