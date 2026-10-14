-- +goose Up
-- Publish adopted negative review only with SHA-bound original Params bytes supplied by the trusted scheduler.
-- +goose StatementBegin
CREATE FUNCTION photo.create_v29_negative_review(p_tenant uuid,p_task uuid,p_root uuid,p_candidate uuid,p_base uuid,p_params_jcs text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task_row photo.tasks%ROWTYPE; run_row photo.runs%ROWTYPE; existing photo.reviews%ROWTYPE;
 candidate_sha text; accepted boolean; review_id uuid; cursor_value bigint;
BEGIN
 IF p_params_jcs IS NULL OR octet_length(p_params_jcs) NOT BETWEEN 2 AND 65536
 OR (p_params_jcs::jsonb)->>'schema_version' IS DISTINCT FROM '1'
 OR (p_params_jcs::jsonb)->'base'->'source'->>'artifact_id' IS DISTINCT FROM p_base::text
 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO run_row FROM photo.runs WHERE tenant_id=p_tenant AND id=task_row.run_id FOR UPDATE;
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF run_row.id IS NULL OR run_row.status NOT IN('running','waiting_review') OR run_row.cancel_requested
 OR NOT run_row.graph_sealed
 OR EXISTS(SELECT 1 FROM photo.tasks pending WHERE pending.tenant_id=p_tenant AND pending.run_id=run_row.id
 AND (pending.state<>'succeeded' OR pending.canonical_result_id IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(pending.id,pending.canonical_result_id))))
 OR task_row.operation<>'optical.negative' OR task_row.operation_version<>'1'
 OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=task_row.canonical_result_id
 AND z.task_id=p_task AND z.attempt=task_row.attempt AND z.fencing_token=task_row.fencing_token AND z.review_required)
 THEN RETURN NULL;END IF;
 SELECT sha256 INTO candidate_sha FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_candidate;
SELECT EXISTS(
 SELECT 1 FROM photo.tasks t
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.v2_artifact_links root_link ON root_link.tenant_id=w.tenant_id AND root_link.workflow_id=w.id AND root_link.project_id=w.project_id
 JOIN photo.artifacts root ON root.tenant_id=root_link.tenant_id AND root.id=root_link.artifact_id AND root.sha256=root_link.sha256
 JOIN photo.v2_artifact_links child_link ON child_link.tenant_id=w.tenant_id AND child_link.workflow_id=w.id AND child_link.project_id=w.project_id
 JOIN photo.artifacts c ON c.tenant_id=child_link.tenant_id AND c.id=child_link.artifact_id AND c.sha256=child_link.sha256
 JOIN photo.artifacts b ON b.tenant_id=t.tenant_id AND b.id=p_base
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.run_id=task_row.run_id AND t.operation='optical.negative' AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
 AND root.id=p_root AND root_link.ordinal=0 AND root_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
 AND child_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id::text,'sha256',root.sha256))
 AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
 AND c.id=p_candidate AND c.sha256=candidate_sha AND c.created_by_task_id=t.id AND c.produced_attempt=t.attempt AND c.produced_fence=t.fencing_token
 AND root.state='ready' AND root.verified_at IS NOT NULL AND root.media_type='application/json' AND root.output_role='document'
 AND c.state='ready' AND c.verified_at IS NOT NULL AND c.media_type='application/json' AND c.output_role='working_image'
 AND b.state='ready' AND b.verified_at IS NOT NULL AND b.media_type='application/json'
 AND coalesce(root.object_version,'') NOT IN('','null') AND coalesce(c.object_version,'') NOT IN('','null') AND coalesce(b.object_version,'') NOT IN('','null')
 AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()) AND (b.expires_at IS NULL OR b.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.artifact_id=b.id AND i.sha256=b.sha256 AND i.contract='WorkingImage')
 AND EXISTS(SELECT 1 FROM photo.artifacts params
 JOIN photo.v2_task_inputs pi ON pi.tenant_id=t.tenant_id AND pi.task_id=t.id AND pi.project_id=w.project_id
 AND pi.artifact_id=params.id AND pi.sha256=params.sha256 AND pi.contract='Params'
 WHERE params.tenant_id=t.tenant_id AND params.id=t.params_artifact_id
 AND params.sha256=encode(sha256(convert_to(p_params_jcs,'UTF8')),'hex')
 AND params.state='ready' AND params.verified_at IS NOT NULL AND params.media_type='application/json'
 AND params.bytes=octet_length(p_params_jcs) AND params.object_version NOT IN('','null')
 AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 AND (p_params_jcs::jsonb)->'base'->'source'=jsonb_build_object('artifact_id',b.id::text,'sha256',b.sha256))
) INTO accepted;
 IF NOT accepted THEN RETURN NULL;END IF;
 SELECT * INTO existing FROM photo.reviews WHERE tenant_id=p_tenant AND task_id=p_task AND kind='optical.negative';
 IF FOUND THEN
 IF existing.run_id=task_row.run_id AND existing.candidate_artifact_id=p_candidate AND existing.candidate_hash=candidate_sha
 AND existing.base_artifact_id=p_base THEN RETURN existing.id;END IF;
 RETURN NULL;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.reviews WHERE tenant_id=p_tenant AND task_id=p_task) THEN RETURN NULL;END IF;
 INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,base_artifact_id,candidate_hash,kind,reasons)
 VALUES(p_tenant,task_row.run_id,p_task,p_candidate,p_base,candidate_sha,'optical.negative','["negative_review"]') RETURNING id INTO review_id;
 IF run_row.status='running' THEN
 IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status='waiting_review',revision=revision+1 WHERE tenant_id=p_tenant AND id=run_row.id;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,cursor_value,run_row.id,run_row.created_by,'principal','v2.run.waiting_review',
 jsonb_build_object('run_id',run_row.id,'status','waiting_review','review_id',review_id));
 END IF;
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,review_id,'photo.events.v1','review.created',jsonb_build_object('schema_version',1,'event_id',review_id,
 'type','review.created','tenant_id',p_tenant,'run_id',run_row.id,'review_id',review_id,'kind','optical.negative'));
 RETURN review_id;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_v29_negative_review(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v29_negative_review(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v29_negative_review(uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
