-- Mirror immutable V23 decisions into the tenant audit stream in the same
-- transaction. The review reason remains in its restricted source row.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.audit_v23_learning_safety_review() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,
  resource_type,resource_id,request_id,details)
 VALUES(NEW.tenant_id,'user',NEW.actor_id,'learning.safety_review',
  'learning_version',NEW.version_id,gen_random_uuid(),
  jsonb_build_object('review_id',NEW.id,'project_id',NEW.project_id,
   'evaluation_sha',NEW.evaluation_sha,'accepted',NEW.accept));
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.audit_v23_learning_safety_review() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.audit_v23_learning_safety_review() FROM PUBLIC;
CREATE TRIGGER v23_learning_safety_review_audit
 AFTER INSERT ON photo.learning_safety_reviews
 FOR EACH ROW EXECUTE FUNCTION photo.audit_v23_learning_safety_review();

-- +goose StatementBegin
CREATE FUNCTION photo.audit_v23_profile_activation() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,
  resource_type,resource_id,request_id,details)
 VALUES(NEW.tenant_id,'user',NEW.actor_id,'learning.profile_activation',
  'learning_profile',NEW.profile_id,gen_random_uuid(),
  jsonb_build_object('activation_id',NEW.id,'project_id',NEW.project_id,
   'version_id',NEW.version_id,'previous_version_id',
   NEW.previous_version_id,'reason',NEW.reason));
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.audit_v23_profile_activation() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.audit_v23_profile_activation() FROM PUBLIC;
CREATE TRIGGER v23_profile_activation_audit
 AFTER INSERT ON photo.profile_activations
 FOR EACH ROW EXECUTE FUNCTION photo.audit_v23_profile_activation();
