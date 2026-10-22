-- +goose Up
-- Ordinary segmentation/manual masks do not require a new consent. Preserve
-- only the dependencies actually recorded by an existing person producer.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_recorded_subject_consents_live(p_tenant uuid,p_subject jsonb)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE root photo.artifacts%ROWTYPE;producer photo.tasks%ROWTYPE;producer_run uuid;
BEGIN
 SELECT * INTO root FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id=(p_subject->>'artifact_id')::uuid AND sha256=p_subject->>'sha256';
 IF NOT FOUND THEN RETURN false;END IF;
 IF root.output_role<>'person_mask_ref' THEN RETURN true;END IF;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=root.created_by_task_id;
 IF NOT FOUND OR producer.operation<>'mask.person' OR producer.operation_version<>'1'
  OR producer.state<>'succeeded' OR producer.cancel_requested
  OR root.produced_attempt IS DISTINCT FROM producer.attempt
  OR root.produced_fence IS DISTINCT FROM producer.fencing_token THEN RETURN false;END IF;
 producer_run:=producer.run_id;
 -- Completed legacy Apply producers need no artificial graph-sealed flag.
 -- A current draft or open learning expansion can still add dependencies.
 PERFORM run.id FROM photo.runs run WHERE run.tenant_id=p_tenant AND run.id=producer.run_id
  AND NOT run.cancel_requested AND run.status<>'draft'
  AND NOT EXISTS(SELECT 1 FROM photo.v2_workflows workflow WHERE workflow.tenant_id=p_tenant
   AND workflow.run_id=run.id AND workflow.workflow_type='learning'
   AND run.status='running' AND NOT run.graph_sealed) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 -- Re-read the producer only after its run is stable (run→task→artifact).
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=root.created_by_task_id FOR SHARE;
 IF NOT FOUND OR producer.run_id IS DISTINCT FROM producer_run OR producer.operation<>'mask.person' OR producer.operation_version<>'1'
  OR producer.state<>'succeeded' OR producer.cancel_requested
  OR root.produced_attempt IS DISTINCT FROM producer.attempt
  OR root.produced_fence IS DISTINCT FROM producer.fencing_token THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=root.id
  AND sha256=p_subject->>'sha256' AND created_by_task_id=producer.id
  AND produced_attempt=producer.attempt AND produced_fence=producer.fencing_token FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM workflow.id FROM photo.v2_workflows workflow WHERE workflow.tenant_id=p_tenant
  AND workflow.run_id=producer.run_id ORDER BY workflow.id FOR SHARE;
 -- Link and grant rows stay stable through the enclosing authority transaction.
 PERFORM link.id FROM photo.workflow_consent_links link JOIN photo.v2_workflows workflow
  ON workflow.tenant_id=link.tenant_id AND workflow.id=link.workflow_id
  WHERE workflow.tenant_id=p_tenant AND workflow.run_id=producer.run_id ORDER BY link.id FOR SHARE OF link;
 PERFORM grant_row.id FROM photo.consent_grants grant_row
  WHERE grant_row.tenant_id=p_tenant AND grant_row.id IN(
   SELECT link.consent_id FROM photo.workflow_consent_links link JOIN photo.v2_workflows workflow
    ON workflow.tenant_id=link.tenant_id AND workflow.id=link.workflow_id
   WHERE workflow.tenant_id=p_tenant AND workflow.run_id=producer.run_id)
  ORDER BY grant_row.id FOR SHARE;
 RETURN NOT EXISTS(SELECT 1 FROM photo.v2_workflows workflow
  JOIN photo.workflow_consent_links link ON link.tenant_id=workflow.tenant_id AND link.workflow_id=workflow.id
  LEFT JOIN photo.consent_grants grant_row ON grant_row.tenant_id=link.tenant_id
   AND grant_row.project_id=link.project_id AND grant_row.id=link.consent_id
  WHERE workflow.tenant_id=p_tenant AND workflow.run_id=producer.run_id
   AND (link.project_id IS DISTINCT FROM workflow.project_id OR grant_row.id IS NULL
    OR grant_row.status IS DISTINCT FROM 'granted' OR grant_row.epoch IS DISTINCT FROM link.consent_epoch));
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_relight_recorded_subject_consents_live(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_recorded_subject_consents_live(uuid,jsonb) FROM PUBLIC;

-- Serialize inserting a dependency with sealing its workflow run. Keep the
-- existing draft/learning policy and grant checks unchanged.
DO $$ DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.guard_v22_workflow_consent()'::regprocedure);
 needle:=' IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.runs r';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1
  THEN RAISE EXCEPTION 'RELIGHT_CONSENT_INSERT_LOCK_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,
  ' PERFORM r.id FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id WHERE w.tenant_id=NEW.tenant_id AND w.project_id=NEW.project_id AND w.id=NEW.workflow_id FOR SHARE OF r;'||needle);
END $$;

DO $$ DECLARE signature text;definition text;needle text;replacement text;
BEGIN
 FOR signature,needle,replacement IN SELECT * FROM (VALUES
 ('photo.v28_relight_intent_person_origin(uuid,uuid,text)',
  'params:=(request->>''params_jcs'')::jsonb;inputs:=(request->>''inputs_jcs'')::jsonb;',
  'params:=(request->>''params_jcs'')::jsonb;inputs:=(request->>''inputs_jcs'')::jsonb; IF photo.v28_relight_recorded_subject_consents_live(p_tenant,params->''subject'') IS DISTINCT FROM true THEN RETURN NULL;END IF;'),
 ('photo.v28_worker_relight_person_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text)',
  'mask:=p_mask_body::jsonb;',
  'mask:=p_mask_body::jsonb; IF photo.v28_relight_recorded_subject_consents_live(p_tenant,p_body::jsonb->''subject'') IS DISTINCT FROM true THEN RETURN NULL;END IF;'),
 ('photo.v28_relight_adoption_person_origin(uuid,uuid,integer,bigint,uuid,text,text)',
  'mask:=p_mask_body::jsonb;',
  'mask:=p_mask_body::jsonb; IF photo.v28_relight_recorded_subject_consents_live(p_tenant,p_body::jsonb->''subject'') IS DISTINCT FROM true THEN RETURN NULL;END IF;'),
 ('photo.v28_relight_review_authority(uuid,uuid,uuid,uuid)',
  'body:=v.relight_params_jcs::jsonb;',
  'body:=v.relight_params_jcs::jsonb; IF photo.v28_relight_recorded_subject_consents_live(p_tenant,body->''subject'') IS DISTINCT FROM true THEN RETURN false;END IF;'),
 ('photo.apply_v28_relight_version(uuid,uuid,uuid,uuid,text,text,text)',
  'body:=v.relight_params_jcs::jsonb;',
  'body:=v.relight_params_jcs::jsonb; IF photo.v28_relight_recorded_subject_consents_live(p_tenant,body->''subject'') IS DISTINCT FROM true THEN RETURN NULL;END IF;')
 ) patches(signature,needle,replacement) LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1
   THEN RAISE EXCEPTION 'RELIGHT_RECORDED_CONSENT_PATCH_MISMATCH: %',signature;END IF;
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
