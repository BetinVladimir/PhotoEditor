-- +goose Up
-- Exact previously validated Params/PersonReport catalog pins after producer locks.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_person_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_root uuid,p_root_sha text,p_child uuid,p_child_sha text,p_expected jsonb,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF NOT photo.lock_semantic_proposal_person_producer(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_root,p_root_sha,p_child,p_child_sha,p_expected) THEN RETURN false; END IF;
 SELECT jsonb_agg(jsonb_build_object('contract',e.contract,'artifact_id',e.artifact_id,'sha256',e.sha256,'bucket',e.bucket,'key',e.object_key,'version',e.object_version,'bytes',e.bytes,'backend',e.backend) ORDER BY e.contract)
 INTO actual FROM photo.semantic_proposal_person_evidence_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_root,p_root_sha,p_child,p_child_sha) e;
 RETURN coalesce(actual=p_evidence AND jsonb_array_length(actual)=2,false);
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_person_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_person_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_person_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
