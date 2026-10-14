-- +goose Up
-- Bounded list from the immutable negative invalidation ledger, never client graph traversal.
-- +goose StatementBegin
CREATE FUNCTION photo.list_v29_perspective_replacement_reviews(p_tenant uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_after uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid;item record;items jsonb:='[]'::jsonb;scope jsonb;last_id uuid;more boolean:=false;n integer:=0;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 SELECT w.project_id INTO project FROM photo.reviews v JOIN photo.v2_workflows w ON w.tenant_id=v.tenant_id AND w.run_id=v.run_id
 WHERE v.tenant_id=p_tenant AND v.id=p_review AND v.kind='optical.negative' AND v.state='decided'
 AND v.revision=2 AND v.decision='reject' AND v.decision_route='base' AND v.decision_hash=p_hash;
 IF NOT FOUND OR photo.v29_negative_invalidation_authorized(p_tenant,project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 FOR item IN SELECT i.task_id FROM photo.v29_negative_invalidations i
 JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id AND w.project_id=project
 WHERE i.tenant_id=p_tenant AND i.review_id=p_review AND i.decision_hash=p_hash AND i.route='base'
 AND t.operation='optical.perspective' AND t.operation_version='1' AND (p_after IS NULL OR i.task_id>p_after)
 ORDER BY i.task_id LIMIT 101 LOOP
 n:=n+1;IF n>100 THEN more:=true;EXIT;END IF;
 scope:=photo.read_v29_perspective_replacement_review_scope(p_tenant,project,p_actor,p_session,p_review,p_hash,item.task_id);
 items:=items||jsonb_build_array(jsonb_build_object('original_task_id',item.task_id,'project_id',project,'scope',scope));last_id:=item.task_id;
 END LOOP;
 RETURN jsonb_build_object('review_id',p_review,'decision_hash',p_hash,'items',items,'next_after',CASE WHEN more THEN last_id ELSE NULL END);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.list_v29_perspective_replacement_reviews(uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_v29_perspective_replacement_reviews(uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.list_v29_perspective_replacement_reviews(uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
