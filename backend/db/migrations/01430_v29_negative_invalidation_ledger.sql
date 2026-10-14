-- +goose Up
ALTER TABLE photo.tasks ADD COLUMN v29_dependency_epoch bigint NOT NULL DEFAULT 0 CHECK(v29_dependency_epoch>=0);
CREATE TABLE photo.v29_negative_invalidations (
 tenant_id uuid NOT NULL,
 task_id uuid NOT NULL,
 review_id uuid NOT NULL,
 origin_task_id uuid NOT NULL,
 decision_hash text NOT NULL CHECK(decision_hash ~ '^[0-9a-f]{64}$'),
 route text NOT NULL CHECK(route IN('base','skip')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,task_id,review_id),
 FOREIGN KEY(tenant_id,task_id) REFERENCES photo.tasks(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,origin_task_id) REFERENCES photo.tasks(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,review_id) REFERENCES photo.reviews(tenant_id,id) ON DELETE CASCADE,
 CHECK(task_id<>origin_task_id)
);
CREATE INDEX v29_negative_invalidations_origin ON photo.v29_negative_invalidations(tenant_id,origin_task_id,review_id);
ALTER TABLE photo.v29_negative_invalidations ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v29_negative_invalidations FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v29_negative_invalidations TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,DELETE ON photo.v29_negative_invalidations TO photo_fn_owner;
CREATE TRIGGER v29_negative_invalidations_immutable BEFORE UPDATE ON photo.v29_negative_invalidations
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_negative_invalidated_publication()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE producer uuid;
BEGIN
 IF TG_TABLE_NAME='task_results' THEN producer:=NEW.task_id;
 ELSIF TG_TABLE_NAME='v2_artifact_links' THEN
  SELECT created_by_task_id INTO producer FROM photo.artifacts WHERE tenant_id=NEW.tenant_id AND id=NEW.artifact_id AND sha256=NEW.sha256;
 ELSE RAISE EXCEPTION 'NEGATIVE_INVALIDATION_RELATION_INVALID' USING ERRCODE='23514';END IF;
 IF producer IS NOT NULL THEN
  -- The invalidation writer must retain this same task lock until commit.
  PERFORM id FROM photo.tasks WHERE tenant_id=NEW.tenant_id AND id=producer FOR SHARE;
  IF EXISTS(SELECT 1 FROM photo.v29_negative_invalidations WHERE tenant_id=NEW.tenant_id AND task_id=producer) THEN
   RAISE EXCEPTION 'NEGATIVE_DEPENDENCY_INVALIDATED' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_negative_invalidated_publication() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_negative_invalidated_publication() FROM PUBLIC;
CREATE TRIGGER v29_negative_invalidated_result BEFORE INSERT ON photo.task_results
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_negative_invalidated_publication();
CREATE TRIGGER v29_negative_invalidated_link BEFORE INSERT ON photo.v2_artifact_links
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_negative_invalidated_publication();

CREATE FUNCTION photo.guard_v29_negative_invalidation_insert()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM photo.reviews v WHERE v.tenant_id=NEW.tenant_id AND v.id=NEW.review_id
 AND v.task_id=NEW.origin_task_id AND v.kind='optical.negative' AND v.state='decided' AND v.revision=2
 AND v.decision_hash=NEW.decision_hash AND v.decision_route=NEW.route
 AND ((v.decision='reject' AND NEW.route='base') OR (v.decision='skip' AND NEW.route='skip'))) THEN
  RAISE EXCEPTION 'NEGATIVE_INVALIDATION_DECISION_STALE' USING ERRCODE='23514';END IF;
 -- A changed MVCC tuple makes stale repeatable-read/serializable consumers
 -- fail serialization after waiting, rather than read a pre-invalidation ledger.
 UPDATE photo.tasks SET v29_dependency_epoch=v29_dependency_epoch+1 WHERE tenant_id=NEW.tenant_id AND id=NEW.task_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'NEGATIVE_INVALIDATION_TASK_MISSING' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_negative_invalidation_insert() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_negative_invalidation_insert() FROM PUBLIC;
CREATE TRIGGER v29_negative_invalidation_insert BEFORE INSERT ON photo.v29_negative_invalidations
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_negative_invalidation_insert();

CREATE FUNCTION photo.guard_v29_dependency_epoch()
RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.v29_dependency_epoch IS DISTINCT FROM OLD.v29_dependency_epoch AND
 (current_user<>'photo_fn_owner' OR NEW.v29_dependency_epoch<>OLD.v29_dependency_epoch+1) THEN
 RAISE EXCEPTION 'NEGATIVE_DEPENDENCY_EPOCH_IMMUTABLE' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_dependency_epoch() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_dependency_epoch() FROM PUBLIC;
CREATE TRIGGER v29_dependency_epoch_guard BEFORE UPDATE OF v29_dependency_epoch ON photo.tasks
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_dependency_epoch();

-- Keep the established explicit purge and its storage-cleanup preconditions.
DO $$ DECLARE definition text;anchor text:='''provenance_manifests'',';BEGIN
 definition:=pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure);
 IF strpos(definition,anchor)=0 OR strpos(definition,'v29_negative_invalidations')>0 THEN
  RAISE EXCEPTION 'NEGATIVE_INVALIDATION_PURGE_PATCH_MISMATCH';END IF;
 definition:=replace(definition,anchor,'''v29_negative_invalidations'','||anchor);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
