-- +goose Up
-- Private final-commit validation; caller must hold current review source/target locks
-- and verify initialization documents before using this predicate.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_params_ready(p_tenant uuid,p_parent uuid,p_intent uuid,p_decisions jsonb,p_params jsonb,p_documents jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n integer; approvals integer;
BEGIN
 IF jsonb_typeof(p_decisions) IS DISTINCT FROM 'array' OR jsonb_typeof(p_params) IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_documents) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 n:=jsonb_array_length(p_params);
 approvals:=(SELECT count(*) FROM jsonb_array_elements(p_decisions) d WHERE d->>'decision'='approve');
 IF n NOT BETWEEN 1 AND 100 OR n<>approvals
 OR (SELECT count(DISTINCT p->>'target_id') FROM jsonb_array_elements(p_params) p)<>n
 OR (SELECT count(DISTINCT p->>'task_id') FROM jsonb_array_elements(p_params) p)<>n
 OR (SELECT count(DISTINCT p->>'artifact_id') FROM jsonb_array_elements(p_params) p)<>n THEN RETURN false;END IF;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant
 AND a.id::text IN(SELECT p->>'artifact_id' FROM jsonb_array_elements(p_params) p) ORDER BY a.id FOR SHARE;
 RETURN NOT EXISTS(
 WITH candidates AS MATERIALIZED(SELECT d->>'artifact_id' AS id,d->>'sha256' AS sha,(d->>'body')::jsonb AS body
 FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='TransferCandidate'),
 intents AS MATERIALIZED(SELECT d->>'artifact_id' AS id,d->>'sha256' AS sha,(d->>'body')::jsonb AS body
 FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='EditIntent')
 SELECT 1 FROM jsonb_array_elements(p_params) p
 LEFT JOIN photo.semantic_targets t ON t.tenant_id=p_tenant AND t.intent_id=p_intent AND t.id::text=p->>'target_id'
 LEFT JOIN photo.semantic_intents i ON i.tenant_id=p_tenant AND i.id=p_intent
 LEFT JOIN photo.v2_workflows w ON w.tenant_id=p_tenant AND w.id=i.workflow_id AND w.run_id=p_parent
 LEFT JOIN candidates c ON c.id=t.candidate_artifact_id::text
 LEFT JOIN intents original ON original.id=i.intent_artifact_id::text AND original.sha=i.intent_sha
 LEFT JOIN photo.backend_documents b ON b.tenant_id=p_tenant AND b.run_id=p_parent
 AND b.owner_id::text=p->>'task_id' AND b.purpose='task_params'
 LEFT JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=b.artifact_id
 WHERE t.id IS NULL OR w.id IS NULL OR c.id IS NULL OR original.id IS NULL OR t.state<>'proposed'
 OR t.child_run_id IS NOT NULL OR t.batch_id IS NOT NULL
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_decisions) d WHERE d->>'target_id'=t.id::text AND d->>'decision'='approve')
 OR (p-'task_id'-'target_id'-'artifact_id'-'sha256'-'body')<>'{}'::jsonb
 OR jsonb_typeof(p->'body') IS DISTINCT FROM 'string' OR octet_length(p->>'body') NOT BETWEEN 2 AND 1048576
 OR b.id IS NULL OR b.state<>'ready' OR b.artifact_id::text IS DISTINCT FROM p->>'artifact_id'
 OR b.sha256 IS DISTINCT FROM p->>'sha256' OR b.sha256 IS DISTINCT FROM encode(sha256(convert_to(p->>'body','UTF8')),'hex')
 OR b.bytes IS DISTINCT FROM octet_length(p->>'body') OR a.id IS NULL OR a.state<>'ready'
 OR a.verified_at IS NULL OR a.sha256 IS DISTINCT FROM b.sha256 OR a.bytes IS DISTINCT FROM b.bytes
 OR a.bucket IS DISTINCT FROM b.bucket OR a.object_version IS DISTINCT FROM b.object_version
 OR a.object_version IS NULL OR a.object_version IN('','null') OR a.media_type<>'application/json' OR a.output_role<>'task_params'
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())
 OR (p->>'body')::jsonb IS DISTINCT FROM jsonb_build_object('schema_version',1,'target_id',t.id,
 'source',c.body->'target','intent',jsonb_build_object('artifact_id',i.intent_artifact_id,'sha256',i.intent_sha),
 'candidate',jsonb_build_object('artifact_id',c.id,'sha256',c.sha),'mask',c.body->'mask','transform',c.body->'transform',
 'kind',original.body->'kind','strength',original.body->'strength','ev',original.body->'ev','prompt',original.body->'prompt','preserve_persons',true)
 OR c.body->>'state' IS DISTINCT FROM 'proposed' OR c.body->'mask' IS NULL OR c.body->'mask'='null'::jsonb
 OR c.body->'transform' IS NULL OR c.body->'transform'='null'::jsonb OR original.body->'preserve_persons' IS DISTINCT FROM 'true'::jsonb);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_approved_params_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_params_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
