#!/usr/bin/env python3
"""Emit explicitly reviewed domain relationships; never infer a polymorphic FK."""
import json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
tables=json.loads((ROOT/'docs/implementation/contracts/tables.json').read_text())['tables'];by={t['name']:t for t in tables}
refs={
 'user_id':'users','created_by':'users','invited_by':'users','published_by':'users','decided_by':'users','requested_by':'users','owner_user_id':'users',
 'project_id':'projects','asset_id':'assets','asset_version_id':'asset_versions','master_artifact_id':'artifacts','original_artifact_id':'artifacts',
 'parent_asset_version_id':'asset_versions','created_by_run_id':'runs','source_collection_id':'collections','source_run_id':'runs',
 'applied_style_version_id':'style_versions','collection_id':'collections','share_id':'share_links','staging_artifact_id':'artifacts',
 'verified_version_id':'asset_versions','reservation_id':'quota_reservations','upload_session_id':'upload_sessions',
 'style_id':'styles','training_run_id':'runs','export_artifact_id':'artifacts','style_version_id':'style_versions','artifact_id':'artifacts',
 'training_set_id':'training_sets','before_asset_version_id':'asset_versions','after_asset_version_id':'asset_versions',
 'ignore_mask_artifact_id':'artifacts','snapshot_id':'snapshots','input_snapshot_id':'snapshots','output_collection_id':'collections',
 'run_id':'runs','params_artifact_id':'artifacts','canonical_result_id':'task_results','task_id':'tasks',
 'result_id':'task_results','candidate_artifact_id':'artifacts','base_artifact_id':'artifacts','last_preview_run_id':'runs',
 'bulk_operation_id':'bulk_operations','download_job_id':'download_jobs','manifest_artifact_id':'artifacts',
 'version_id':'asset_versions','source_version_id':'asset_versions','created_by_task_id':'tasks',
 'challenge_id':'auth_challenges','invitation_id':'invitations','payload_id':'mail_payloads','parent_id':'sessions',
 'parent_task_id':'tasks',
}
lines=['-- Explicit domain constraints and row security grants.','-- +goose Up']
def sql(x):lines.append(x)
def unique(t,cols):sql(f'ALTER TABLE photo.{t} ADD UNIQUE ({cols});')
def fk(table,cols,target,remote):sql(f'ALTER TABLE photo.{table} ADD FOREIGN KEY ({cols}) REFERENCES photo.{target} ({remote}) DEFERRABLE INITIALLY DEFERRED;')
for t in tables:
 name=t['name']
 if t['scope']=='tenant':fk(name,'tenant_id','tenants','id')
 for col in t['columns']:
  field=col.split(':')[0]
  if field not in refs:continue
  target=refs[field]
  if by[target]['scope']=='global':fk(name,field,target,'id')
  elif t['scope']=='tenant' or name=='resource_slots':fk(name,'tenant_id,'+field,target,'tenant_id,id')
  elif name=='mail_payloads' and field=='invitation_id':fk(name,field,target,'id')
  else:raise ValueError((name,field,target))
