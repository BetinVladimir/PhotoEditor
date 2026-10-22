"""Authority regressions for the real SQL heartbeat tenant-root ordering fix."""
import json
from uuid import uuid4
import pytest
from psycopg import sql,errors
from psycopg.types.json import Jsonb
from test_processing_worker_activation import activation_input,activate
from test_worker_capacity_reconfiguration import snapshot

def setup(db):
 v=activation_input(db,'media');activate(db,v);return v

def identity(db,v):
 db.execute(sql.SQL('SET SESSION AUTHORIZATION {}').format(sql.Identifier(v['db_principal'])))
 db.execute('SET ROLE photo_worker')

def beat(db,v,**changes):
 args=dict(worker=v['worker_id'],kind='media',backend='cpu',operations=list(json.loads(v['qualified_operations_json'])),ram=int(v['ram_budget_bytes']),vram=0,scratch=int(v['scratch_budget_bytes']),policy=v['capacity_policy_sha']);args.update(changes)
 return db.execute('SELECT photo.heartbeat_worker_capacity_v2(%s,%s,%s,%s,%s,%s,%s,%s)',tuple(args.values())).fetchone()[0]

def residency(db,v,models=None,**changes):
 return db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(changes.get('worker',v['worker_id']),changes.get('policy',v['capacity_policy_sha']),Jsonb(models or []))).fetchone()[0]

def test_real_authenticated_capacity_and_residency_still_accepted(db):
 v=setup(db);identity(db,v);assert beat(db,v);assert residency(db,v)

@pytest.mark.parametrize('case',['foreign_principal','foreign_worker','disabled_worker','disabled_node','wrong_policy','wrong_class','missing_operation','wrong_backend','wrong_vram'])
def test_capacity_denies_unauthorized_or_invalid_reports(db,case):
 v=setup(db)
 if case=='disabled_worker':db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(v['worker_id'],))
 if case=='disabled_node':db.execute('UPDATE photo.resource_nodes SET enabled=false WHERE node_id=%s',(v['node_id'],))
 before=snapshot(db,v)
 if case!='foreign_principal':identity(db,v)
 else:db.execute('SET ROLE photo_worker')
 overrides={'foreign_worker':dict(worker=uuid4()),'wrong_policy':dict(policy='f'*64),'wrong_class':dict(kind='cpu'),'missing_operation':dict(operations=['unregistered.op']),'wrong_backend':dict(backend='cuda'),'wrong_vram':dict(vram=1)}
 if case in ['wrong_backend','wrong_vram']:
  with pytest.raises(errors.InvalidParameterValue):
   with db.transaction():beat(db,v,**overrides[case])
 else:assert beat(db,v,**overrides.get(case,{})) is False
 db.execute('RESET SESSION AUTHORIZATION');db.execute('RESET ROLE');assert snapshot(db,v)==before

@pytest.mark.parametrize('case',['foreign_principal','foreign_worker','disabled_worker','wrong_policy','wrong_lock','wrong_backend','duplicate_model'])
def test_residency_preserves_existing_identity_and_model_fences(db,case):
 v=setup(db)
 if case=='disabled_worker':db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(v['worker_id'],))
 before=snapshot(db,v)
 if case!='foreign_principal':identity(db,v)
 else:db.execute('SET ROLE photo_worker')
 overrides={};models=[]
 if case=='foreign_worker':overrides['worker']=uuid4()
 if case=='wrong_policy':overrides['policy']='f'*64
 if case in ['wrong_lock','wrong_backend','duplicate_model']:
  model=dict(model_lock_sha='f'*64 if case=='wrong_lock' else v['model_lock_sha'],model_id='u2net',backend='cuda' if case=='wrong_backend' else 'cpu',qualification_report_sha='d'*64,generation=1);models=[model,model] if case=='duplicate_model' else [model]
 if case=='duplicate_model':
  with pytest.raises(errors.InvalidParameterValue):
   with db.transaction():residency(db,v,models,**overrides)
 else:assert residency(db,v,models,**overrides) is False
 db.execute('RESET SESSION AUTHORIZATION');db.execute('RESET ROLE');assert snapshot(db,v)==before
