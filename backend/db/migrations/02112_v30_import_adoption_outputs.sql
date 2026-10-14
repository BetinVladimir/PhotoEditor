-- +goose Up
-- Output pins are read only under the current discovery lease and the exact
-- succeeded canonical result. The caller cannot choose an output ordinal.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_adoption_root_output(p_tenant uuid,p_workflow uuid,
 p_run uuid,p_task uuid,p_result uuid,p_attempt integer,p_fence bigint,p_token uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,
 object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE z photo.task_results%ROWTYPE;t photo.tasks%ROWTYPE;
 root_id uuid;root_sha text;count_outputs integer;
BEGIN
 IF photo.v30_import_adoption_claim_current(p_tenant,p_workflow,p_run,p_task,
  p_result,p_attempt,p_fence,p_token) IS DISTINCT FROM true OR
  photo.v30_interchange_import_adoption_context(p_tenant,p_task,p_attempt,p_fence,
   p_result) IS NULL THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run FOR SHARE;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result
  AND task_id=p_task FOR SHARE;
 IF t.id IS NULL OR z.id IS NULL OR t.state IS DISTINCT FROM 'succeeded'
  OR t.cancel_requested OR t.canonical_result_id IS DISTINCT FROM z.id
  OR z.attempt IS DISTINCT FROM p_attempt OR z.fencing_token IS DISTINCT FROM p_fence
  OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' THEN RETURN;END IF;
 count_outputs:=jsonb_array_length(z.outputs);
 IF count_outputs NOT BETWEEN 3 AND 52 OR
  (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(z.outputs))<>count_outputs
  THEN RETURN;END IF;
 root_id:=(z.outputs->0->>'artifact_id')::uuid;
 root_sha:=z.outputs->0->>'sha256';
 IF root_id IS NULL OR root_sha IS NULL OR root_sha !~ '^[0-9a-f]{64}$'
  OR z.outputs->0 IS DISTINCT FROM jsonb_build_object('artifact_id',root_id,'sha256',root_sha)
  THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256,a.bucket,a.key,a.object_version,a.bytes,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=root_id AND a.sha256=root_sha
  AND a.created_by_task_id=p_task AND a.produced_attempt=p_attempt
  AND a.produced_fence=p_fence AND a.output_role='document'
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.bytes BETWEEN 2 AND 65536 AND a.media_type='application/json'
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs input
   WHERE input.tenant_id=p_tenant AND input.task_id=p_task AND input.artifact_id=a.id)
 FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_adoption_root_output(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_adoption_root_output(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_adoption_root_output(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_adoption_output(p_tenant uuid,p_workflow uuid,
 p_run uuid,p_task uuid,p_result uuid,p_attempt integer,p_fence bigint,p_token uuid,
 p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,
 bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE z photo.task_results%ROWTYPE;selected_ordinal integer;
 selected_count integer;count_outputs integer;required_role text;maximum_bytes bigint;
BEGIN
 IF p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_contract IS NULL OR p_contract NOT IN('InterchangeResult','MappingReport','MappingShard') THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_import_adoption_root_output(p_tenant,p_workflow,p_run,
  p_task,p_result,p_attempt,p_fence,p_token);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result
  AND task_id=p_task FOR SHARE;
 IF z.id IS NULL OR z.attempt IS DISTINCT FROM p_attempt OR z.fencing_token IS DISTINCT FROM p_fence
  OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' THEN RETURN;END IF;
 count_outputs:=jsonb_array_length(z.outputs);
 IF count_outputs NOT BETWEEN 3 AND 52 THEN RETURN;END IF;
 SELECT count(*),min(ordinality::integer-1) INTO selected_count,selected_ordinal
 FROM jsonb_array_elements(z.outputs) WITH ORDINALITY AS output(value,ordinality)
 WHERE value=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha);
 IF selected_count<>1 OR (p_contract='InterchangeResult' AND selected_ordinal<>0)
  OR (p_contract='MappingReport' AND selected_ordinal<>1)
  OR (p_contract='MappingShard' AND selected_ordinal<2) THEN RETURN;END IF;
 required_role:=CASE p_contract WHEN 'InterchangeResult' THEN 'document'
  WHEN 'MappingReport' THEN 'report' ELSE 'mapping_shard' END;
 maximum_bytes:=CASE p_contract WHEN 'InterchangeResult' THEN 65536 ELSE 8388608 END;
 RETURN QUERY SELECT a.bucket,a.key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha
  AND a.created_by_task_id=p_task AND a.produced_attempt=p_attempt
  AND a.produced_fence=p_fence AND a.output_role=required_role
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.bytes BETWEEN 2 AND maximum_bytes AND a.media_type='application/json'
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs input
   WHERE input.tenant_id=p_tenant AND input.task_id=p_task AND input.artifact_id=a.id)
 FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_adoption_output(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_adoption_output(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_adoption_output(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
