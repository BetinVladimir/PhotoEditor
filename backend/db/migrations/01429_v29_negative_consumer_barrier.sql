-- +goose Up
-- Serialize a declining review with new content consumers. Existing consumers
-- still require fencing/invalidation and replacement admission; no graph rewrite.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_consumer_allowed(p_tenant uuid,p_producer uuid,p_consumer uuid,p_consumer_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE origin photo.tasks%ROWTYPE;
BEGIN
 SELECT * INTO origin FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_producer FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF origin.operation<>'optical.negative' OR origin.operation_version<>'1' OR p_producer=p_consumer THEN RETURN true;END IF;
 -- Read-only derivatives of this exact canonical result retain the exception
 -- already used by negative review completion. They do not select a new image.
 IF EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_consumer_run AND r.kind='preview'
 AND r.v26_derivative_task_id=p_producer AND r.config_snapshot->>'source_result_id'=origin.canonical_result_id::text) THEN RETURN true;END IF;
 -- A repeatable-read snapshot could predate the declining review even after
 -- waiting for the producer lock. Refuse such consumer registration explicitly.
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'NEGATIVE_CONSUMER_REQUIRES_READ_COMMITTED' USING ERRCODE='25001';END IF;
 RETURN NOT EXISTS(SELECT 1 FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.task_id=p_producer AND v.run_id=origin.run_id
 AND v.kind='optical.negative' AND v.state='decided' AND v.decision IN('reject','skip') AND v.decision_route IN('base','skip'));
END $$;
ALTER FUNCTION photo.v29_negative_consumer_allowed(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_consumer_allowed(uuid,uuid,uuid,uuid) FROM PUBLIC;

CREATE FUNCTION photo.guard_v29_negative_review_consumers()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.kind='optical.negative' AND NEW.state='decided' AND NEW.decision IN('reject','skip') THEN
  -- The exclusive producer lock conflicts with every consumer's shared lock.
  -- After waiting, the consumer helper executes a fresh review lookup.
  PERFORM id FROM photo.tasks WHERE tenant_id=NEW.tenant_id AND id=NEW.task_id FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'NEGATIVE_REVIEW_PRODUCER_MISSING' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_negative_review_consumers() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_negative_review_consumers() FROM PUBLIC;
CREATE TRIGGER v29_negative_review_consumer_barrier BEFORE INSERT OR UPDATE OF state,decision,decision_route ON photo.reviews
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_negative_review_consumers();

CREATE FUNCTION photo.guard_v29_negative_content_consumer()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE producer uuid;consumer uuid;consumer_run uuid;
BEGIN
 IF TG_TABLE_NAME='tasks' THEN
  IF TG_OP='UPDATE' AND NEW.inputs IS NOT DISTINCT FROM OLD.inputs THEN RETURN NEW;END IF;
  consumer:=NEW.id;consumer_run:=NEW.run_id;
  FOR producer IN SELECT DISTINCT a.created_by_task_id FROM jsonb_array_elements(NEW.inputs) input
  JOIN photo.artifacts a ON a.tenant_id=NEW.tenant_id AND a.id::text=input->>'artifact_id' AND a.sha256=input->>'sha256'
  JOIN photo.tasks origin ON origin.tenant_id=a.tenant_id AND origin.id=a.created_by_task_id
  WHERE origin.operation='optical.negative' AND origin.operation_version='1' ORDER BY a.created_by_task_id LOOP
   IF photo.v29_negative_consumer_allowed(NEW.tenant_id,producer,consumer,consumer_run) IS DISTINCT FROM true THEN
    RAISE EXCEPTION 'NEGATIVE_RESULT_DECLINED' USING ERRCODE='23514';END IF;
  END LOOP;
 ELSIF TG_TABLE_NAME='v2_task_inputs' THEN
  consumer:=NEW.task_id;
  SELECT run_id INTO consumer_run FROM photo.tasks WHERE tenant_id=NEW.tenant_id AND id=consumer;
  SELECT a.created_by_task_id INTO producer FROM photo.artifacts a JOIN photo.tasks origin ON origin.tenant_id=a.tenant_id AND origin.id=a.created_by_task_id
  WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.artifact_id AND a.sha256=NEW.sha256 AND origin.operation='optical.negative' AND origin.operation_version='1';
  IF producer IS NOT NULL AND photo.v29_negative_consumer_allowed(NEW.tenant_id,producer,consumer,consumer_run) IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'NEGATIVE_RESULT_DECLINED' USING ERRCODE='23514';END IF;
 ELSIF TG_TABLE_NAME='task_edges' THEN
  SELECT run_id INTO consumer_run FROM photo.tasks WHERE tenant_id=NEW.tenant_id AND id=NEW.dependent_id;
  IF photo.v29_negative_consumer_allowed(NEW.tenant_id,NEW.prerequisite_id,NEW.dependent_id,consumer_run) IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'NEGATIVE_RESULT_DECLINED' USING ERRCODE='23514';END IF;
 ELSE RAISE EXCEPTION 'NEGATIVE_CONSUMER_BARRIER_RELATION_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_negative_content_consumer() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_negative_content_consumer() FROM PUBLIC;
CREATE TRIGGER v29_negative_direct_consumer_barrier BEFORE INSERT OR UPDATE OF inputs ON photo.tasks
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_negative_content_consumer();
CREATE TRIGGER v29_negative_closure_consumer_barrier BEFORE INSERT OR UPDATE OF artifact_id,sha256,task_id ON photo.v2_task_inputs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_negative_content_consumer();
CREATE TRIGGER v29_negative_edge_consumer_barrier BEFORE INSERT OR UPDATE OF prerequisite_id,dependent_id ON photo.task_edges
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_negative_content_consumer();
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
