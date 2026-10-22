"""Real operator SQL guards; synthetic relational fixtures do not qualify photos."""
import copy,hashlib,json,re
from datetime import datetime,timezone
from pathlib import Path
import pytest
from psycopg import errors,sql
from test_processing_worker_activation import activation_input,activate
from test_worker_capacity_reconfiguration import snapshot,relational_task
ROOT=Path(__file__).resolve().parents[2]
ADDITIONS=['style.import','style.resolve','image.color','image.local','quality.check','export.encode']
def prepare(db):
 v=activation_input(db,'cpu');activate(db,v)
 p={**snapshot(db,v),'resource_class':'cpu','doctor_report_jcs':v['doctor_report_jcs'],'doctor_report_file_sha':v['doctor_report_file_sha']}
 p['new_operations']={**p['expected_worker']['qualified_operations'],**{x:['cpu'] for x in ADDITIONS}}
 p['preflight']=dict(status='OPERATOR_REVIEWED_PROCESSING_ADDITIONS',reviewed_at=datetime.now(timezone.utc).isoformat(),worker_id=v['worker_id'],worker_stopped=True,scheduler_stopped=True,rabbit_empty=True,runtime_lock_sha256='1'*64,host_evidence_sha256='2'*64,operation_evidence_sha256='3'*64)
 return v,p
def apply(db,p):
 s=(ROOT/'deploy/requalify-processing-worker.sql').read_text();s='\n'.join(x for x in s.splitlines() if not x.startswith('\\') and x.strip() not in ('BEGIN;','COMMIT;'))
 db.execute(re.sub(":'processing_requalification_json'",lambda _:sql.Literal(json.dumps(p)).as_string(db),s))
def test_addition_preserves_identity_resources_doctor_and_requires_heartbeat(db):
 v,p=prepare(db);apply(db,p);after=snapshot(db,v)
 before=copy.deepcopy(p['expected_worker']);actual=after['expected_worker']
 for k in before:
  if k not in ['qualified_operations','last_seen','reported_capacity_policy_sha','capacity_policy_reported_at']:assert actual[k]==before[k],k
 assert actual['qualified_operations']==p['new_operations']
 assert actual['last_seen'].startswith('1970-01-01') and actual['reported_capacity_policy_sha'] is None
 assert after['expected_node']==p['expected_node'] and after['expected_slots']==p['expected_slots']
@pytest.mark.parametrize('case',['unsupported_model','wrong_backend','wrong_class','worker_drift','wrong_cert','wrong_policy','doctor_drift','remove_old','render_addition','missing_review','privileged_login'])
def test_denied_requalification_is_atomic(db,case):
 v,p=prepare(db)
 if case=='unsupported_model':
  op=db.execute("select operation from photo.operation_registry where resource_class='cpu' and backends ? 'cpu' and model_id is not null limit 1").fetchone();assert op;p['new_operations'][op[0]]=['cpu']
 elif case=='wrong_backend':p['new_operations']['style.import']=['cuda']
 elif case=='wrong_class':p['new_operations']['asset.verify']=['cpu']
 elif case=='worker_drift':db.execute("update photo.worker_registrations set capacity='{\"changed\":1}' where worker_id=%s",(v['worker_id'],))
 elif case=='wrong_cert':p['expected_worker']['certificate_sha256']='f'*64
 elif case=='wrong_policy':p['expected_worker']['capacity_policy_sha']='e'*64
 elif case=='doctor_drift':p['doctor_report_file_sha']='d'*64
 elif case=='remove_old':p['new_operations']={x:['cpu'] for x in ADDITIONS if x not in p['expected_worker']['qualified_operations']}
 elif case=='render_addition':p['new_operations']['render.compose']=['cpu']
 elif case=='missing_review':p['preflight']['worker_stopped']=False
 else:db.execute(sql.SQL('GRANT photo_api TO {}').format(sql.Identifier(v['db_principal'])))
 before=snapshot(db,v)
 with pytest.raises(errors.RaiseException):
  with db.transaction():apply(db,p)
 assert snapshot(db,v)==before
@pytest.mark.parametrize('binding',['running','dispatch_only','reserved_expired'])
def test_live_binding_or_reservation_denied(db,binding):
 v,p=prepare(db);task=relational_task(db,v,binding)
 if binding=='reserved_expired':db.execute("insert into photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,vram_bytes,expires_at) values(%s,%s,%s,1,1,0,clock_timestamp()-interval '1 day')",(v['tenant_id'],task,v['node_id']))
 db.execute('SET CONSTRAINTS ALL IMMEDIATE');db.execute('SET CONSTRAINTS ALL DEFERRED');before=snapshot(db,v)
 with pytest.raises(errors.RaiseException,match='CAS_OR_IDLE_CONFLICT'):
  with db.transaction():apply(db,p)
 assert snapshot(db,v)==before
