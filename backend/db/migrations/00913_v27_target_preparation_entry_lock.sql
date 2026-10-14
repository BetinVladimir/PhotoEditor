-- +goose Up
-- Target scope authorization only. No task, quota, output or admission authority.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v27_target_preparation_entry(p_tenant uuid,p_detector uuid,p_evidence jsonb,p_manifest_sha text,p_ordinal integer,p_operation text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;native jsonb;snapshot jsonb;manifest jsonb;entry jsonb;binding jsonb;namespace uuid;parent uuid;stages jsonb:='[]';op text;deps jsonb;expected jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_manifest_sha IS NULL OR p_manifest_sha !~ '^[0-9a-f]{64}$'
 OR p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999
 OR p_operation IS NULL OR p_operation NOT IN('mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose') THEN RETURN NULL;END IF;
 IF photo.lock_semantic_native_admission_anchor(p_tenant,p_detector,p_evidence) IS NOT TRUE
 OR p_evidence->>'operation' IS DISTINCT FROM 'face.detect' THEN RETURN NULL;END IF;
 parent:=(p_evidence->>'parent_run_id')::uuid;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=parent FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.status NOT IN('queued','running','succeeded')
 OR r.created_by::text IS DISTINCT FROM p_evidence->>'actor_id' OR r.v27_initial_manual_selection IS NOT NULL
 OR r.config_snapshot IS DISTINCT FROM (p_evidence->>'config_text')::jsonb
 OR r.config_hash IS DISTINCT FROM encode(public.digest(convert_to(p_evidence->>'config_text','UTF8'),'sha256'),'hex')
 OR r.v27_target_manifest IS NULL OR r.v27_target_manifest_sha IS DISTINCT FROM p_manifest_sha
 OR encode(public.digest(convert_to(r.v27_target_manifest,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_manifest_sha THEN RETURN NULL;END IF;
 native:=r.config_snapshot->'v27_detector_prepare';snapshot:=(p_evidence->'anchor'->>'snapshot_text')::jsonb;
 manifest:=r.v27_target_manifest::jsonb;
 IF r.input_snapshot_id::text IS DISTINCT FROM snapshot->>'snapshot_id'
 OR (manifest-'entries') IS DISTINCT FROM jsonb_build_object('schema_version',1,'tenant_id',p_tenant,'project_id',p_evidence->'project_id','actor_id',r.created_by,
 'session_id',native->'auth_session_id','parent_run_id',parent,'request_sha',native->'request_sha','intent',native->'intent','snapshot',native->'snapshot')
 OR jsonb_typeof(manifest->'entries') IS DISTINCT FROM 'array'
 OR jsonb_array_length(manifest->'entries')<>jsonb_array_length(snapshot->'items')
 OR p_ordinal>=jsonb_array_length(snapshot->'items') THEN RETURN NULL;END IF;
 binding:=snapshot->'items'->p_ordinal->'binding';
 IF binding=native->'source' THEN RETURN NULL;END IF; -- reference reuse requires original producer path
 namespace:=photo.semantic_uuid_v5(parent,'semantic.target.prepare:1:'||(native->'snapshot'->>'sha256')||':'||p_ordinal||':'||
 encode(public.digest(convert_to(photo.v27_target_manifest_json_text(binding),'UTF8'),'sha256'),'hex'));
 FOREACH op IN ARRAY ARRAY['mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose'] LOOP
  deps:=CASE op
   WHEN 'face.landmarks' THEN jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:face.detect'))
   WHEN 'face.parse' THEN jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:face.detect'))
   WHEN 'mask.face_skin' THEN jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:face.parse'))
   WHEN 'defects.propose' THEN jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:mask.person'),photo.semantic_uuid_v5(namespace,'stage:face.landmarks'),photo.semantic_uuid_v5(namespace,'stage:mask.face_skin')) ELSE '[]'::jsonb END;
  stages:=stages||jsonb_build_array(jsonb_build_object('id',photo.semantic_uuid_v5(namespace,'stage:'||op),'operation',op,'operation_version','1','requires',deps));
 END LOOP;
 expected:=jsonb_build_object('ordinal',p_ordinal,'binding',binding,'namespace',namespace,'reuse_reference',false,'stages',stages);
 entry:=manifest->'entries'->p_ordinal;
 IF entry IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 RETURN entry;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.lock_v27_target_preparation_entry(uuid,uuid,jsonb,text,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v27_target_preparation_entry(uuid,uuid,jsonb,text,integer,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v27_target_preparation_entry(uuid,uuid,jsonb,text,integer,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
