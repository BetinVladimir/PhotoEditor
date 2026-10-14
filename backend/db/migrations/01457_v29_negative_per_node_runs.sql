-- +goose Up
-- Distinct node runs admit only after parent output bytes/SHA are known.
-- Old grouped plans must fail membership, never silently recover as v2 plans.
-- +goose StatementBegin
DO $$
DECLARE definition text;old_text text;
BEGIN
 definition:=pg_get_functiondef('photo.v29_negative_replacement_member(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,uuid)'::regprocedure);
 old_text:='V29.NegativeReplacement:';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_REPLACEMENT_NAMESPACE_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,'V29.NegativeReplacement/v2:');
 old_text:='''run:''||(intent->>''original_run_id'')';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_REPLACEMENT_RUN_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,old_text||'||'':''||p_original::text');
 old_text:='''version'',''v29_negative_replacement_v1''';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_REPLACEMENT_MARKER_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,'''version'',''v29_negative_replacement_v2'',''original_task_id'',original.id::text');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