unique('asset_versions','tenant_id,asset_id,id')
fk('collection_items','tenant_id,asset_id,asset_version_id','asset_versions','tenant_id,asset_id,id')
fk('collection_items','tenant_id,collection_id,section_id','sections','tenant_id,collection_id,id')
unique('collection_items','tenant_id,id,asset_version_id');unique('collection_items','tenant_id,collection_id,id')
fk('publications','tenant_id,item_id,asset_version_id','collection_items','tenant_id,id,asset_version_id')
fk('collections','tenant_id,id,cover_item_id','collection_items','tenant_id,collection_id,id')
unique('share_links','tenant_id,id,collection_id')
fk('share_sessions','tenant_id,share_id,collection_id','share_links','tenant_id,id,collection_id')
unique('tasks','tenant_id,run_id,id')
for c in ['prerequisite_id','dependent_id']:fk('task_edges','tenant_id,run_id,'+c,'tasks','tenant_id,run_id,id')
fk('task_results','tenant_id,task_id,attempt','task_attempts','tenant_id,task_id,attempt')
fk('graph_expansions','tenant_id,run_id,parent_task_id','tasks','tenant_id,run_id,id')
checks={
 'users':["locale IN ('ru','bg','en')","theme IN ('light','dark','system')","status IN ('active','disabled')"],
 'auth_challenges':['attempts BETWEEN 0 AND 5'],
 'sessions':["client_type IN ('web','native')"],
 'mail_payloads':["(kind='otp' AND challenge_id IS NOT NULL AND invitation_id IS NULL) OR (kind='invite' AND invitation_id IS NOT NULL AND challenge_id IS NULL)"],
 'memberships':["role IN ('owner','admin','editor','viewer')","status IN ('active','suspended')"],
 'invitations':["role IN ('admin','editor','viewer')"],
 'projects':["visibility IN ('workspace','restricted')","length(btrim(name)) BETWEEN 1 AND 120"],
 'project_acl':["role IN ('viewer','editor')"],
 'collections':["state IN ('draft','published','archived','deleting')","length(btrim(name)) BETWEEN 1 AND 120"],
 'collection_items':['rating BETWEEN 0 AND 5',"visibility IN ('hidden','visible')","moderation_state IN ('pending_review','accepted','rejected')","jsonb_typeof(tags)='array'"],
 'publications':["state IN ('published','revoked')"],
 'share_links':["moderation IN ('owner_review','auto_after_validation')","jsonb_typeof(permissions)='object' AND permissions ?& ARRAY['view','download','upload']","jsonb_typeof(permissions->'view')='boolean' AND jsonb_typeof(permissions->'download')='boolean' AND jsonb_typeof(permissions->'upload')='boolean'"],
 'upload_sessions':['expected_size>0','actual_size IS NULL OR actual_size BETWEEN 1 AND expected_size',"state IN ('initializing','uploading','completing','verifying','verified','rejected','aborted','expired')"],
 'upload_parts':['part_no BETWEEN 1 AND 10000'],
 'styles':["length(btrim(name)) BETWEEN 1 AND 120","length(description)<=2000"],
 'style_versions':["schema_version='2.0.0'","renderer_version='photo_renderer_v2'","jsonb_typeof(config_json)='object'","octet_length(config_json::text)<=110000000","approval_status IN ('accepted','needs_review')"],
 'training_pairs':['before_asset_version_id<>after_asset_version_id'],
 'snapshots':["kind IN ('input','training','selection','download')"],
 'runs':["kind IN ('analyze','apply','cull','preview','export','ingest','download','bulk','maintenance')","status IN ('draft','queued','running','waiting_review','cancelling','cancelled','completed','completed_with_errors','failed')"],
 'tasks':["state IN ('blocked','ready','queued','running','succeeded','retry_wait','failed','cancelled','skipped')","resource_class IN ('cpu','gpu','media','archive','collection','mail')",'attempt>=0 AND retry_count BETWEEN 0 AND 3 AND oom_count BETWEEN 0 AND 2',"(state='running') = (lease_until IS NOT NULL)","state<>'succeeded' OR canonical_result_id IS NOT NULL"],
 'task_edges':['prerequisite_id<>dependent_id',"condition IN ('success','accepted','rejected','terminal')"],
 'reviews':["state IN ('pending','decided')","decision IS NULL OR decision IN ('accept','reject','skip')","(state='pending' AND decision IS NULL) OR (state='decided' AND decision IS NOT NULL AND decided_by IS NOT NULL AND decided_at IS NOT NULL)"],
 'download_jobs':["status IN ('queued','preparing','building','uploading','ready','expired','failed','cancelling','cancelled')"],
 'resource_slots':['(tenant_id IS NULL)=(task_id IS NULL)'],
}
for table,values in checks.items():
 for i,value in enumerate(values):sql(f'ALTER TABLE photo.{table} ADD CONSTRAINT {table}_domain_{i} CHECK ({value});')
sql("CREATE UNIQUE INDEX one_owner_per_tenant ON photo.memberships(tenant_id) WHERE role='owner' AND status='active';")
for table,cols,where in [
 ('collection_items','tenant_id,collection_id,position,id',''),('outbox','created_at', ' WHERE sent_at IS NULL'),
 ('tasks','tenant_id,state,next_attempt_at,id',''),('artifacts','tenant_id,expires_at',''),('artifact_leases','tenant_id,artifact_id,expires_at',''),
 ('run_events','tenant_id,collection_id,cursor',''),('run_events','tenant_id,run_id,cursor',''),('auth_challenges','email_normalized,created_at',''),
 ('sessions','family_id',''),('upload_sessions','state,expires_at',''),('download_jobs','tenant_id,status,expires_at','')]:
 sql(f'CREATE INDEX ON photo.{table}({cols}){where};')
