-- +goose Up
-- Candidate sidecars are authorized by exact canonical candidate bytes and
-- same producer attempt/fence, not by arbitrary caller artifact references.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_candidate_child(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_version uuid,p_candidate uuid,p_candidate_sha text,p_body text,p_kind text)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate photo.artifacts%ROWTYPE;producer photo.tasks%ROWTYPE;body jsonb;pin jsonb;
BEGIN
 PERFORM photo.lock_semantic_approved_params_publication(p_tenant,p_intent,p_actor,p_session,p_revision,p_target,p_target_revision,p_version,p_candidate,p_candidate_sha);
 IF p_kind IS NULL OR p_kind NOT IN('mask','transform') OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 THEN RETURN;END IF;
 SELECT a.* INTO candidate FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_candidate;
 SELECT t.* INTO producer FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=candidate.created_by_task_id FOR SHARE;
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=producer.run_id FOR SHARE;
 PERFORM r.id FROM photo.task_results r WHERE r.tenant_id=p_tenant AND r.id=producer.canonical_result_id FOR SHARE;
 SELECT a.* INTO candidate FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_candidate FOR SHARE;
 IF candidate.sha256 IS DISTINCT FROM p_candidate_sha OR candidate.bytes IS DISTINCT FROM octet_length(p_body)::bigint
 OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM p_candidate_sha
 OR candidate.state IS DISTINCT FROM 'ready' OR candidate.verified_at IS NULL
 OR candidate.media_type IS DISTINCT FROM 'application/json' OR candidate.object_version IS NULL OR candidate.object_version IN('','null')
 OR (candidate.expires_at IS NOT NULL AND candidate.expires_at<=clock_timestamp())
 OR producer.operation IS DISTINCT FROM 'semantic.transfer' OR producer.operation_version IS DISTINCT FROM '1' OR producer.state IS DISTINCT FROM 'succeeded'
 OR candidate.produced_attempt IS DISTINCT FROM producer.attempt OR candidate.produced_fence IS DISTINCT FROM producer.fencing_token
 OR NOT EXISTS(SELECT 1 FROM photo.task_results r WHERE r.tenant_id=p_tenant AND r.id=producer.canonical_result_id
 AND r.task_id=producer.id AND r.attempt=producer.attempt AND r.fencing_token=producer.fencing_token
 AND r.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_candidate,'sha256',p_candidate_sha))) THEN RETURN;END IF;
 body:=p_body::jsonb;pin:=body->p_kind;
 IF body->>'state' IS DISTINCT FROM 'proposed' OR jsonb_typeof(pin) IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(pin))<>2 OR coalesce(pin->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 RETURN QUERY SELECT CASE p_kind WHEN 'mask' THEN 'MaskRef' ELSE 'ResolvedTransform' END,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(pin->>'artifact_id')::uuid AND a.sha256=pin->>'sha256'
 AND a.created_by_task_id=producer.id AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token
 AND a.output_role=p_kind AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 8388608 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_approved_candidate_child(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_candidate_child(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_approved_candidate_child(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
