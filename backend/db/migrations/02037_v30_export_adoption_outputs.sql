-- +goose Up
-- Scheduler output lookup is rooted in the exact leased canonical result.
-- Positional roles cannot be supplied by the caller or a mutable archive.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_output(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,
 bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE z photo.task_results%ROWTYPE;t photo.tasks%ROWTYPE;
 output_count integer;selected_ordinal integer;selected_count integer;
 required_role text;maximum_bytes bigint;
BEGIN
 IF p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR p_contract IS NULL OR p_contract NOT IN('InterchangeResult','MappingReport','MappingShard','InterchangeArchive') THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task FOR SHARE;
 IF t.id IS NULL OR z.id IS NULL OR t.canonical_result_id IS DISTINCT FROM z.id
 OR t.state IS DISTINCT FROM 'succeeded' OR t.cancel_requested
 OR z.attempt IS DISTINCT FROM t.attempt OR z.fencing_token IS DISTINCT FROM t.fencing_token
 OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' THEN RETURN;END IF;
 output_count:=jsonb_array_length(z.outputs);
 IF output_count NOT BETWEEN 3 AND 53
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(z.outputs))<>output_count THEN RETURN;END IF;
 SELECT count(*),min(ordinality::integer-1) INTO selected_count,selected_ordinal
 FROM jsonb_array_elements(z.outputs) WITH ORDINALITY AS output(value,ordinality)
 WHERE value=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha);
 IF selected_count<>1 THEN RETURN;END IF;
 IF (p_contract='InterchangeResult' AND selected_ordinal<>0)
 OR (p_contract='MappingReport' AND selected_ordinal<>1)
 OR (p_contract='MappingShard' AND (selected_ordinal<2 OR selected_ordinal>=output_count-1))
 OR (p_contract='InterchangeArchive' AND selected_ordinal<>output_count-1) THEN RETURN;END IF;
 required_role:=CASE p_contract WHEN 'InterchangeResult' THEN 'document'
  WHEN 'MappingReport' THEN 'report' WHEN 'MappingShard' THEN 'mapping_shard'
  ELSE 'bundle' END;
 maximum_bytes:=CASE p_contract WHEN 'InterchangeResult' THEN 65536
  WHEN 'InterchangeArchive' THEN 68719476736 ELSE 8388608 END;
 RETURN QUERY SELECT a.bucket,a.key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact
  AND a.sha256=p_sha AND a.created_by_task_id=p_task
  AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
  AND a.output_role=required_role AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.bytes BETWEEN 2 AND maximum_bytes
  AND a.media_type=CASE WHEN p_contract='InterchangeArchive' THEN 'application/zip'
   ELSE 'application/json' END
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs input
   WHERE input.tenant_id=p_tenant AND input.task_id=p_task AND input.artifact_id=a.id)
 FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
