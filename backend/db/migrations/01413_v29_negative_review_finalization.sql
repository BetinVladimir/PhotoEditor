-- +goose Up
-- Extend only accepted candidate review completion; preserve all existing
-- predecessor finalization, graph/adoption, quota and event barriers.
-- +goose StatementBegin
DO $$
DECLARE definition text; old_text text; new_text text;
BEGIN
 definition:=pg_get_functiondef('photo.finalize_v2_run(uuid,uuid)'::regprocedure);
 old_text:='v.kind<>''render.compose''';
 new_text:='v.kind NOT IN (''render.compose'',''optical.negative'')';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_FINALIZER_KIND_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,new_text);
 old_text:='task.operation<>''render.compose''';
 new_text:='task.operation NOT IN (''render.compose'',''optical.negative'')';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_FINALIZER_OPERATION_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,new_text);
 old_text:='v.kind=''render.compose'' AND v.state=''decided''';
 new_text:='v.kind=task.operation AND v.state=''decided''';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_FINALIZER_REVIEW_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,new_text);
 -- A null decision must also block completion, even for corrupt legacy rows.
 definition:=replace(definition,'v.decision<>''accept''','v.decision IS DISTINCT FROM ''accept''');
 definition:=replace(definition,'v.decision_route<>''candidate''','v.decision_route IS DISTINCT FROM ''candidate''');
 EXECUTE definition;

 definition:=pg_get_functiondef('photo.v2_pending_finalizations()'::regprocedure);
 old_text:='rt.operation=''render.compose''';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_PENDING_OPERATION_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,'rt.operation IN (''render.compose'',''optical.negative'')');
 old_text:='rv.kind=''render.compose''';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_PENDING_REVIEW_PATCH_MISMATCH';END IF;
 -- The positive per-task binding below is made exact after extending the
 -- run-level existence gate. Unknown review kinds still block the run.
 definition:=replace(definition,old_text,'rv.kind IN (''render.compose'',''optical.negative'')');
 old_text:='rv.kind<>''render.compose''';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_PENDING_KIND_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,'rv.kind NOT IN (''render.compose'',''optical.negative'')');
 old_text:='rt.operation<>''render.compose''';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_PENDING_REQUIRED_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,'rt.operation NOT IN (''render.compose'',''optical.negative'')');
 old_text:='rv.task_id=rt.id';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'NEGATIVE_PENDING_TASK_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_text,'rv.task_id=rt.id AND rv.kind=rt.operation AND rv.decided_by IS NOT NULL AND rv.decided_at IS NOT NULL AND rv.decision_hash IS NOT NULL');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
