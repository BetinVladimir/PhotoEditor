-- +goose Up
-- Completed workers do not have running leases. Reuse the independently
-- authenticated issuer checks, but never manufacture a decided human review.
-- +goose StatementBegin
DO $$ DECLARE definition text;old text;replacement text;BEGIN
 definition:=pg_get_functiondef('photo.v28_glare_selected_authority(uuid,uuid,uuid,uuid)'::regprocedure);
 old:=$a$photo.v28_glare_selected_authority(p_tenant uuid, p_review uuid, p_actor uuid, p_session uuid)$a$;
 IF position(old in definition)=0 THEN RAISE EXCEPTION 'GLARE_COMPLETED_SIGNATURE_DRIFT';END IF;
 definition:=replace(definition,old,'photo.v28_glare_completed_source_ready(p_tenant uuid, p_task uuid, p_result uuid)');
 old:='DECLARE v photo.reviews%ROWTYPE;';
 IF (length(definition)-length(replace(definition,old,'')))/length(old)<>1 THEN RAISE EXCEPTION 'GLARE_COMPLETED_DECLARE_DRIFT';END IF;
 definition:=replace(definition,old,'DECLARE p_actor uuid;p_session uuid;');
 old:=substring(definition FROM position(' IF p_tenant IS DISTINCT FROM' in definition)
  FOR position(' SELECT * INTO w FROM photo.v2_workflows' in definition)-position(' IF p_tenant IS DISTINCT FROM' in definition));
 IF old IS NULL OR old='' THEN RAISE EXCEPTION 'GLARE_COMPLETED_SCOPE_DRIFT';END IF;
 replacement:=$a$ IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_task IS NULL OR p_result IS NULL THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id AND kind='v2'
  AND status IN('running','waiting_review') AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=r.id
  AND operation='portrait.glare' AND operation_version='1' AND state='succeeded' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR t.canonical_result_id IS DISTINCT FROM p_result THEN RETURN false;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=p_result
  AND attempt=t.attempt AND fencing_token=t.fencing_token AND review_required FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 p_actor:=r.created_by;p_session:=r.v28_restoration_session_id;
$a$;
 definition:=replace(definition,old,replacement);
 old:=$a$ SELECT * INTO selected FROM photo.get_v28_restoration_review_selection(p_tenant,p_actor,p_session,p_review);
 IF NOT FOUND OR selected.route NOT IN('candidate','base') THEN RETURN false;END IF;
