-- +goose Up
-- Full global V27 policy catalog, including disabled rows. Exact stability is
-- distinct from choosing required models or validating qualification content.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_qualification_policy_inventory(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT CASE WHEN photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN
 (SELECT coalesce(jsonb_agg(to_jsonb(policy) ORDER BY policy.id),'[]'::jsonb) FROM
 (SELECT * FROM photo.v2_qualification_policies WHERE operation IN('mask.person','face.detect','face.landmarks','face.parse','defects.propose','semantic.transfer') ORDER BY id LIMIT 65) policy)
 ELSE NULL END;
$$;
ALTER FUNCTION photo.semantic_proposal_qualification_policy_inventory(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_qualification_policy_inventory(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_qualification_policy_inventory(uuid,uuid,uuid,uuid) TO photo_api;
CREATE FUNCTION photo.lock_semantic_proposal_qualification_policy_snapshot(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_inventory jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF jsonb_typeof(p_inventory) IS DISTINCT FROM 'array' OR pg_column_size(p_inventory)>4194304 THEN RETURN false; END IF;
 IF jsonb_array_length(p_inventory) NOT BETWEEN 1 AND 64 THEN RETURN false; END IF;
 IF photo.lock_semantic_proposal_qualification_inventory(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN false; END IF;
 actual:=photo.semantic_proposal_qualification_policy_inventory(p_tenant,p_project,p_actor,p_session);
 RETURN actual IS NOT NULL AND jsonb_array_length(actual)<=64 AND pg_column_size(actual)<=4194304 AND actual=p_inventory;
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_qualification_policy_snapshot(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_qualification_policy_snapshot(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_qualification_policy_snapshot(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
