-- +goose Up
-- Server-issued donor lineage plus current consent/session/source authority.
-- Exact manifest/face/mask bytes and producer replay remain worker requirements.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_worker_donor_source_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text,p_proof_json text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;proof jsonb;d photo.backend_documents%ROWTYPE;c jsonb;project_id uuid;actor_id uuid;auth_session uuid;
BEGIN
 IF photo.v28_donor_portrait_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true
 OR p_proof_json IS NULL OR octet_length(p_proof_json) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 body:=p_json::jsonb;proof:=p_proof_json::jsonb;
 SELECT doc.* INTO d FROM photo.backend_documents doc JOIN photo.artifacts a
 ON a.tenant_id=doc.tenant_id AND a.id=doc.artifact_id AND a.sha256=doc.sha256
 WHERE doc.tenant_id=p_tenant AND doc.run_id=p_run AND doc.artifact_id=(body->'confirmation'->>'artifact_id')::uuid
 AND doc.sha256=body->'confirmation'->>'sha256' AND doc.state='ready' AND doc.purpose='v28_donor_confirmation'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes=doc.bytes AND a.bucket=doc.bucket
 AND a.media_type='application/json' AND a.output_role='v28_donor_confirmation'
 AND a.object_version=doc.object_version AND a.object_version NOT IN('','null')
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||doc.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF doc,a;
 IF NOT FOUND THEN RETURN false;END IF;
 c:=d.v28_donor_issuer_context;
 IF c->>'tenant_id' IS DISTINCT FROM p_tenant::text OR c->>'run_id' IS DISTINCT FROM p_run::text
 OR (c->'params'-'confirmation') IS DISTINCT FROM (body-'confirmation')
 OR c->'params'->'confirmation'->>'sha256' IS DISTINCT FROM d.sha256
 OR c->>'proof_jcs' IS DISTINCT FROM p_proof_json OR d.bytes IS DISTINCT FROM octet_length(p_proof_json)::bigint
 OR encode(sha256(convert_to(p_proof_json,'UTF8')),'hex') IS DISTINCT FROM d.sha256 THEN RETURN false;END IF;
 project_id:=(c->>'project_id')::uuid;actor_id:=(c->>'actor_id')::uuid;auth_session:=(c->>'auth_session_id')::uuid;
 PERFORM w.id FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.project_id=project_id AND w.workflow_type='preview'
 AND w.renderer_version='v2.2' AND r.created_by=actor_id FOR SHARE OF w,r;
 IF NOT FOUND THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM (VALUES ('DonorConfirmation'::text,body->'confirmation'),
 ('FaceObservations'::text,body->'source_faces'),('FaceObservations'::text,body->'donor_faces'),
 ('WorkingImage'::text,body->'base'->'source'),('WorkingImage'::text,body->'donor'->'source'),
 ('MaskRef'::text,body->'eyes_mask'),('MaskRef'::text,body->'protected')) refs(contract,ref)
 WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.contract=refs.contract AND i.artifact_id::text=refs.ref->>'artifact_id' AND i.sha256=refs.ref->>'sha256')) THEN RETURN false;END IF;
 IF photo.lock_v28_donor_session(p_tenant,project_id,(c->>'shoot_session_id')::uuid,actor_id,auth_session,
 proof,body->'source_faces',body->'donor_faces') IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.v28_donor_portrait_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_worker_donor_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_worker_donor_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_worker_donor_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
