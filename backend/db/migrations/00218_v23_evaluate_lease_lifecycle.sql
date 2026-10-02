-- A failed late compiler releases its fenced lease; a long compiler renews
-- without changing the canonical fit result or granting access to live data.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.renew_v23_learning_evaluate(p_expansion uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE; r photo.runs%ROWTYPE;
BEGIN
 IF p_expansion IS NULL OR p_token IS NULL THEN RETURN false; END IF;
 SELECT * INTO hint FROM photo.graph_expansions x WHERE x.id=p_expansion;
 IF hint.id IS NULL OR hint.kind<>'v2.learning.evaluate/1' THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs rr WHERE rr.tenant_id=hint.tenant_id
  AND rr.id=hint.run_id FOR UPDATE;
 IF r.id IS NULL OR r.cancel_requested OR r.status<>'running' OR
  r.graph_sealed OR NOT EXISTS(SELECT 1 FROM
   photo.v23_learning_candidate_input(hint.tenant_id,hint.run_id))
  THEN RETURN false; END IF;
 UPDATE photo.graph_expansions x
  SET expansion_lease_until=clock_timestamp()+interval '60 seconds'
 WHERE x.tenant_id=hint.tenant_id AND x.id=hint.id AND x.run_id=hint.run_id
  AND x.kind='v2.learning.evaluate/1' AND x.state='open'
  AND x.expansion_token=p_token
  AND x.expansion_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_v23_learning_evaluate(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_v23_learning_evaluate(uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_v23_learning_evaluate(uuid,uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.release_v23_learning_evaluate(p_expansion uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE;
BEGIN
 IF p_expansion IS NULL OR p_token IS NULL THEN RETURN false; END IF;
 SELECT * INTO hint FROM photo.graph_expansions x WHERE x.id=p_expansion;
 IF hint.id IS NULL OR hint.kind<>'v2.learning.evaluate/1' THEN RETURN false; END IF;
 PERFORM rr.id FROM photo.runs rr WHERE rr.tenant_id=hint.tenant_id
  AND rr.id=hint.run_id FOR UPDATE;
 UPDATE photo.graph_expansions x SET expansion_token=NULL,
  expansion_lease_until=NULL,
  next_expansion_at=clock_timestamp()+interval '5 seconds'
 WHERE x.tenant_id=hint.tenant_id AND x.id=hint.id AND x.run_id=hint.run_id
  AND x.kind='v2.learning.evaluate/1' AND x.state='open'
  AND x.expansion_token=p_token;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_v23_learning_evaluate(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_v23_learning_evaluate(uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_v23_learning_evaluate(uuid,uuid)
 TO photo_scheduler;
