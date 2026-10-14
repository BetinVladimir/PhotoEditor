-- +goose Up
-- Retain real task failures and run-wide cancellation. Only terminal omitted
-- descendants of an authoritative settled skip can bypass adoption/review.
-- +goose StatementBegin
DO $$
DECLARE definition text; old_gate text;
BEGIN
 definition:=pg_get_functiondef('photo.v29_negative_skip_task_omitted(uuid,uuid)'::regprocedure);
 old_gate:='dependent.state IN(''succeeded'',''failed'',''cancelled'',''skipped'')';
 IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SKIP_FAILURE_RETENTION_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,old_gate,'dependent.state IN(''succeeded'',''cancelled'',''skipped'')');

 -- This is the original leaf finalizer OID, retained by the semantic wrapper.
 -- No operation-specific wrapper, cancellation branch or accounting is replaced.
 definition:=pg_get_functiondef('photo.finalize_v2_run_pre_semantic(uuid,uuid)'::regprocedure);
 old_gate:='AND t.state IN (''failed'',''cancelled'',''skipped'')';
 IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SKIP_FAILURE_GATE_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_gate,'AND NOT photo.v29_negative_skip_task_omitted(t.tenant_id,t.id) '||old_gate);
 old_gate:='t.tenant_id=p_tenant AND t.run_id=p_run AND (t.state<>''succeeded''';
 IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SKIP_ADOPTION_GATE_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_gate,'t.tenant_id=p_tenant AND t.run_id=p_run AND NOT photo.v29_negative_skip_task_omitted(t.tenant_id,t.id) AND (t.state<>''succeeded''');
 old_gate:='t.tenant_id=p_tenant AND t.run_id=p_run AND z.review_required';
 IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SKIP_REQUIRED_REVIEW_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_gate,'t.tenant_id=p_tenant AND t.run_id=p_run AND NOT photo.v29_negative_skip_task_omitted(t.tenant_id,t.id) AND z.review_required');
 EXECUTE definition;

 definition:=pg_get_functiondef('photo.v2_pending_finalizations()'::regprocedure);
 old_gate:='AND t.state IN (''failed'',''cancelled'',''skipped'')';
 IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SKIP_PENDING_FAILURE_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_gate,'AND NOT photo.v29_negative_skip_task_omitted(t.tenant_id,t.id) '||old_gate);
 old_gate:='AND (t.state<>''succeeded'' OR t.canonical_result_id IS NULL';
 IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SKIP_PENDING_ADOPTION_PATCH_MISMATCH';END IF;
 definition:=replace(definition,old_gate,'AND NOT photo.v29_negative_skip_task_omitted(t.tenant_id,t.id) '||old_gate);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
