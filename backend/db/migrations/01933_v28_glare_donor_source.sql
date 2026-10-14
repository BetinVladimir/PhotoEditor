-- +goose Up
-- Bind a server-issued open-eyes donor confirmation to the same immutable
-- source/donor face pair used by donor-mode glare. Mask geometry is separately
-- bound by the restoration task and verified from bytes by the worker.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_glare_donor_source_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,
 p_digest text,p_proof_json text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;proof jsonb;d photo.backend_documents%ROWTYPE;c jsonb;
 project_id uuid;actor_id uuid;auth_session uuid;
BEGIN
 IF photo.v28_restoration_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,
  p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true
 OR photo.v28_restoration_qualification_ready(p_tenant,p_run,p_task,p_attempt,p_fence,
  p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true
 OR p_proof_json IS NULL OR octet_length(p_proof_json) NOT BETWEEN 2 AND 65536
 THEN RETURN false;END IF;
 body:=p_json::jsonb;proof:=p_proof_json::jsonb;
 IF body->>'mode' IS DISTINCT FROM 'donor'
  OR jsonb_typeof(body->'donor') IS DISTINCT FROM 'object'
  OR jsonb_typeof(body->'donor_faces') IS DISTINCT FROM 'object'
  OR proof->'source' IS DISTINCT FROM body->'base'
  OR proof->'donor' IS DISTINCT FROM body->'donor'
  THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
  AND t.id=p_task AND t.operation='portrait.glare' AND t.operation_version='1' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT doc.* INTO d FROM photo.backend_documents doc JOIN photo.artifacts a
 ON a.tenant_id=doc.tenant_id AND a.id=doc.artifact_id AND a.sha256=doc.sha256
 WHERE doc.tenant_id=p_tenant AND doc.run_id=p_run
 AND doc.artifact_id=(body->'confirmation'->>'artifact_id')::uuid
 AND doc.sha256=body->'confirmation'->>'sha256'
 AND doc.state='ready' AND doc.purpose='v28_donor_confirmation'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes=doc.bytes
 AND a.bucket=doc.bucket AND a.media_type='application/json'
 AND a.output_role='v28_donor_confirmation'
 AND a.object_version=doc.object_version AND a.object_version NOT IN('','null')
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||doc.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF doc,a;
 IF NOT FOUND THEN RETURN false;END IF;
 c:=d.v28_donor_issuer_context;
 IF c->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR c->>'run_id' IS DISTINCT FROM p_run::text
 OR c->'params'->'base' IS DISTINCT FROM body->'base'
 OR c->'params'->'donor' IS DISTINCT FROM body->'donor'
 OR c->'params'->'source_faces' IS DISTINCT FROM body->'source_faces'
 OR c->'params'->'donor_faces' IS DISTINCT FROM body->'donor_faces'
 OR c->'params'->'eyes_mask' IS DISTINCT FROM body->'glasses_mask'
 OR c->'params'->'protected' IS DISTINCT FROM body->'protected'
 OR c->'params'->'strength' IS DISTINCT FROM body->'strength'
 OR d.owner_id::text IS DISTINCT FROM c->'params'->'confirmation'->>'artifact_id'
 OR d.owner_id IS DISTINCT FROM p_task
 OR c->'params'->'confirmation'->>'sha256' IS DISTINCT FROM d.sha256
 OR d.artifact_id::text IS DISTINCT FROM body->'confirmation'->>'artifact_id'
 OR c->>'proof_jcs' IS DISTINCT FROM p_proof_json
 OR d.bytes IS DISTINCT FROM octet_length(p_proof_json)::bigint
 OR encode(sha256(convert_to(p_proof_json,'UTF8')),'hex') IS DISTINCT FROM d.sha256
 THEN RETURN false;END IF;
 project_id:=(c->>'project_id')::uuid;
 actor_id:=(c->>'actor_id')::uuid;
 auth_session:=(c->>'auth_session_id')::uuid;
 PERFORM w.run_id FROM photo.v2_workflows w JOIN photo.runs r
 ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.project_id=project_id
 AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 AND r.created_by=actor_id FOR SHARE OF w,r;
 IF NOT FOUND THEN RETURN false;END IF;
 IF photo.lock_v28_donor_session(p_tenant,project_id,
  (c->>'shoot_session_id')::uuid,actor_id,auth_session,
  proof,body->'source_faces',body->'donor_faces') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 RETURN photo.v28_restoration_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,
  p_worker,p_params,p_sha,p_json,p_digest)
  AND photo.v28_restoration_qualification_ready(p_tenant,p_run,p_task,p_attempt,
   p_fence,p_worker,p_params,p_sha,p_json,p_digest);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_glare_donor_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_glare_donor_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_glare_donor_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