# Ownership stays with migrator; only explicit function owner can access all tenant rows.
sql('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA photo TO photo_fn_owner;')
for t in tables:
 if t['scope']=='tenant' and t['name'] not in ['outbox','inbox','task_attempts','task_results','worker_registrations','resource_reservations','scheduler_tenants']:
  sql(f'GRANT SELECT, INSERT, UPDATE, DELETE ON photo.{t["name"]} TO photo_api;')
for name in ['users','auth_challenges','sessions','auth_rate_limits','auth_idempotency_records','mail_payloads','auth_mail_outbox']:
 sql(f'GRANT SELECT, INSERT, UPDATE, DELETE ON photo.{name} TO photo_auth;')
sql('REVOKE ALL ON ALL FUNCTIONS IN SCHEMA photo FROM PUBLIC;')
# Block direct updates to immutable payloads while allowing lifecycle metadata.
sql('-- +goose StatementBegin\nCREATE FUNCTION photo.reject_content_update() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$\nDECLARE col text; BEGIN FOREACH col IN ARRAY TG_ARGV LOOP IF to_jsonb(NEW)->col IS DISTINCT FROM to_jsonb(OLD)->col THEN RAISE EXCEPTION \'immutable field %.%\',TG_TABLE_NAME,col USING ERRCODE=\'23514\'; END IF; END LOOP; RETURN NEW; END $$;\n-- +goose StatementEnd')
immutable={
 'asset_versions':['asset_id','master_artifact_id','parent_asset_version_id','created_by_run_id','content_hash','version_no'],
 'collection_items':['asset_id','asset_version_id'],
 'style_versions':['style_id','version_no','config_json','config_hash','schema_version','renderer_version','training_run_id','metrics','approval_status'],
 'snapshots':['kind','collection_id','source_revision','training_set_id','principal_type','principal_id','digest','item_count'],
 'snapshot_items':['snapshot_id','ordinal','item_id','asset_version_id','artifact_id','sha256','payload'],
 'task_results':['task_id','attempt','fencing_token','outputs','metrics','review_required','result_digest'],
 'artifacts':['bucket','object_key','object_version','sha256','bytes','media_type','width','height','icc_sha256'],
 'overrides':['asset_version_id','input_sha256','revision','config','config_hash','created_by'],
 'publications':['item_id','asset_version_id'],
}
for table,columns in immutable.items():
 args=','.join("'"+c+"'" for c in ['tenant_id','id',*columns])
 sql(f'CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.{table} FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update({args});')
sql('-- +goose StatementBegin\nCREATE FUNCTION photo.enforce_owner() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$\nDECLARE tid uuid; expected uuid; actual uuid; BEGIN IF TG_TABLE_NAME=\'tenants\' THEN tid:=COALESCE(NEW.id,OLD.id); ELSE tid:=COALESCE(NEW.tenant_id,OLD.tenant_id); END IF; SELECT owner_user_id INTO expected FROM photo.tenants WHERE id=tid; IF NOT FOUND THEN RETURN NULL; END IF; SELECT user_id INTO actual FROM photo.memberships WHERE tenant_id=tid AND role=\'owner\' AND status=\'active\'; IF actual IS DISTINCT FROM expected THEN RAISE EXCEPTION \'tenant owner membership invariant\' USING ERRCODE=\'23514\'; END IF; RETURN NULL; END $$;\n-- +goose StatementEnd')
sql('ALTER FUNCTION photo.enforce_owner() OWNER TO photo_fn_owner;')
for table in ['tenants','memberships']:sql(f'CREATE CONSTRAINT TRIGGER owner_invariant AFTER INSERT OR UPDATE OR DELETE ON photo.{table} DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION photo.enforce_owner();')
sql('REVOKE ALL ON ALL FUNCTIONS IN SCHEMA photo FROM PUBLIC;')
sql('-- +goose Down\n-- +goose StatementBegin\nDO $$ BEGIN RAISE EXCEPTION \'Forward-only migration\'; END $$;\n-- +goose StatementEnd')
path=ROOT/'backend/db/migrations/00010_constraints_security.sql'
content='\n\n'.join(lines)+'\n'
import sys
if '--check' in sys.argv:
 if not path.exists() or path.read_text()!=content:raise SystemExit('Database constraints are stale')
else:path.write_text(content)
