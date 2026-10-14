-- +goose Up
-- Transaction-bound rediscovery. This does not prevent later phantom inserts.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_person_candidate_matches(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_root uuid,p_root_sha text,p_child uuid,p_child_sha text,p_expected jsonb)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT count(*)=1 AND coalesce(bool_and(candidate.root_id=p_root AND candidate.root_sha=p_root_sha AND candidate.child_id=p_child AND candidate.child_sha=p_child_sha),false)
 FROM photo.semantic_proposal_person_candidates(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_expected) candidate;
$$;
ALTER FUNCTION photo.semantic_proposal_person_candidate_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_person_candidate_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_person_candidate_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
