-- +goose Up
-- Persist only a complete, recomputed dependency closure. Cancellation and
-- replacement admission are separate steps; terminal runs/results stay intact.
-- +goose StatementBegin
CREATE FUNCTION photo.record_v29_negative_invalidation(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_review uuid,p_decision text,p_plan jsonb,p_max_tasks integer,p_max_edges integer)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE inventory jsonb;current_inventory jsonb;v photo.reviews%ROWTYPE;origin uuid;origin_run uuid;
 ids uuid[];remaining uuid[];roots uuid[];affected uuid[];runs uuid[];expected jsonb;node uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_max_tasks IS NULL OR p_max_tasks NOT BETWEEN 1 AND 4096 OR p_max_edges IS NULL OR p_max_edges NOT BETWEEN 0 AND 65536
 OR p_plan IS NULL OR jsonb_typeof(p_plan)<>'object' OR pg_column_size(p_plan)>1048576
 OR p_decision IS NULL OR p_decision !~ '^[0-9a-f]{64}$'
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT task_id,run_id INTO origin,origin_run FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review AND kind='optical.negative';
 IF NOT FOUND THEN RETURN false;END IF;
 inventory:=photo.read_v29_negative_dependency_inventory(p_tenant,p_project,p_actor,p_session,origin,p_max_tasks,p_max_edges);
 IF inventory IS NULL THEN RETURN false;END IF;
 SELECT array_agg((n->>'task_id')::uuid ORDER BY (n->>'task_id')::uuid) INTO ids FROM jsonb_array_elements(inventory) n;
 -- Runs then tasks in UUID order; consumers must share-lock their producers.
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id IN(SELECT (n->>'run_id')::uuid FROM jsonb_array_elements(inventory) n) ORDER BY r.id FOR UPDATE;
 PERFORM t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=ANY(ids) ORDER BY t.id FOR UPDATE;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review FOR SHARE;
 IF NOT FOUND OR v.task_id<>origin OR v.run_id<>origin_run OR v.state<>'decided' OR v.revision<>2
 OR v.decision_hash IS DISTINCT FROM p_decision
 OR NOT ((v.decision='reject' AND v.decision_route='base') OR (v.decision='skip' AND v.decision_route='skip')) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v29_negative_review_selection(p_tenant,origin) s WHERE s.review_id=p_review AND s.decision_hash=p_decision AND s.route=v.decision_route) THEN RETURN false;END IF;
 current_inventory:=photo.read_v29_negative_dependency_inventory(p_tenant,p_project,p_actor,p_session,origin,p_max_tasks,p_max_edges);
 IF current_inventory IS DISTINCT FROM inventory THEN RETURN false;END IF;
 -- Topological removal validates every component, including unrelated cycles.
 remaining:=ids;
 WHILE cardinality(remaining)>0 LOOP
  SELECT array_agg((n->>'task_id')::uuid) INTO roots FROM jsonb_array_elements(inventory) n
  WHERE (n->>'task_id')::uuid=ANY(remaining) AND NOT EXISTS(
   SELECT 1 FROM jsonb_array_elements_text(n->'parents') parent WHERE parent::uuid=ANY(remaining));
  IF coalesce(cardinality(roots),0)=0 THEN RETURN false;END IF;
  SELECT coalesce(array_agg(id),'{}'::uuid[]) INTO remaining FROM unnest(remaining) id WHERE NOT id=ANY(roots);
 END LOOP;
 WITH RECURSIVE dependency AS (
 SELECT (n->>'task_id')::uuid child,parent::uuid parent FROM jsonb_array_elements(inventory) n CROSS JOIN LATERAL jsonb_array_elements_text(n->'parents') parent
 ), reached(id) AS (
 SELECT origin UNION SELECT d.child FROM dependency d JOIN reached r ON r.id=d.parent
 ) SELECT coalesce(array_agg(id ORDER BY id) FILTER(WHERE id<>origin),'{}'::uuid[]) INTO affected FROM reached;
 SELECT coalesce(array_agg(DISTINCT (n->>'run_id')::uuid ORDER BY (n->>'run_id')::uuid),'{}'::uuid[]) INTO runs FROM jsonb_array_elements(inventory) n
 WHERE (n->>'task_id')::uuid=ANY(affected) AND (n->>'run_id')::uuid<>origin_run;
 expected:=jsonb_build_object('schema_version',1,'origin_task_id',origin,'origin_run_id',origin_run,'route',v.decision_route,
 'affected_tasks',to_jsonb(affected),'consumer_runs',to_jsonb(runs),'recompile_origin',EXISTS(
 SELECT 1 FROM jsonb_array_elements(inventory) n WHERE (n->>'task_id')::uuid=ANY(affected) AND (n->>'run_id')::uuid=origin_run));
 IF expected IS DISTINCT FROM p_plan THEN RETURN false;END IF;
 FOREACH node IN ARRAY affected LOOP
  INSERT INTO photo.v29_negative_invalidations(tenant_id,task_id,review_id,origin_task_id,decision_hash,route)
  SELECT p_tenant,node,p_review,origin,p_decision,v.decision_route
  WHERE NOT EXISTS(SELECT 1 FROM photo.v29_negative_invalidations WHERE tenant_id=p_tenant AND task_id=node AND review_id=p_review)
  ON CONFLICT DO NOTHING;
  IF NOT EXISTS(SELECT 1 FROM photo.v29_negative_invalidations WHERE tenant_id=p_tenant AND task_id=node AND review_id=p_review
  AND origin_task_id=origin AND decision_hash=p_decision AND route=v.decision_route) THEN
   RAISE EXCEPTION 'NEGATIVE_INVALIDATION_REPLAY_CONFLICT' USING ERRCODE='23514';END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.record_v29_negative_invalidation(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v29_negative_invalidation(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v29_negative_invalidation(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
