-- Preserve all baseline kind values while admitting the fenced v2 graph.
ALTER TABLE photo.snapshots DROP CONSTRAINT snapshots_domain_0;
ALTER TABLE photo.snapshots ADD CONSTRAINT snapshots_domain_0
 CHECK(kind IN('input','training','selection','download','style_import','holdout','v2'));
ALTER TABLE photo.runs DROP CONSTRAINT runs_domain_0;
ALTER TABLE photo.runs ADD CONSTRAINT runs_domain_0
 CHECK(kind IN('analyze','apply','cull','preview','export','ingest','download','bulk','maintenance','v2'));
ALTER TABLE photo.runs ADD CONSTRAINT runs_v2_fail_fast
 CHECK(kind<>'v2' OR (failure_policy='fail_fast' AND status<>'completed_with_errors'));
