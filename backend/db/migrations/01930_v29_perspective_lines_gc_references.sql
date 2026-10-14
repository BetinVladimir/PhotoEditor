-- +goose Up
-- Serialize new line/source bindings with artifact GC and retain both refs
-- while the immutable tenant-scoped catalogue row exists.
CREATE TRIGGER ready_artifact_reference BEFORE INSERT ON photo.v29_perspective_lines
 FOR EACH ROW EXECUTE FUNCTION photo.guard_ready_artifact_reference('source_id','artifact_id');
-- +goose StatementBegin
DO $$
DECLARE body text; updated text;
BEGIN
 SELECT prosrc INTO body FROM pg_proc
 WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]'
    OR strpos(body,'photo.v29_perspective_lines')>0 THEN
  RAISE EXCEPTION 'V29_PERSPECTIVE_LINES_GC_SHAPE';
 END IF;
 updated:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',
  E'\\1 EXISTS(SELECT FROM photo.v29_perspective_lines l WHERE l.tenant_id=p_tenant AND p_artifact IN(l.source_id,l.artifact_id)) OR ','i');
 IF updated=body THEN RAISE EXCEPTION 'V29_PERSPECTIVE_LINES_GC_PATCH_FAILED';END IF;
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',updated);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
