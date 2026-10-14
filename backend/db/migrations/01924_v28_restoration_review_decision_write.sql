-- +goose Up
-- The V28 review trigger deliberately forbids direct API updates. Route the
-- exact state transition through a dedicated owner function instead.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.guard_v28_restoration_review() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.kind IN('portrait.glare','image.reflection') AND current_user<>'photo_fn_owner'
  THEN RAISE EXCEPTION 'V28 review requires verified receipt' USING ERRCODE='42501';END IF;
 ELSIF OLD.kind IN('portrait.glare','image.reflection') OR
  NEW.kind IN('portrait.glare','image.reflection') THEN
  IF current_user<>'photo_fn_owner' OR
   OLD.kind IS DISTINCT FROM NEW.kind OR OLD.tenant_id IS DISTINCT FROM NEW.tenant_id OR
   OLD.id IS DISTINCT FROM NEW.id OR OLD.run_id IS DISTINCT FROM NEW.run_id OR
   OLD.task_id IS DISTINCT FROM NEW.task_id OR
   OLD.candidate_artifact_id IS DISTINCT FROM NEW.candidate_artifact_id OR
   OLD.candidate_hash IS DISTINCT FROM NEW.candidate_hash OR
   OLD.base_artifact_id IS DISTINCT FROM NEW.base_artifact_id OR
   OLD.state<>'pending' OR NEW.state<>'decided' OR NEW.revision<>OLD.revision+1 OR
   NEW.decided_by IS NULL OR NEW.decided_at IS NULL OR
   NEW.decision NOT IN('accept','reject','skip') OR
   NEW.decision_route IS DISTINCT FROM CASE NEW.decision WHEN 'accept' THEN 'candidate'
    WHEN 'reject' THEN 'base' ELSE 'skip' END OR
   NEW.decision_hash !~ '^[0-9a-f]{64}$' OR NEW.selection IS NULL
  THEN RAISE EXCEPTION 'V28 review decision requires dedicated authority' USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v28_restoration_review() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_restoration_review() FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.decide_v28_restoration_review(
 p_tenant uuid,p_review uuid,p_actor uuid,p_session uuid,p_expected_revision bigint,
 p_decision text,p_selection jsonb,p_hash text,p_route text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;project_id uuid;authorized boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL OR
  p_expected_revision IS NULL OR p_expected_revision<1 OR p_expected_revision>=9007199254740991
  OR p_decision IS NULL OR p_decision NOT IN('accept','reject','skip') OR
  p_route IS DISTINCT FROM CASE p_decision WHEN 'accept' THEN 'candidate'
   WHEN 'reject' THEN 'base' ELSE 'skip' END OR
  p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' OR p_selection IS NULL
  THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active')
 THEN RETURN false;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review FOR UPDATE;
 IF NOT FOUND OR v.kind NOT IN('portrait.glare','image.reflection') OR
  v.state<>'pending' OR v.revision<>p_expected_revision OR
  p_selection IS DISTINCT FROM jsonb_build_object(
   'version','candidate_review_v1','candidate',jsonb_build_object(
    'artifact_id',v.candidate_artifact_id::text,'sha256',v.candidate_hash),
   'base',jsonb_build_object('artifact_id',v.base_artifact_id::text,
    'sha256',(SELECT sha256 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=v.base_artifact_id)),
   'revision',v.revision+1) OR
  coalesce(p_selection->'base'->>'sha256','') !~ '^[0-9a-f]{64}$'
  THEN RETURN false;END IF;
 -- Every interpolated value below is either a UUID, a fixed enum/operation,
 -- an int64, or a lowercase SHA proven by the exact selection above. The
 -- field order is the RFC 8785/JCS order used by Go PlanCandidate.
 IF p_hash IS DISTINCT FROM encode(sha256(convert_to(format(
  '{"actor_id":"%s","decision":"%s","kind":"%s","review_id":"%s","run_id":"%s","selection":{"base":{"artifact_id":"%s","sha256":"%s"},"candidate":{"artifact_id":"%s","sha256":"%s"},"revision":%s,"version":"candidate_review_v1"},"tenant_id":"%s","version":"candidate_decision_v1"}',
  p_actor,p_decision,v.kind,p_review,v.run_id,v.base_artifact_id,
  p_selection->'base'->>'sha256',v.candidate_artifact_id,v.candidate_hash,
  v.revision+1,p_tenant),'UTF8')),'hex') THEN RETURN false;END IF;
 SELECT w.project_id INTO project_id FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=v.run_id;
 IF project_id IS NULL THEN RETURN false;END IF;
 SELECT EXISTS(SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) a
  JOIN photo.projects project ON project.tenant_id=p_tenant AND project.id=project_id
   AND project.archived_at IS NULL
  WHERE a.role IN('owner','admin','editor') AND
   (a.role IN('owner','admin') OR project.visibility='workspace' OR EXISTS(
    SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant
     AND acl.project_id=project_id AND acl.user_id=p_actor
     AND acl.role IN('owner','admin','editor')))) INTO authorized;
 IF NOT authorized OR NOT photo.v28_restoration_review_decision_ready(p_tenant,p_review)
 THEN RETURN false;END IF;
 UPDATE photo.reviews SET state='decided',decision=p_decision,decided_by=p_actor,
  decided_at=clock_timestamp(),revision=v.revision+1,selection=p_selection,
  decision_hash=p_hash,decision_route=p_route
  WHERE tenant_id=p_tenant AND id=p_review AND state='pending' AND revision=p_expected_revision;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.decide_v28_restoration_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.decide_v28_restoration_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.decide_v28_restoration_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
