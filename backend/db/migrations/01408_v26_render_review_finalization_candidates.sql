-- +goose Up
-- Preserve the candidate function OID, ordering, limit and existing barriers.
-- +goose StatementBegin
DO $$
DECLARE definition text; old_gate text; new_gate text;
BEGIN
 definition:=pg_get_functiondef('photo.v2_pending_finalizations()'::regprocedure);
 old_gate:='r.status IN (''running'',''cancelling'')';
 new_gate:=$gate$(r.status IN ('running','cancelling') OR (
 r.status='waiting_review' AND NOT r.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.tasks rt JOIN photo.task_results rz
 ON rz.tenant_id=rt.tenant_id AND rz.task_id=rt.id AND rz.id=rt.canonical_result_id
 WHERE rt.tenant_id=r.tenant_id AND rt.run_id=r.id AND rt.operation='render.compose' AND rz.review_required)
 AND EXISTS(SELECT 1 FROM photo.reviews rv WHERE rv.tenant_id=r.tenant_id AND rv.run_id=r.id AND rv.kind='render.compose')
 AND NOT EXISTS(SELECT 1 FROM photo.reviews rv WHERE rv.tenant_id=r.tenant_id AND rv.run_id=r.id
 AND (rv.kind<>'render.compose' OR rv.state<>'decided' OR rv.decision IS DISTINCT FROM 'accept' OR rv.decision_route IS DISTINCT FROM 'candidate'))
 AND NOT EXISTS(SELECT 1 FROM photo.tasks rt JOIN photo.task_results rz
 ON rz.tenant_id=rt.tenant_id AND rz.task_id=rt.id AND rz.id=rt.canonical_result_id
 WHERE rt.tenant_id=r.tenant_id AND rt.run_id=r.id AND rz.review_required AND
 (rt.operation<>'render.compose' OR rt.operation_version<>'1' OR NOT EXISTS(
 SELECT 1 FROM photo.reviews rv WHERE rv.tenant_id=r.tenant_id AND rv.run_id=r.id AND rv.task_id=rt.id
 AND rv.kind='render.compose' AND rv.state='decided' AND rv.decision='accept' AND rv.decision_route='candidate')))))$gate$;
 IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'V26_REVIEW_CANDIDATE_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,old_gate,new_gate);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