$a$;
 IF position(old in definition)=0 THEN RAISE EXCEPTION 'GLARE_COMPLETED_SELECTION_DRIFT';END IF;
 definition:=replace(definition,old,'');
 old:=$a$ plan:=r.v28_restoration_descriptor_jcs::jsonb;body:=plan->'params';pin:=t.execution_profile->'restoration_qualification';$a$;
 IF position(old in definition)=0 THEN RAISE EXCEPTION 'GLARE_COMPLETED_DESCRIPTOR_DRIFT';END IF;
 definition:=replace(definition,old,old||$a$
 IF plan->'task'->>'node_key' IS DISTINCT FROM t.node_key
 OR plan->'task'->>'operation' IS DISTINCT FROM t.operation
 OR plan->'task'->>'params_artifact_id' IS DISTINCT FROM t.params_artifact_id::text
 OR plan->'task'->>'input_digest' IS DISTINCT FROM t.input_digest
 OR (t.execution_profile IS DISTINCT FROM plan->'task'->'execution_profile' AND
  (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM plan->'task'->'execution_profile')
 OR photo.v28_restoration_inventory_digest_ready(t.id,t.params_artifact_id,plan->'task'->>'params_sha',
  t.operation,t.input_digest,plan->'entries',body,plan->'task'->'execution_profile'->'restoration_qualification') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 PERFORM d.artifact_id FROM photo.backend_documents d JOIN photo.artifacts a
 ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=r.id AND d.purpose='task_params'
 AND d.state='ready' AND d.artifact_id=t.params_artifact_id AND d.sha256=plan->'task'->>'params_sha'
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF d,a;
 IF NOT FOUND THEN RETURN false;END IF;
$a$);
 definition:=replace(definition,'v.base_artifact_id',$a$(body->'base'->'source'->>'artifact_id')::uuid$a$);
 old:=substring(definition FROM position(' PERFORM artifact.id FROM photo.artifacts artifact' in definition)
  FOR position(' SELECT doc.* INTO d FROM photo.backend_documents' in definition)-position(' PERFORM artifact.id FROM photo.artifacts artifact' in definition));
 IF old IS NULL OR old='' THEN RAISE EXCEPTION 'GLARE_COMPLETED_ARTIFACT_DRIFT';END IF;
 definition:=replace(definition,old,'');
 -- The receipt is written only AFTER this authority check. Its physical object
 -- cardinality remains enforced by adopt_v28_restoration_verified itself.
 old:=$a$   OR r.v28_restoration_adoption_receipt#>>'{evidence,operation}' IS DISTINCT FROM 'portrait.glare'
   OR jsonb_array_length(r.v28_restoration_adoption_receipt#>'{evidence,objects}') IS DISTINCT FROM 12$a$;
 IF position(old in definition)=0 THEN RAISE EXCEPTION 'GLARE_COMPLETED_RECEIPT_DRIFT';END IF;
 definition:=replace(definition,old,'');
 -- Bind the authenticated issuer to the original login, not merely an active
 -- replacement login owned by the same photographer.
 definition:=replace(definition,$a$c->>'actor_id' IS DISTINCT FROM r.created_by::text$a$,
  $a$c->>'actor_id' IS DISTINCT FROM r.created_by::text OR c->>'auth_session_id' IS DISTINCT FROM p_session::text$a$);
 definition:=replace(definition,$a$AND artifact.state='ready' AND artifact.verified_at IS NOT NULL AND artifact.bytes=doc.bytes$a$,
  $a$AND artifact.media_type='application/json' AND artifact.state='ready' AND artifact.verified_at IS NOT NULL AND artifact.bytes=doc.bytes$a$);
 IF position('p_review' in definition)>0 OR position('v.candidate_artifact_id' in definition)>0
 THEN RAISE EXCEPTION 'GLARE_COMPLETED_REVIEW_DEPENDENCY';END IF;
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.v28_glare_completed_source_ready(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_glare_completed_source_ready(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_glare_completed_source_ready(uuid,uuid,uuid) TO photo_scheduler;

DO $$ DECLARE definition text;anchor text;BEGIN
 definition:=pg_get_functiondef('photo.adopt_v28_restoration_verified(uuid,uuid,uuid,text)'::regprocedure);
 anchor:=' SELECT project_id INTO project_id FROM photo.v2_workflows';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'GLARE_ADOPTION_AUTHORITY_DRIFT';END IF;
 definition:=replace(definition,anchor,$a$ IF t.operation='portrait.glare' AND photo.v28_glare_completed_source_ready(p_tenant,p_task,p_result) IS DISTINCT FROM true THEN RETURN false;END IF;
$a$||anchor);
 EXECUTE definition;
 definition:=pg_get_functiondef('photo.create_v28_restoration_review(uuid,uuid,uuid)'::regprocedure);
 anchor:=' SELECT * INTO candidate FROM photo.artifacts a';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'GLARE_PUBLICATION_AUTHORITY_DRIFT';END IF;
 definition:=replace(definition,anchor,$a$ IF t.operation='portrait.glare' AND photo.v28_glare_completed_source_ready(p_tenant,p_task,p_result) IS DISTINCT FROM true THEN RETURN NULL;END IF;
$a$||anchor);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02489 is forward-only: completed Glare consent authority must remain installed';END $$;
-- +goose StatementEnd
