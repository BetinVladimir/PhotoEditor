-- +goose Up
-- Draft consumers must enter cancelling so existing task cancellation and run
-- finalization can drain them. Replay also repairs prior cancel-requested drafts.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.request_v29_negative_invalidation_cancel(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_review uuid,p_decision text,p_plan jsonb,p_max_tasks integer,p_max_edges integer)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE origin_run uuid;run uuid;revision_value bigint;
BEGIN
 -- Re-record/recompute under retained locks in this same transaction. A saved
 -- Go plan or earlier ledger row alone is insufficient cancellation authority.
 IF photo.record_v29_negative_invalidation(p_tenant,p_project,p_actor,p_session,p_review,p_decision,p_plan,p_max_tasks,p_max_edges) IS DISTINCT FROM true THEN RETURN false;END IF;
 origin_run:=(p_plan->>'origin_run_id')::uuid;
 UPDATE photo.tasks t SET cancel_requested=true
 WHERE t.tenant_id=p_tenant AND t.id IN(SELECT value::uuid FROM jsonb_array_elements_text(p_plan->'affected_tasks'))
 AND t.state NOT IN('succeeded','failed','cancelled','skipped') AND NOT t.cancel_requested;
 -- V2 fail-fast consumer runs are cancelled as a whole. The origin run is kept
 -- for explicit review/recompile closure, never silently cancelled here.
 FOR run IN SELECT value::uuid FROM jsonb_array_elements_text(p_plan->'consumer_runs') ORDER BY value::uuid LOOP
  SELECT revision INTO revision_value FROM photo.runs WHERE tenant_id=p_tenant AND id=run AND kind='v2'
  AND status IN('draft','queued','running','waiting_review') AND (NOT cancel_requested OR status='draft') FOR UPDATE;
  IF FOUND THEN
   IF revision_value>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
   UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1
   WHERE tenant_id=p_tenant AND id=run AND id<>origin_run;
  END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.request_v29_negative_invalidation_cancel(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.request_v29_negative_invalidation_cancel(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.request_v29_negative_invalidation_cancel(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
