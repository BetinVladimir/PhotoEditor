-- +goose Up
-- A prepared target proposal needs a new tenant editor decision. Archived
-- review bytes never populate this table and no role receives direct DML.
-- +goose StatementBegin
CREATE TABLE photo.v30_import_review_decisions(
 tenant_id uuid NOT NULL,
 workflow_id uuid NOT NULL,
 project_id uuid NOT NULL,
 entry_id uuid NOT NULL,
 target_version_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN('defects','dust')),
 proposal_id uuid NOT NULL,
 proposal_sha text NOT NULL CHECK(proposal_sha ~ '^[0-9a-f]{64}$'),
 base_id uuid NOT NULL,
 base_sha text NOT NULL CHECK(base_sha ~ '^[0-9a-f]{64}$'),
 label_map_id uuid,
 label_map_sha text,
 actor_id uuid NOT NULL,
 request_id uuid NOT NULL,
 decision text NOT NULL CHECK(decision IN('accept','reject','skip')),
 accepted_ids jsonb NOT NULL CHECK(CASE WHEN jsonb_typeof(accepted_ids)='array' THEN jsonb_array_length(accepted_ids)<=4096 ELSE false END),
 rejected_ids jsonb NOT NULL CHECK(CASE WHEN jsonb_typeof(rejected_ids)='array' THEN jsonb_array_length(rejected_ids)<=4096 ELSE false END),
 decision_hash text NOT NULL CHECK(decision_hash ~ '^[0-9a-f]{64}$'),
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 status text NOT NULL CHECK(status IN('granted','revoked')),
 epoch bigint NOT NULL CHECK(epoch BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,workflow_id,entry_id,target_version_id,kind,proposal_id),
 FOREIGN KEY(tenant_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 FOREIGN KEY(tenant_id,target_version_id) REFERENCES photo.asset_versions(tenant_id,id),
 CHECK((kind='defects' AND label_map_id IS NOT NULL AND label_map_sha ~ '^[0-9a-f]{64}$')
    OR (kind='dust' AND label_map_id IS NULL AND label_map_sha IS NULL)),
 CHECK(proposal_id<>base_id AND (label_map_id IS NULL OR (label_map_id<>proposal_id AND label_map_id<>base_id)))
);
ALTER TABLE photo.v30_import_review_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_import_review_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_import_review_decisions
 TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v30_import_review_decisions
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.v30_import_review_decisions TO photo_fn_owner;

-- The established cleanup gate controls physical tenant metadata deletion.
DO $$ DECLARE definition text;BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF definition IS NULL OR position('tables_to_purge constant text[]:=ARRAY[' IN definition)=0
 OR position('''v30_import_review_decisions''' IN definition)>0 THEN
  RAISE EXCEPTION 'V30_IMPORT_REVIEW_PURGE_ALLOWLIST_DRIFT';
 END IF;
 EXECUTE replace(definition,'tables_to_purge constant text[]:=ARRAY[',
  'tables_to_purge constant text[]:=ARRAY[''v30_import_review_decisions'',');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
