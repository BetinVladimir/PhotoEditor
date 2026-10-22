-- +goose Up
-- Source-only authority is deliberately separate from donor identity matching.
-- It does not admit a graph or publish a confirmation by itself.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v28_glare_source_session(p_tenant uuid,p_project uuid,p_shoot uuid,
 p_actor uuid,p_auth_session uuid,p_proof jsonb,p_source_faces jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE s photo.shoot_sessions%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session) IS DISTINCT FROM true
  OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object' OR p_proof->>'schema_version' IS DISTINCT FROM '1'
  OR p_proof->>'actor_id' IS DISTINCT FROM p_actor::text
  OR p_proof->'donor' IS DISTINCT FROM 'null'::jsonb
  OR p_proof->'donor_face_id' IS DISTINCT FROM 'null'::jsonb
  OR p_proof->'donor_observation_id' IS DISTINCT FROM 'null'::jsonb
  OR jsonb_typeof(p_source_faces) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 PERFORM person.id FROM photo.project_persons person
 JOIN photo.consent_grants g ON g.tenant_id=person.tenant_id AND g.project_id=person.project_id
  AND g.person_id=person.id AND g.id=(p_proof->>'consent_id')::uuid
  AND g.purpose='identity_matching' AND g.status='granted' AND g.epoch=person.consent_epoch
 WHERE person.tenant_id=p_tenant AND person.project_id=p_project
  AND person.id=(p_proof->>'person_id')::uuid AND person.state='active'
  AND person.consent_epoch::text=p_proof->>'epoch' FOR SHARE OF person,g;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.shoot_sessions WHERE tenant_id=p_tenant AND project_id=p_project
  AND id=p_shoot AND state IN('open','closed') FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM c.id FROM photo.collections c WHERE c.tenant_id=p_tenant AND c.project_id=p_project
  AND c.id=s.collection_id AND c.deleted_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM v.id FROM photo.asset_versions v
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.collection_items ci ON ci.tenant_id=v.tenant_id AND ci.collection_id=s.collection_id
  AND ci.asset_id=a.id AND ci.asset_version_id=v.id
 JOIN photo.artifacts image ON image.tenant_id=v.tenant_id AND image.id=v.master_artifact_id AND image.sha256=v.content_hash
 JOIN photo.person_observations o ON o.tenant_id=v.tenant_id AND o.project_id=p_project
  AND o.id=(p_proof->>'source_observation_id')::uuid AND o.asset_version_id=v.id
  AND o.face_id=(p_proof->>'source_face_id')::uuid AND o.person_id=(p_proof->>'person_id')::uuid
  AND o.confirmed_by IS NOT NULL AND o.evidence_artifact_id=(p_source_faces->>'artifact_id')::uuid
 JOIN photo.artifacts evidence ON evidence.tenant_id=o.tenant_id AND evidence.id=o.evidence_artifact_id
  AND evidence.sha256=p_source_faces->>'sha256'
 WHERE v.tenant_id=p_tenant AND v.id=(p_proof->'source'->>'asset_version_id')::uuid
  AND v.master_artifact_id=(p_proof->'source'->'source'->>'artifact_id')::uuid
  AND v.content_hash=p_proof->'source'->'source'->>'sha256'
  AND v.review_state='accepted' AND a.project_id=p_project AND a.deleted_at IS NULL AND a.upload_state='verified'
  AND image.state='ready' AND image.verified_at IS NOT NULL AND image.media_type='application/json'
  AND image.object_version IS NOT NULL AND image.object_version NOT IN('','null')
  AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
  AND evidence.state='ready' AND evidence.verified_at IS NOT NULL AND evidence.media_type='application/json'
  AND evidence.bytes BETWEEN 2 AND 1048576 AND evidence.object_version IS NOT NULL
  AND evidence.object_version NOT IN('','null') AND (evidence.expires_at IS NULL OR evidence.expires_at>clock_timestamp())
 FOR SHARE OF v,a,ci,image,o,evidence;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v28_glare_source_session(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_glare_source_session(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v28_glare_source_session(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb) TO photo_scheduler;

CREATE FUNCTION photo.build_v28_glare_source_confirmation(p_tenant uuid,p_project uuid,p_shoot uuid,
 p_actor uuid,p_auth_session uuid,p_params jsonb,p_observation uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE person_id uuid;face_id uuid;grant_id uuid;epoch_value text;n bigint;proof jsonb;
BEGIN
 IF p_observation IS NULL OR p_params->>'mode' IS DISTINCT FROM 'generative'
  OR p_params->'donor' IS DISTINCT FROM 'null'::jsonb OR p_params->'donor_faces' IS DISTINCT FROM 'null'::jsonb
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT o.person_id,o.face_id INTO person_id,face_id FROM photo.person_observations o
 WHERE o.tenant_id=p_tenant AND o.project_id=p_project AND o.id=p_observation
  AND o.asset_version_id=(p_params->'base'->>'asset_version_id')::uuid
  AND o.evidence_artifact_id=(p_params->'source_faces'->>'artifact_id')::uuid AND o.confirmed_by IS NOT NULL;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT count(*),min(g.id::text)::uuid,min(g.epoch)::text INTO n,grant_id,epoch_value
 FROM photo.consent_grants g JOIN photo.project_persons p ON p.tenant_id=g.tenant_id
  AND p.project_id=g.project_id AND p.id=g.person_id AND p.state='active' AND p.consent_epoch=g.epoch
 WHERE g.tenant_id=p_tenant AND g.project_id=p_project AND g.person_id=person_id
  AND g.purpose='identity_matching' AND g.status='granted';
 IF n<>1 THEN RETURN NULL;END IF;
 proof:=jsonb_build_object('schema_version',1,'person_id',person_id,'source',p_params->'base','donor',NULL,
  'source_face_id',face_id,'donor_face_id',NULL,'source_observation_id',p_observation,'donor_observation_id',NULL,
  'consent_id',grant_id,'epoch',epoch_value,'actor_id',p_actor);
 IF photo.lock_v28_glare_source_session(p_tenant,p_project,p_shoot,p_actor,p_auth_session,proof,p_params->'source_faces')
  IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN proof;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.build_v28_glare_source_confirmation(uuid,uuid,uuid,uuid,uuid,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.build_v28_glare_source_confirmation(uuid,uuid,uuid,uuid,uuid,jsonb,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.build_v28_glare_source_confirmation(uuid,uuid,uuid,uuid,uuid,jsonb,uuid) TO photo_scheduler;

ALTER TABLE photo.backend_documents ADD COLUMN v28_glare_source_issuer_context jsonb;
ALTER TABLE photo.backend_documents ADD CONSTRAINT v28_glare_source_context_shape CHECK(
 (purpose='v28_glare_source_confirmation' AND v28_glare_source_issuer_context IS NOT NULL
  AND jsonb_typeof(v28_glare_source_issuer_context) IS NOT DISTINCT FROM 'object'
  AND v28_glare_source_issuer_context->>'schema_version' IS NOT DISTINCT FROM '1')
 OR (purpose<>'v28_glare_source_confirmation' AND v28_glare_source_issuer_context IS NULL));
DO $$ DECLARE definition text;BEGIN
 SELECT pg_get_constraintdef(oid) INTO STRICT definition FROM pg_constraint
 WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE 'ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ('
  ||substring(definition from 8 for length(definition)-8)||' OR purpose=''v28_glare_source_confirmation'')';
END $$;

CREATE FUNCTION photo.v28_glare_source_document_scope(p_tenant uuid,p_run uuid,p_owner uuid,
 p_context jsonb,p_sha text,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE proof jsonb;body text;project_id uuid;actor_id uuid;auth_session uuid;shoot_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_tenant IS NULL OR p_run IS NULL OR p_owner IS NULL
  OR jsonb_typeof(p_context) IS DISTINCT FROM 'object'
  OR p_context->>'schema_version' IS DISTINCT FROM '1'
  OR p_context->>'tenant_id' IS DISTINCT FROM p_tenant::text
  OR p_context->>'run_id' IS DISTINCT FROM p_run::text
  OR p_context->'params'->>'mode' IS DISTINCT FROM 'generative'
  OR p_context->'params'->'confirmation'->>'artifact_id' IS DISTINCT FROM p_owner::text
  OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 project_id:=(p_context->>'project_id')::uuid;actor_id:=(p_context->>'actor_id')::uuid;
 auth_session:=(p_context->>'auth_session_id')::uuid;shoot_id:=(p_context->>'shoot_session_id')::uuid;
 body:=p_context->>'proof_jcs';
 IF body IS NULL OR octet_length(body)<>p_bytes
  OR encode(public.digest(convert_to(body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_sha
  OR p_context->'params'->'confirmation'->>'sha256' IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 PERFORM r.id FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.created_by=actor_id AND r.kind='v2'
  AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
  AND w.project_id=project_id AND w.workflow_type='preview' AND w.renderer_version='v2.2' FOR SHARE OF r,w;
 IF NOT FOUND THEN RETURN false;END IF;
 proof:=photo.build_v28_glare_source_confirmation(p_tenant,project_id,shoot_id,actor_id,auth_session,
  p_context->'params',(p_context->>'source_observation_id')::uuid);
 RETURN proof IS NOT NULL AND proof=body::jsonb;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_glare_source_document_scope(uuid,uuid,uuid,jsonb,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_glare_source_document_scope(uuid,uuid,uuid,jsonb,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_glare_source_document_scope(uuid,uuid,uuid,jsonb,text,bigint) TO photo_scheduler;

-- Reuse only the established quota/object-fence machinery, with a distinct
-- publication purpose, context and authority. Never clone a donor lease gate.
DO $$ DECLARE definition text;spec record;BEGIN
 FOR spec IN SELECT * FROM (VALUES
  ('photo.reserve_v28_donor_document(uuid,uuid,uuid,jsonb,text,bigint,text)'::regprocedure),
  ('photo.complete_v28_donor_document(uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,text)'::regprocedure)) f(fn)
 LOOP
  definition:=pg_get_functiondef(spec.fn);
  IF position('v28_donor_document_scope' in definition)=0 OR position('v28_donor_issuer_context' in definition)=0 THEN
   RAISE EXCEPTION 'GLARE_SOURCE_PUBLICATION_TEMPLATE_CHANGED';END IF;
  definition:=replace(definition,'reserve_v28_donor_document','reserve_v28_glare_source_document');
  definition:=replace(definition,'complete_v28_donor_document','complete_v28_glare_source_document');
  definition:=replace(definition,'v28_donor_document_scope','v28_glare_source_document_scope');
  definition:=replace(definition,'v28_donor_issuer_context','v28_glare_source_issuer_context');
  definition:=replace(definition,'v28_donor_confirmation','v28_glare_source_confirmation');
  EXECUTE definition;
 END LOOP;
END $$;
ALTER FUNCTION photo.reserve_v28_glare_source_document(uuid,uuid,uuid,jsonb,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v28_glare_source_document(uuid,uuid,uuid,jsonb,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v28_glare_source_document(uuid,uuid,uuid,jsonb,text,bigint,text) TO photo_scheduler;
ALTER FUNCTION photo.complete_v28_glare_source_document(uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v28_glare_source_document(uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v28_glare_source_document(uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,text) TO photo_scheduler;

ALTER FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)
 RENAME TO complete_backend_document_pre_glare_source;
REVOKE ALL ON FUNCTION photo.complete_backend_document_pre_glare_source(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) FROM photo_scheduler;
CREATE FUNCTION photo.complete_backend_document(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_purpose='v28_glare_source_confirmation' THEN RETURN false;END IF;
 RETURN photo.complete_backend_document_pre_glare_source(p_tenant,p_run,p_owner,p_purpose,p_artifact,p_token,p_sha,p_bytes,p_bucket,p_version);
END $$;
ALTER FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) TO photo_scheduler;

DO $$ DECLARE definition text;BEGIN
 definition:=pg_get_functiondef('photo.v28_donor_confirmation_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text)'::regprocedure);
 IF position('v28_donor_document_scope' in definition)=0 OR position('v28_donor_issuer_context' in definition)=0 THEN
  RAISE EXCEPTION 'GLARE_SOURCE_READ_TEMPLATE_CHANGED';END IF;
 definition:=replace(definition,'v28_donor_confirmation_scope','v28_glare_source_confirmation_scope');
 definition:=replace(definition,'v28_donor_document_scope','v28_glare_source_document_scope');
 definition:=replace(definition,'v28_donor_issuer_context','v28_glare_source_issuer_context');
 definition:=replace(definition,'v28_donor_confirmation','v28_glare_source_confirmation');
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.v28_glare_source_confirmation_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_glare_source_confirmation_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_glare_source_confirmation_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text) TO photo_api;

ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text)
 RENAME TO v2_draft_artifact_scope_pre_glare_source;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope_pre_glare_source(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM photo_api;
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_contract='DonorConfirmation' AND EXISTS(SELECT 1 FROM photo.backend_documents d
  WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact AND d.purpose='v28_glare_source_confirmation') THEN
  RETURN photo.v28_glare_source_confirmation_scope(p_tenant,p_project,p_version,p_artifact,p_sha,p_actor,p_session,p_parent,p_parent_sha);
 END IF;
 RETURN photo.v2_draft_artifact_scope_pre_glare_source(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,p_parent,p_parent_sha);
END $$;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;

-- Source-only variants preserve the original immutable draft, physical input,
-- qualification and transport guards. Only donor-specific authority is replaced.
DO $$ DECLARE definition text;old text;replacement text;BEGIN
 definition:=pg_get_functiondef('photo.begin_v28_restoration_draft(text,text,text,bigint)'::regprocedure);
 definition:=replace(definition,'begin_v28_restoration_draft','begin_v28_glare_source_draft');
 old:=$a$OR source_observation IS NULL OR donor_observation IS NULL
 OR source_observation=donor_observation$a$;
 IF (length(definition)-length(replace(definition,old,'')))/length(old)<>1 THEN RAISE EXCEPTION 'GLARE_SOURCE_BEGIN_OBSERVATION_ANCHOR';END IF;
 definition:=replace(definition,old,$a$OR source_observation IS NULL OR donor_observation IS NOT NULL
 OR proposal->>'mode' IS DISTINCT FROM 'generative'
 OR proposal->'donor' IS DISTINCT FROM 'null'::jsonb
 OR proposal->'donor_faces' IS DISTINCT FROM 'null'::jsonb$a$);
 old:=$a$OR (intent->>'donor_asset_version_id')::uuid IS NULL
 OR intent->>'base_asset_version_id' = intent->>'donor_asset_version_id'$a$;
 IF (length(definition)-length(replace(definition,old,'')))/length(old)<>1 THEN RAISE EXCEPTION 'GLARE_SOURCE_BEGIN_VERSION_ANCHOR';END IF;
 definition:=replace(definition,old,$a$OR intent->'donor_asset_version_id' IS DISTINCT FROM 'null'::jsonb$a$);
 EXECUTE definition;

 definition:=pg_get_functiondef('photo.v28_restoration_donor_params_request_scope(uuid,uuid,jsonb)'::regprocedure);
 definition:=replace(definition,'v28_restoration_donor_params_request_scope','v28_glare_source_params_request_scope');
 definition:=replace(definition,$a$body->>'mode' IS DISTINCT FROM 'donor'$a$,$a$body->>'mode' IS DISTINCT FROM 'generative'$a$);
 old:=$a$jsonb_typeof(body->'donor') IS DISTINCT FROM 'object'$a$;
 IF position(old in definition)=0 THEN RAISE EXCEPTION 'GLARE_SOURCE_PARAMS_MODE_ANCHOR';END IF;
 definition:=replace(definition,old,$a$body->'donor' IS DISTINCT FROM 'null'::jsonb OR body->'donor_faces' IS DISTINCT FROM 'null'::jsonb$a$);
 definition:=replace(definition,'v28_donor_confirmation','v28_glare_source_confirmation');
 definition:=replace(definition,'v28_donor_issuer_context','v28_glare_source_issuer_context');
 definition:=replace(definition,$a$c->'params'->'eyes_mask'$a$,$a$c->'params'->'glasses_mask'$a$);
 definition:=replace(definition,$a$c->'params'->'base' IS DISTINCT FROM body->'base'$a$,$a$((c->'params')-'confirmation') IS DISTINCT FROM (body-'confirmation') OR c->'params'->'base' IS DISTINCT FROM body->'base'$a$);
 old:=$a$photo.lock_v28_donor_session(p_tenant,w.project_id,
  (c->>'shoot_session_id')::uuid,r.created_by,
  (p_request->>'auth_session_id')::uuid,proof,
  body->'source_faces',body->'donor_faces')$a$;
 replacement:=$a$photo.lock_v28_glare_source_session(p_tenant,w.project_id,
  (c->>'shoot_session_id')::uuid,r.created_by,
  (p_request->>'auth_session_id')::uuid,proof,body->'source_faces')$a$;
 IF position(old in definition)=0 THEN RAISE EXCEPTION 'GLARE_SOURCE_PARAMS_AUTHORITY_ANCHOR';END IF;
 definition:=replace(definition,old,replacement);EXECUTE definition;

 definition:=pg_get_functiondef('photo.read_v28_restoration_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)'::regprocedure);
 definition:=replace(definition,'read_v28_restoration_admission_ticket','read_v28_glare_source_admission_ticket');
 old:=$a$OR p_source_observation IS NULL OR p_donor_observation IS NULL
 OR p_source_observation=p_donor_observation$a$;
 IF position(old in definition)=0 THEN RAISE EXCEPTION 'GLARE_SOURCE_TICKET_OBSERVATION_ANCHOR';END IF;
 definition:=replace(definition,old,$a$OR p_source_observation IS NULL OR p_donor_observation IS NOT NULL$a$);
 definition:=replace(definition,$a$body->>'mode' IS DISTINCT FROM 'donor'$a$,$a$body->>'mode' IS DISTINCT FROM 'generative'
 OR body->'donor' IS DISTINCT FROM 'null'::jsonb OR body->'donor_faces' IS DISTINCT FROM 'null'::jsonb$a$);
 definition:=replace(definition,'v28_donor_confirmation','v28_glare_source_confirmation');
 definition:=replace(definition,'v28_donor_issuer_context','v28_glare_source_issuer_context');
 definition:=replace(definition,$a$c->'params'->'eyes_mask'$a$,$a$c->'params'->'glasses_mask'$a$);
 definition:=replace(definition,$a$c->'params'->'base' IS DISTINCT FROM body->'base'$a$,$a$((c->'params')-'confirmation') IS DISTINCT FROM (body-'confirmation') OR c->'params'->'base' IS DISTINCT FROM body->'base'$a$);
 old:=$a$photo.lock_v28_donor_session(p_tenant,w.project_id,
   (c->>'shoot_session_id')::uuid,p_actor,p_session,proof::jsonb,
   body->'source_faces',body->'donor_faces')$a$;
 IF position(old in definition)=0 THEN RAISE EXCEPTION 'GLARE_SOURCE_TICKET_AUTHORITY_ANCHOR';END IF;
 definition:=replace(definition,old,$a$photo.lock_v28_glare_source_session(p_tenant,w.project_id,
   (c->>'shoot_session_id')::uuid,p_actor,p_session,proof::jsonb,body->'source_faces')$a$);EXECUTE definition;

 definition:=pg_get_functiondef('photo.v28_glare_donor_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text)'::regprocedure);
 definition:=replace(definition,'v28_glare_donor_source_ready','v28_glare_generative_source_ready');
 definition:=replace(definition,$a$body->>'mode' IS DISTINCT FROM 'donor'$a$,$a$body->>'mode' IS DISTINCT FROM 'generative'$a$);
 definition:=replace(definition,$a$jsonb_typeof(body->'donor') IS DISTINCT FROM 'object'$a$,$a$body->'donor' IS DISTINCT FROM 'null'::jsonb$a$);
 definition:=replace(definition,$a$jsonb_typeof(body->'donor_faces') IS DISTINCT FROM 'object'$a$,$a$body->'donor_faces' IS DISTINCT FROM 'null'::jsonb$a$);
 definition:=replace(definition,'v28_donor_confirmation','v28_glare_source_confirmation');
 definition:=replace(definition,'v28_donor_issuer_context','v28_glare_source_issuer_context');
 definition:=replace(definition,$a$c->'params'->'eyes_mask'$a$,$a$c->'params'->'glasses_mask'$a$);
 definition:=replace(definition,$a$c->'params'->'base' IS DISTINCT FROM body->'base'$a$,$a$((c->'params')-'confirmation') IS DISTINCT FROM (body-'confirmation') OR c->'params'->'base' IS DISTINCT FROM body->'base'$a$);
 old:=$a$photo.lock_v28_donor_session(p_tenant,project_id,
  (c->>'shoot_session_id')::uuid,actor_id,auth_session,
  proof,body->'source_faces',body->'donor_faces')$a$;
 IF position(old in definition)=0 THEN RAISE EXCEPTION 'GLARE_SOURCE_WORKER_AUTHORITY_ANCHOR';END IF;
 definition:=replace(definition,old,$a$photo.lock_v28_glare_source_session(p_tenant,project_id,
  (c->>'shoot_session_id')::uuid,actor_id,auth_session,proof,body->'source_faces')$a$);EXECUTE definition;
END $$;
ALTER FUNCTION photo.begin_v28_glare_source_draft(text,text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v28_glare_source_draft(text,text,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v28_glare_source_draft(text,text,text,bigint) TO photo_scheduler;
ALTER FUNCTION photo.v28_glare_source_params_request_scope(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_glare_source_params_request_scope(uuid,uuid,jsonb) FROM PUBLIC;
ALTER FUNCTION photo.read_v28_glare_source_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_glare_source_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_glare_source_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid) TO photo_scheduler;
ALTER FUNCTION photo.v28_glare_generative_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_glare_generative_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_glare_generative_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text) TO photo_worker;

ALTER FUNCTION photo.v28_restoration_params_request_scope(uuid,uuid,jsonb) RENAME TO v28_restoration_params_request_scope_pre_source;
CREATE FUNCTION photo.v28_restoration_params_request_scope(p_tenant uuid,p_run uuid,p_request jsonb)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF (p_request->>'params_jcs')::jsonb->>'mode'='generative' THEN
  RETURN photo.v28_glare_source_params_request_scope(p_tenant,p_run,p_request);END IF;
 RETURN photo.v28_restoration_params_request_scope_pre_source(p_tenant,p_run,p_request);
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_restoration_params_request_scope(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_params_request_scope(uuid,uuid,jsonb) FROM PUBLIC;

-- Preserve the donor branch and all common graph gates; source-only mode gets
-- its own issued proof and exact effective Params comparison.
DO $$ DECLARE definition text;anchor text;BEGIN
 definition:=pg_get_functiondef('photo.seal_v28_restoration_graph(uuid,uuid,uuid,uuid)'::regprocedure);
 anchor:=$a$IF task->>'operation'='portrait.glare' THEN$a$;
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'GLARE_SOURCE_SEAL_ANCHOR';END IF;
 definition:=replace(definition,anchor,$a$IF task->>'operation'='portrait.glare' AND body->>'mode'='generative' THEN
  IF photo.v28_glare_source_confirmation_scope(p_tenant,project_id,(body->'base'->>'asset_version_id')::uuid,
   (body->'confirmation'->>'artifact_id')::uuid,body->'confirmation'->>'sha256',p_actor,p_session,
   (body->'base'->'source'->>'artifact_id')::uuid,body->'base'->'source'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
  SELECT d.v28_glare_source_issuer_context INTO issuer FROM photo.backend_documents d
  WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.artifact_id=(body->'confirmation'->>'artifact_id')::uuid
   AND d.sha256=body->'confirmation'->>'sha256' AND d.purpose='v28_glare_source_confirmation' AND d.state='ready' FOR SHARE;
  IF NOT FOUND OR ((issuer->'params')-'confirmation') IS DISTINCT FROM (body-'confirmation') THEN RETURN false;END IF;
 ELSIF task->>'operation'='portrait.glare' THEN$a$);EXECUTE definition;
END $$;

DO $$ DECLARE definition text;BEGIN
 definition:=pg_get_functiondef('photo.v28_glare_selected_authority(uuid,uuid,uuid,uuid)'::regprocedure);
 IF position($a$doc.purpose='v28_donor_confirmation'$a$ in definition)=0
  OR position($a$c:=d.v28_donor_issuer_context$a$ in definition)=0 THEN RAISE EXCEPTION 'GLARE_SOURCE_SELECTED_ANCHOR';END IF;
 definition:=replace(definition,$a$doc.purpose='v28_donor_confirmation'$a$,
  $a$doc.purpose=CASE body->>'mode' WHEN 'generative' THEN 'v28_glare_source_confirmation' ELSE 'v28_donor_confirmation' END$a$);
 definition:=replace(definition,$a$artifact.output_role='v28_donor_confirmation'$a$,$a$artifact.output_role=doc.purpose$a$);
 definition:=replace(definition,$a$c:=d.v28_donor_issuer_context$a$,
  $a$c:=CASE body->>'mode' WHEN 'generative' THEN d.v28_glare_source_issuer_context ELSE d.v28_donor_issuer_context END$a$);
 definition:=replace(definition,$a$c->'params'->'eyes_mask' IS DISTINCT FROM body->'glasses_mask'$a$,
  $a$(CASE body->>'mode' WHEN 'generative' THEN c->'params'->'glasses_mask' ELSE c->'params'->'eyes_mask' END) IS DISTINCT FROM body->'glasses_mask'$a$);
 definition:=replace(definition,$a$IF body->>'mode'='generative' THEN$a$,
  $a$IF body->>'mode'='generative' THEN
   IF ((c->'params')-'confirmation') IS DISTINCT FROM (body-'confirmation')
    OR photo.lock_v28_glare_source_session(p_tenant,w.project_id,(c->>'shoot_session_id')::uuid,
     p_actor,p_session,proof::jsonb,body->'source_faces') IS DISTINCT FROM true THEN RETURN false;END IF;$a$);
 definition:=replace(definition,$a$IF body->>'mode' NOT IN('donor','generative')$a$,
  $a$IF coalesce(body->>'mode','') NOT IN('donor','generative')$a$);
 EXECUTE definition;
END $$;

-- Python's canonical Params retain explicit nullable donor fields. The old
-- generative inventory guard required their absence and rejected valid bytes.
DO $$ DECLARE definition text;anchor text;BEGIN
 definition:=pg_get_functiondef('photo.v28_restoration_inventory_digest_ready(uuid,uuid,text,text,text,jsonb,jsonb,jsonb)'::regprocedure);
 anchor:=$a$(NOT donor_mode AND ((p_body ? 'donor') OR (p_body ? 'donor_faces')))$a$;
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'GLARE_SOURCE_INVENTORY_NULL_ANCHOR';END IF;
 definition:=replace(definition,anchor,$a$(NOT donor_mode AND (p_body->'donor' IS DISTINCT FROM 'null'::jsonb OR p_body->'donor_faces' IS DISTINCT FROM 'null'::jsonb))$a$);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
