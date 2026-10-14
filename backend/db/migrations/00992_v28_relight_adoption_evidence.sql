-- +goose Up
-- Aggregate exact completed dependencies under retained row locks; no result commit.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_adoption_evidence(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_body text,p_depth text,p_subject text,p_guard text,p_subject_root text,p_guard_root text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;qualification jsonb;depth jsonb;subject jsonb;guard jsonb;
BEGIN
 -- Lock declared producers and their runs as well as pixel artifacts: origin
 -- helper EXISTS checks alone must not permit concurrent revocation at commit.
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR SHARE;
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id IN (
  SELECT t.run_id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND (t.id=p_task OR t.id IN (
   SELECT a.created_by_task_id FROM photo.artifacts a JOIN photo.v2_task_inputs i
   ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id WHERE i.tenant_id=p_tenant AND i.task_id=p_task)))
 ORDER BY r.id FOR SHARE;
 PERFORM t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND (t.id=p_task OR t.id IN (
  SELECT a.created_by_task_id FROM photo.artifacts a JOIN photo.v2_task_inputs i
  ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id WHERE i.tenant_id=p_tenant AND i.task_id=p_task))
 ORDER BY t.id FOR SHARE;
 PERFORM a.id FROM photo.artifacts a JOIN photo.v2_task_inputs i
 ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 ORDER BY a.id FOR SHARE OF a;
 source:=photo.v28_relight_adoption_source(p_tenant,p_task,p_attempt,p_fence,p_result,p_body);
 IF source IS NULL THEN RETURN NULL;END IF;
 qualification:=photo.v28_relight_adoption_qualification(p_tenant,p_task,p_attempt,p_fence,p_result,p_body);
 depth:=photo.v28_relight_adoption_depth_origin(p_tenant,p_task,p_attempt,p_fence,p_result,p_body,p_depth);
 IF qualification IS NULL OR depth IS NULL THEN RETURN NULL;END IF;
 IF p_subject::jsonb->>'selector'='person' AND p_subject_root IS NULL THEN
  subject:=photo.v28_relight_adoption_person_origin(p_tenant,p_task,p_attempt,p_fence,p_result,p_body,p_subject);
 ELSIF p_subject::jsonb->>'selector'='manual' AND p_subject_root IS NOT NULL THEN
  subject:=photo.v28_relight_adoption_manual_origin(p_tenant,p_task,p_attempt,p_fence,p_result,p_body,p_subject,p_subject_root,false);
 ELSE RETURN NULL;END IF;
 IF p_guard::jsonb->>'selector' IS DISTINCT FROM 'manual' OR p_guard_root IS NULL THEN RETURN NULL;END IF;
 guard:=photo.v28_relight_adoption_manual_origin(p_tenant,p_task,p_attempt,p_fence,p_result,p_body,p_guard,p_guard_root,true);
 IF subject IS NULL OR guard IS NULL THEN RETURN NULL;END IF;
 IF photo.v28_relight_adoption_source(p_tenant,p_task,p_attempt,p_fence,p_result,p_body) IS DISTINCT FROM source THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('source',source,'qualification',qualification,'depth',depth,'subject',subject,'guard',guard);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_relight_adoption_evidence(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_adoption_evidence(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_adoption_evidence(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
