-- +goose Up
-- Repeat full target inventory, exact published intent and snapshot/recipe heads.
-- No seal or queue authority is granted by this component.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_admission_anchor(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE anchor jsonb;intent jsonb;snapshot jsonb;native jsonb;project uuid;actor uuid;session_id uuid;parent uuid;row record;item jsonb;matched integer:=0;expected integer;intent_text text;snapshot_text text;operation text;config jsonb;
BEGIN
 IF jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432
 OR p_evidence->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_evidence->>'tenant_id' IS DISTINCT FROM p_tenant::text OR p_evidence->>'run_id' IS DISTINCT FROM p_run::text THEN RETURN false;END IF;
 operation:=p_evidence->>'operation';
 IF operation IS NULL OR operation NOT IN ('face.detect','mask.person') THEN RETURN false;END IF;
 anchor:=p_evidence->'anchor';
 IF jsonb_typeof(anchor) IS DISTINCT FROM 'object' OR (anchor-'intent_text'-'snapshot_text')<>'{}'
 OR jsonb_typeof(anchor->'intent_text') IS DISTINCT FROM 'string' OR jsonb_typeof(anchor->'snapshot_text') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 intent_text:=anchor->>'intent_text';snapshot_text:=anchor->>'snapshot_text';
 IF octet_length(intent_text) NOT BETWEEN 2 AND 1048576 OR octet_length(snapshot_text) NOT BETWEEN 2 AND 8388608 THEN RETURN false;END IF;
 intent:=intent_text::jsonb;snapshot:=snapshot_text::jsonb;
 project:=(p_evidence->>'project_id')::uuid;actor:=(p_evidence->>'actor_id')::uuid;session_id:=(p_evidence->>'session_id')::uuid;parent:=(p_evidence->>'parent_run_id')::uuid;
 config:=(p_evidence->>'config_text')::jsonb;
 native:=config->CASE operation WHEN 'face.detect' THEN 'v27_detector_prepare' ELSE 'v27_person_prepare' END;
 IF jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR native->>'parent_run_id' IS DISTINCT FROM parent::text OR native->>'auth_session_id' IS DISTINCT FROM session_id::text
 OR native->>'request_sha' IS DISTINCT FROM p_evidence->>'request_sha' OR config->>'project_id' IS DISTINCT FROM project::text
 OR native->'source' IS DISTINCT FROM p_evidence->'binding' THEN RETURN false;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5(parent,'semantic.'||operation||':reference')
 OR intent->'schema_version' IS DISTINCT FROM '1'::jsonb OR intent->'reference' IS DISTINCT FROM native->'source'
 OR snapshot->>'snapshot_id' IS DISTINCT FROM intent->>'target_snapshot_id'
 OR encode(public.digest(convert_to(intent_text,'UTF8'),'sha256'),'hex') IS DISTINCT FROM native->'intent'->>'sha256'
 OR encode(public.digest(convert_to(snapshot_text,'UTF8'),'sha256'),'hex') IS DISTINCT FROM native->'snapshot'->>'sha256'
 OR encode(public.digest(convert_to('{"actor_id":"'||actor::text||'","intent":'||intent_text||',"tenant_id":"'||p_tenant::text||'"}','UTF8'),'sha256'),'hex') IS DISTINCT FROM p_evidence->>'request_sha'
 OR jsonb_typeof(snapshot->'items') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 expected:=jsonb_array_length(snapshot->'items');IF expected NOT BETWEEN 1 AND 5000 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 FOR row IN SELECT * FROM photo.lock_semantic_proposal_inventory(p_tenant,actor,session_id,
 (intent->'reference'->>'asset_version_id')::uuid,(intent->'reference'->'source'->>'artifact_id')::uuid,intent->'reference'->'source'->>'sha256',intent->'reference'->>'canvas_hash',
 (intent->'reference_mask'->>'artifact_id')::uuid,intent->'reference_mask'->>'sha256',(intent->>'target_snapshot_id')::uuid) LOOP
  item:=snapshot->'items'->matched;
  IF row.project_id IS DISTINCT FROM project OR row.item_count<>expected OR row.ordinal<>matched
  OR item->'ordinal' IS DISTINCT FROM to_jsonb(matched)
  OR item->'binding' IS DISTINCT FROM jsonb_build_object('asset_version_id',row.asset_version_id,'source',jsonb_build_object('artifact_id',row.source_id,'sha256',row.source_sha),'canvas_hash',row.canvas_hash) THEN RETURN false;END IF;
  matched:=matched+1;
 END LOOP;
 IF matched<>expected THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_document(p_tenant,project,actor,session_id,parent,(intent->'reference'->>'asset_version_id')::uuid,
 (native->'intent'->>'artifact_id')::uuid,native->'intent'->>'sha256',octet_length(intent_text)) IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.lock_semantic_proposal_snapshot(p_tenant,project,actor,session_id,parent,(intent->>'target_snapshot_id')::uuid,
 (native->'snapshot'->>'artifact_id')::uuid,native->'snapshot'->>'sha256',snapshot_text);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_admission_anchor(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_admission_anchor(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_admission_anchor(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
