-- +goose Up
-- Issuer lineage is kept in the existing tenant-purged document table.
-- The new publication purpose is not enabled until fenced reserve/complete exist.
ALTER TABLE photo.backend_documents ADD COLUMN v28_donor_issuer_context jsonb;
ALTER TABLE photo.backend_documents ADD CONSTRAINT v28_donor_issuer_context_shape CHECK (
 v28_donor_issuer_context IS NULL OR
 (purpose='v28_donor_confirmation' AND jsonb_typeof(v28_donor_issuer_context) IS NOT DISTINCT FROM 'object'
  AND v28_donor_issuer_context->>'schema_version' IS NOT DISTINCT FROM '1'));

-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_document_scope(p_tenant uuid,p_run uuid,p_owner uuid,
 p_context jsonb,p_sha text,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE proof jsonb;current_proof jsonb;body text;project_id uuid;actor_id uuid;auth_session uuid;shoot_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_tenant IS NULL OR p_run IS NULL OR p_owner IS NULL
  OR jsonb_typeof(p_context) IS DISTINCT FROM 'object'
  OR p_context->>'schema_version' IS DISTINCT FROM '1'
  OR p_context->>'tenant_id' IS DISTINCT FROM p_tenant::text
  OR p_context->>'run_id' IS DISTINCT FROM p_run::text
  OR p_context->'params'->'confirmation'->>'artifact_id' IS DISTINCT FROM p_owner::text
  OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 project_id:=(p_context->>'project_id')::uuid;
 actor_id:=(p_context->>'actor_id')::uuid;
 auth_session:=(p_context->>'auth_session_id')::uuid;
 shoot_id:=(p_context->>'shoot_session_id')::uuid;
 body:=p_context->>'proof_jcs';
 IF body IS NULL OR octet_length(body)<>p_bytes
  OR encode(public.digest(convert_to(body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_sha
  OR p_context->'params'->'confirmation'->>'sha256' IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 proof:=body::jsonb;
 -- The exact run is owned by this actor and project, and remains unsealed.
 PERFORM r.id FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.created_by=actor_id AND r.kind='v2'
  AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
  AND w.project_id=project_id AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 FOR SHARE OF r,w;
 IF NOT FOUND THEN RETURN false;END IF;
 current_proof:=photo.build_v28_donor_confirmation(p_tenant,project_id,shoot_id,actor_id,auth_session,
  p_context->'params',(p_context->>'source_observation_id')::uuid,(p_context->>'donor_observation_id')::uuid);
 RETURN current_proof IS NOT NULL AND current_proof=proof;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_donor_document_scope(uuid,uuid,uuid,jsonb,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_document_scope(uuid,uuid,uuid,jsonb,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_donor_document_scope(uuid,uuid,uuid,jsonb,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
