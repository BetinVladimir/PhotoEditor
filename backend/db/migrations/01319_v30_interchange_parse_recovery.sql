-- +goose Up
-- Historical commit acknowledgement only; does not authorize fresh execution.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_parse_adoption_committed(p_tenant uuid,p_task uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;outputs jsonb;marker uuid;root jsonb;receipt jsonb;n bigint;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 IF NOT FOUND OR t.operation<>'v2.interchange.parse' OR t.operation_version<>'1' OR t.state<>'succeeded' OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=t.run_id AND workflow_type='interchange' AND renderer_version='v2.2';
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT z.outputs INTO outputs FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=t.canonical_result_id AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token FOR SHARE;
 IF jsonb_typeof(outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(outputs)<>1 THEN RETURN false;END IF;
 root:=outputs->0;marker:=photo.v2_adoption_event_id(t.id,t.canonical_result_id);
 IF NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt' AND event_id=marker)
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-interchange-ingest-started' AND event_id=marker)
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-interchange-parsed' AND event_id=marker) THEN RETURN false;END IF;
 SELECT payload INTO receipt FROM photo.run_events WHERE tenant_id=p_tenant AND run_id=t.run_id AND type='v2.task.adopted'
 AND payload->>'task_id'=t.id::text AND payload->>'result_id'=t.canonical_result_id::text
 AND payload->>'attempt'=t.attempt::text AND payload->>'fence'=t.fencing_token::text;
 IF receipt IS NULL OR coalesce(receipt->>'inventory_digest','') !~ '^[0-9a-f]{64}$'
 OR coalesce(receipt->>'inventory_count','') !~ '^[1-9][0-9]*$' THEN RETURN false;END IF;
 n:=(receipt->>'inventory_count')::bigint;
 IF n NOT BETWEEN 2 AND 30001 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
 WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id AND l.ordinal=0
 AND l.purpose=encode(sha256(convert_to('tasks/'||t.id::text||'/$','UTF8')),'hex')
 AND l.artifact_id=(root->>'artifact_id')::uuid AND l.sha256=root->>'sha256'
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND a.media_type='application/json' AND a.output_role='document') THEN RETURN false;END IF;
 RETURN (SELECT count(*) FROM photo.output_uploads u WHERE u.tenant_id=p_tenant AND u.task_id=t.id AND u.attempt=t.attempt AND u.fence=t.fencing_token)=n
 AND NOT EXISTS(SELECT 1 FROM photo.output_uploads u WHERE u.tenant_id=p_tenant AND u.task_id=t.id AND u.attempt=t.attempt AND u.fence=t.fencing_token
 AND (u.state<>'ready' OR NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l
 WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id AND l.artifact_id=u.artifact_id AND l.sha256=u.sha256
 AND ((l.ordinal=0 AND l.purpose=encode(sha256(convert_to('tasks/'||t.id::text||'/$','UTF8')),'hex'))
 OR (l.ordinal BETWEEN 1 AND n-1 AND l.purpose=encode(sha256(convert_to('tasks/'||t.id::text||'/$/files/'||(l.ordinal-1)::text||'/content','UTF8')),'hex'))))));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_interchange_parse_adoption_committed(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_parse_adoption_committed(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_parse_adoption_committed(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
