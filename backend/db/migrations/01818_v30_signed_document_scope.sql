-- +goose Up
-- Two private, lease-fenced document purposes for the required-provenance
-- assembly branch. Existing off-mode assembly/shard bytes retain their caps.
-- +goose StatementBegin
DO $$ DECLARE expression text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint
 WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL OR position('v30_delivery_assembly' IN expression)=0 THEN
  RAISE EXCEPTION 'DELIVERY_SIGNED_DOCUMENT_PURPOSE_SHAPE';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose IN (''v30_delivery_signed_closure'',''v30_delivery_signed_envelope''))',expression);
END $$;
-- +goose StatementEnd
ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_v30_delivery_document_exact;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v30_delivery_document_exact
 CHECK(v30_delivery_document_jcs IS NULL OR (
  ((purpose='v30_delivery_assembly' OR purpose ~ '^v30_delivery_slots_[0-9]{3}$')
    AND octet_length(v30_delivery_document_jcs) BETWEEN 2 AND 1048576)
  OR (purpose='v30_delivery_signed_closure' AND octet_length(v30_delivery_document_jcs) BETWEEN 2 AND 8388608)
  OR (purpose='v30_delivery_signed_envelope' AND octet_length(v30_delivery_document_jcs) BETWEEN 2 AND 65536)
 ) AND octet_length(v30_delivery_document_jcs)=bytes
   AND encode(sha256(convert_to(v30_delivery_document_jcs,'UTF8')),'hex')=sha256);

-- Scope is checked both before reserve and before completing a physical
-- object version. No caller-supplied tenant/run/owner identity is trusted.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_document_scope(p_expansion uuid,p_token uuid,p_purpose text,p_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE x photo.graph_expansions%ROWTYPE;frozen jsonb;body jsonb;c photo.delivery_signing_closures%ROWTYPE;
 owner uuid;assembly_ref jsonb;closure_ref jsonb;assembly_doc photo.backend_documents%ROWTYPE;
 closure_doc photo.backend_documents%ROWTYPE;
BEGIN
 IF p_purpose IS NULL OR p_purpose NOT IN('v30_delivery_signed_closure','v30_delivery_signed_envelope')
  OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND
   CASE WHEN p_purpose='v30_delivery_signed_closure' THEN 8388608 ELSE 65536 END THEN RETURN NULL;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1'
  AND state='open' AND expansion_token=p_token AND expansion_lease_until>clock_timestamp();
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=x.tenant_id AND status='active')
  OR NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=x.tenant_id AND id=x.run_id
   AND status='running' AND NOT cancel_requested) THEN RETURN NULL;END IF;
 IF photo.v30_delivery_signing_assembly_gate(p_expansion,p_token) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id
  AND b.owner_id=x.parent_task_id AND b.purpose='task_params' AND b.state='ready'
  AND b.v30_delivery_proof IS NOT NULL;
 IF frozen IS NULL OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required' THEN RETURN NULL;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(x.run_id,'workflow'),'v2.delivery.assemble:0');
 SELECT c.* INTO c FROM photo.delivery_signing_closures c
 JOIN photo.delivery_packages d ON d.tenant_id=c.tenant_id AND d.id=c.package_id
 JOIN photo.v2_workflows w ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id
 WHERE c.tenant_id=x.tenant_id AND c.parent_run_id=x.run_id AND w.run_id=x.run_id
  AND c.project_id=d.project_id AND d.state IN('building','review')
  AND c.actor_id=(SELECT created_by FROM photo.runs WHERE tenant_id=x.tenant_id AND id=x.run_id);
 IF NOT FOUND OR c.policy_id::text IS DISTINCT FROM frozen->'policy'->>'signing_policy_id'
  OR c.policy_sha IS DISTINCT FROM frozen->'policy'->>'signing_policy_sha' THEN RETURN NULL;END IF;
 IF p_purpose='v30_delivery_signed_closure' THEN
  IF p_body IS DISTINCT FROM c.closure_jcs OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM c.closure_sha THEN RETURN NULL;END IF;
 ELSE
  body:=p_body::jsonb;
  IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(body))<>3
   OR body->'schema_version' IS DISTINCT FROM '2'::jsonb THEN RETURN NULL;END IF;
  assembly_ref:=body->'assembly';closure_ref:=body->'signing_closure';
  IF jsonb_typeof(assembly_ref) IS DISTINCT FROM 'object' OR jsonb_typeof(closure_ref) IS DISTINCT FROM 'object'
   OR (SELECT count(*) FROM jsonb_object_keys(assembly_ref))<>2
   OR (SELECT count(*) FROM jsonb_object_keys(closure_ref))<>2
   OR assembly_ref->>'artifact_id'=closure_ref->>'artifact_id' THEN RETURN NULL;END IF;
  SELECT * INTO assembly_doc FROM photo.backend_documents d WHERE d.tenant_id=x.tenant_id
   AND d.run_id=x.run_id AND d.owner_id=owner AND d.purpose='v30_delivery_assembly'
   AND d.artifact_id=(assembly_ref->>'artifact_id')::uuid AND d.sha256=assembly_ref->>'sha256'
   AND d.state='ready' AND d.v30_delivery_document_jcs IS NOT NULL;
  SELECT * INTO closure_doc FROM photo.backend_documents d WHERE d.tenant_id=x.tenant_id
   AND d.run_id=x.run_id AND d.owner_id=owner AND d.purpose='v30_delivery_signed_closure'
   AND d.artifact_id=(closure_ref->>'artifact_id')::uuid AND d.sha256=closure_ref->>'sha256'
   AND d.state='ready' AND d.v30_delivery_document_jcs=c.closure_jcs;
  IF assembly_doc.id IS NULL OR closure_doc.id IS NULL OR
   (assembly_doc.v30_delivery_document_jcs::jsonb)->'policy'->>'provenance' IS DISTINCT FROM 'required'
   OR photo.v30_delivery_saved_assembly(p_expansion,p_token,assembly_doc.artifact_id,assembly_doc.sha256) IS NULL
   OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND a.id=closure_doc.artifact_id
    AND a.sha256=closure_doc.sha256 AND a.state='ready' AND a.verified_at IS NOT NULL
    AND a.bytes=closure_doc.bytes AND a.bucket=closure_doc.bucket
    AND a.object_key='tenants/'||x.tenant_id||'/runs/'||x.run_id||'/documents/'||a.id
    AND a.object_version=closure_doc.object_version AND a.object_version NOT IN('','null')
    AND a.output_role=closure_doc.purpose AND a.media_type='application/json'
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=x.id AND expansion_token=p_token
  AND expansion_lease_until>clock_timestamp()) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('tenant',x.tenant_id,'run',x.run_id,'owner',owner,
  'sha',encode(sha256(convert_to(p_body,'UTF8')),'hex'),'bytes',octet_length(p_body));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_document_scope(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_document_scope(uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
