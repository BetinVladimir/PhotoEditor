from pathlib import Path
import copy,importlib.util,json,os,signal,time
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import pytest
D=Path(__file__).resolve().parent
R=D.parents[1]
MODULE=D/'overlay/tools/v20_operation_evidence.py'
if not MODULE.exists():MODULE=R/'tools/v20_operation_evidence.py'
s=importlib.util.spec_from_file_location('v20_export',MODULE);m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
SCOPE_PATH=D/'overlay/docs/implementation-v2/current-operation-scope-2026-10-23.json'
if not SCOPE_PATH.exists():SCOPE_PATH=R/'docs/implementation-v2/current-operation-scope-2026-10-23.json'
SCOPE=SCOPE_PATH.read_bytes()
T=str(uuid4());P=str(uuid4());Q=str(uuid4());A=str(uuid4())

def fixture(status='unqualified'):
 cap={'operations':[{'operation':'semantic.transfer','operation_version':'1','resource_class':'gpu','model_id':None,'supported_backends':['cuda'],'available_backends':['cuda']if status=='ready'else[],'available':status=='ready','status':status,'reason':None if status=='ready'else'no_qualified_worker'}]}
 q={'qualification_id':Q,'tenant_id':T,'project_id':P,'operation':'semantic.transfer','version':'1','backend':'cuda','stored_status':'passed','trusted_import':True,'report_artifact_id':A,'report_sha':'a'*64,'model_lock_sha':'b'*64,'runtime_lock_sha':'c'*64,'device_profile_sha':'d'*64,'dataset_sha':'e'*64,'created_at':'2026-10-23T00:00:00+00:00','report_expires_at':'2026-10-24T00:00:00+00:00','report_ready':True}
 db={'tenant_id':T,'project_id':P,'head':2659,'database_observed_at':'2026-10-23T00:00:00+00:00','authority_pins':{'photo.tenant_capabilities()':'f'*64,'photo.tenant_capabilities_versioned()':'6'*64,'photo.v2_qualification_report_object(uuid,uuid,text)':'1'*64},'qualifications':[q],'workers':[]}
 return cap,db

def report(cap=None,db=None,annotations=None):
 c,d=fixture();return m.assemble(SCOPE,cap or c,db or d,T,P,'2'*64,annotations)
def identity(out,name='semantic.transfer'):return next(r for r in out['operations']if r['operation']==name)
def operation(out,name='semantic.transfer'):
 row=identity(out,name)
 return row['versions'][0]if row['versions']else{**row,'availability':None,'availability_authority':'not_reported_by_API'}

def test_exact117_identity_accounting_not109synthetic_ready():
 out=report();assert len(out['operations'])==117 and out['identity_counts']=={'product':109,'foundation':6,'bulk_control':1,'removed':1}
 for n,kind in [('alpha.predict','foundation'),('collection.mutate','bulk_control'),('storage.cleanup','removed')]:
  row=operation(out,n);assert row['identity_kind']==kind and row['availability']is None and row['availability_authority']=='not_reported_by_API'
 assert out['qualification_reverified']is False and out['capture_jointly_atomic']is False

def test_passed_reference_never_promotes_unqualified_api():
 out=report();row=operation(out);assert row['availability']['status']=='unqualified'and row['availability']['available']is False
 assert row['evidence'][0]['stored_status']=='passed'and row['evidence'][0]['report_sha']=='a'*64 and row['evidence'][0]['report_expires_at'].startswith('2026-10-24')
 assert not any(b['API_reports_available']for b in row['backends'])

def test_api_ready_without_project_report_preserved_with_explicit_gap():
 cap,db=fixture('ready');db['qualifications']=[];row=operation(report(cap,db));assert row['availability']==cap['operations'][0]and row['evidence_completeness']=='no_project_qualification_reference'

def test_expired_report_cannot_overwrite_authoritative_api_state():
 cap,db=fixture();db['qualifications'][0]['report_expires_at']='2020-01-01T00:00:00Z';out=report(cap,db);assert operation(out)['availability']==cap['operations'][0]and operation(out)['evidence'][0]['report_expires_at']=='2020-01-01T00:00:00Z'

@pytest.mark.parametrize('change',['tenant','project','qualification_tenant','qualification_project','worker_tenant'])
def test_cross_scope_refused(change):
 cap,db=fixture()
 if change in('tenant','project'):db[change+'_id']=str(uuid4())
 elif change.startswith('qualification_'):db['qualifications'][0][change.removeprefix('qualification_')+'_id']=str(uuid4())
 else:db['workers']=[{'tenant_id':str(uuid4())}]
 with pytest.raises(m.Refused):report(cap,db)

@pytest.mark.parametrize('change',['duplicate','unknown','badstatus','badbackend','availability_lie','secret_reason'])
def test_api_invalid_or_ambiguous_authority_refused(change):
 cap,db=fixture();row=cap['operations'][0]
 if change=='duplicate':cap['operations'].append(copy.deepcopy(row))
 elif change=='unknown':row['operation']='invented.operation'
 elif change=='badstatus':row['status']='passed'
 elif change=='badbackend':row['available_backends']=['mps']
 elif change=='availability_lie':row['available']=True
 else:row['reason']='https://user:password@private.invalid/'
 with pytest.raises(m.Refused):report(cap,db)

def test_provider_annotation_is_pinned_reference_not_authority():
 ann={'schema':1,'tenant_id':T,'project_id':P,'entries':[{'qualification_id':Q,'report_sha':'a'*64,'provider_mode':'native_transfer','receipt_links':[{'path':'work/example/result.json','sha256':'3'*64}]}]};out=report(annotations=ann);q=operation(out)['evidence'][0]
 assert q['provider_mode']=='native_transfer'and q['provider_mode_authority']=='operator_annotation_not_verified_by_exporter'and out['availability_not_recomputed']
 ann['entries'][0]['report_sha']='b'*64
 with pytest.raises(m.Refused,match='ANNOTATION_REPORT_PIN'):report(annotations=ann)

@pytest.mark.parametrize('path',['work/../secret.json','https://private.invalid/token','work/result.json?token=x'])
def test_receipt_urls_or_traversal_refused(path):
 ann={'schema':1,'tenant_id':T,'project_id':P,'entries':[{'qualification_id':Q,'report_sha':'a'*64,'provider_mode':'native','receipt_links':[{'path':path,'sha256':'3'*64}]}]}
 with pytest.raises(m.Refused,match='RECEIPT_REFERENCE'):report(annotations=ann)

def test_no_arbitrary_qualification_or_worker_payload_export():
 cap,db=fixture();db['qualifications'][0]['object_key']='SECRET customer/key';db['qualifications'][0]['sql']='SECRET password';raw=json.dumps(report(cap,db));assert'SECRET'not in raw and'object_key'not in raw

def test_scope_byte_drift_and_json_duplicate_refused():
 with pytest.raises(m.Refused,match='SCOPE_PIN'):m.scope_rows(SCOPE+b' ')
 with pytest.raises(m.Refused,match='DUPLICATE_JSON_KEY'):m.strict_json(b'{"x":1,"x":2}')

class Reply:
 def __init__(self,rows):self.rows=rows
 def fetchone(self):return self.rows[0]
 def fetchall(self):return self.rows
class DB:
 def __init__(self,login='observer_test'):self.calls=[];self.login=login
 @contextmanager
 def transaction(self):self.calls.append(('BEGIN',()));yield;self.calls.append(('END',()))
 def execute(self,query,params=()):
  self.calls.append((query,params))
  if query.startswith('SELECT current_user'):return Reply([(self.login,'on',__import__('datetime').datetime.now(__import__('datetime').timezone.utc),2659)])
  if query.startswith('SELECT EXISTS'):return Reply([(True,)])
  if query.startswith('SELECT jsonb_object_agg'):return Reply([(fixture()[1]['authority_pins'],)])
  return Reply([])

def test_db_real_readonly_protocol_and_scope_before_bounded_queries():
 db=DB();v=m.db_get(db,T,P,'observer_test');assert v['tenant_id']==T and v['project_id']==P
 sql=[q for q,p in db.calls];assert sql[1]=='SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY';assert not any(q.startswith(('INSERT','UPDATE','DELETE','CREATE','ALTER','GRANT','SET ROLE'))for q in sql)
 assert (m.QUAL_SQL,(T,P))in db.calls and(m.WORKER_SQL,(T,))in db.calls
 bad=DB('wrong_login')
 with pytest.raises(m.Refused,match='OBSERVER_IDENTITY_OR_READONLY'):m.db_get(bad,T,P,'observer_test')
 assert not any(q==m.QUAL_SQL for q,p in bad.calls)

def test_api_only_get_canonical_tenant_and_private_auth_never_output():
 cap,_=fixture();seen=[];raw=json.dumps(cap).encode()
 class Response:
  status=200
  def __enter__(self):return self
  def __exit__(self,*args):pass
  def read(self,n):assert n==m.MAX_BYTES+1;return raw
 class Opener:
  def open(self,request,timeout):seen.append(request);assert timeout==5;return Response()
 out,pin=m.api_get('http://localhost:58080',T,'SYNTHETIC.token',Opener());assert out==cap and pin==m.digest(raw);assert seen[0].method=='GET'and seen[0].full_url.endswith('/'+T+'/capabilities')and seen[0].headers['Authorization']=='Bearer SYNTHETIC.token'
 with pytest.raises(m.Refused):m.api_get('http://foreign.invalid',T,'x',Opener())
 with pytest.raises(m.Refused,match='API_REDIRECT_REFUSED'):m.NoRedirect().redirect_request(None,None,None,None,None,None)

def test_overall_deadline_restored_and_existing_timer_refused():
 old=signal.getsignal(signal.SIGALRM)
 with pytest.raises(m.Refused,match='OVERALL_DEADLINE'):
  with m.deadline(.02):time.sleep(.1)
 assert signal.getsignal(signal.SIGALRM)==old and signal.getitimer(signal.ITIMER_REAL)==(0.0,0.0)
 signal.setitimer(signal.ITIMER_REAL,5)
 try:
  with pytest.raises(m.Refused,match='ACTIVE_DEADLINE'):
   with m.deadline(1):pass
  assert signal.getitimer(signal.ITIMER_REAL)[0]>0
 finally:signal.setitimer(signal.ITIMER_REAL,0)

def test_createonly_private_output_and_no_network_test_execution(tmp_path):
 target=tmp_path/'result.json';m.write_new(target,b'{}');assert target.stat().st_mode&0o777==0o600
 with pytest.raises(FileExistsError):m.write_new(target,b'{"changed":true}')
 assert target.read_bytes()==b'{}'

def test_public_worker_hashes_only_not_raw_qualification(caplog):
 cap,db=fixture();db['workers']=[{'tenant_id':T,'worker_id':str(uuid4()),'enabled':True,'backends':['cpu'],'lock_id':None,'capacity_policy_sha':'4'*64,'qualified_operations_sha256':'5'*64,'last_seen':'2026-10-23T00:00:00Z','worker_fresh':True,'node_fresh':True,'qualified_operations':{'private':'SECRET'}}]
 out=report(cap,db);assert out['worker_observations'][0]['qualified_operations_sha256']=='5'*64 and'SECRET'not in json.dumps(out)
 db['workers'][0]['capacity_policy_sha']='private-url'
 with pytest.raises(m.Refused,match='WORKER_PIN'):report(cap,db)

def test_full_cli_mocked_capture_keeps_token_dsn_ephemeral(tmp_path,monkeypatch,capsys):
 import sys
 cap,db=fixture();output=tmp_path/'export.json';fds=[];token='SYNTHETIC.token';dsn='postgresql://SYNTHETIC_PRIVATE_DSN'
 for value in(token,dsn):
  rd,wr=os.pipe();os.write(wr,value.encode());os.close(wr);fds.append(rd)
 class Connection:
  def __enter__(self):return self
  def __exit__(self,*args):pass
 def connect(actual,**kw):assert actual==dsn and kw=={'connect_timeout':5,'autocommit':True};return Connection()
 monkeypatch.setitem(sys.modules,'psycopg',SimpleNamespace(connect=connect))
 monkeypatch.setattr(m,'api_get',lambda origin,tenant,actual:(cap,'2'*64)if actual==token and tenant==T else(_ for _ in()).throw(AssertionError('bad binding')))
 monkeypatch.setattr(m,'db_get',lambda connection,tenant,project,login:db if(tenant,project,login)==(T,P,'observer_test')else None)
 monkeypatch.setattr(sys,'argv',['export','--scope',str(SCOPE_PATH),'--tenant',T,'--project',P,'--api-origin','http://localhost:58080','--token-fd',str(fds[0]),'--observer-dsn-fd',str(fds[1]),'--observer-login','observer_test','--output',str(output)])
 try:m.main()
 finally:
  for fd in fds:os.close(fd)
 text=output.read_text()+capsys.readouterr().out;assert token not in text and dsn not in text and len(json.loads(output.read_text())['operations'])==117


@pytest.mark.parametrize('version',['x'*21,'x'*256,'legacy version: (untrusted)/v1 + metadata!','версия — '+('я'*247)])
def test_stored_version_SQL256_domain_is_not_import_policy(version):
 cap,db=fixture();db['qualifications'][0].update(version=version,trusted_import=False)
 out=report(cap,db);row=next(v for v in identity(out)['versions']if v['operation_version']==version);assert row['evidence'][0]['version']==version and row['evidence'][0]['trusted_import']is False and operation(out)['availability']['status']=='unqualified'

@pytest.mark.parametrize('version',['','x'*257,None,1])
def test_stored_version_outside_SQL_domain_refused(version):
 cap,db=fixture();db['qualifications'][0]['version']=version
 with pytest.raises(m.Refused,match='VERSION_SHAPE'):report(cap,db)


def test_new_authored_product_identities_preserve_old114():
 old=json.loads((R/'docs/implementation-v2/current-operation-scope-2026-10-17.json').read_bytes());new=json.loads(SCOPE);mapping={r['operation']:r for r in new['operations']}
 assert all(mapping[r['operation']]==r for r in old['operations'])
 for n in('region.authored_crop','render.authored_geometry','render.authored_segment'):
  row=identity(report(),n);assert row['identity_kind']=='product'and row['versions']==[]and row['API_identity_reported']is False

def test_versioned_availability_never_ORs_render_compose_modes():
 cap,db=fixture();first=cap['operations'][0];first.update(operation='render.compose',operation_version='1');second=copy.deepcopy(first);second.update(operation_version='2',status='ready',available=True,reason=None,available_backends=['cuda']);cap['operations'].append(second)
 q=db['qualifications'][0];q.update(operation='render.compose',version='1')
 row=identity(report(cap,db),'render.compose');by={v['operation_version']:v for v in row['versions']}
 assert by['1']['availability']['status']=='unqualified'and by['1']['evidence'][0]['version']=='1'
 assert by['2']['availability']['status']=='ready'and by['2']['evidence']==[]and row['availability_merge_performed']is False
 assert 'availability'not in row

def test_old_versionless_duplicate_and_single_ambiguous_API_refused():
 cap,db=fixture();cap['operations'][0].pop('operation_version')
 with pytest.raises(m.Refused,match='VERSIONED_CAPABILITY_FIELDS'):report(cap,db)
 cap['operations'].append(copy.deepcopy(cap['operations'][0]))
 with pytest.raises(m.Refused,match='VERSIONED_CAPABILITY_FIELDS'):report(cap,db)

def test_unmatched_qualification_version_cannot_inherit_API_ready():
 cap,db=fixture('ready');db['qualifications'][0]['version']='old-untrusted-version'
 row=identity(report(cap,db));by={v['operation_version']:v for v in row['versions']}
 assert by['1']['availability']['status']=='ready'and by['1']['evidence']==[]
 assert by['old-untrusted-version']['availability']is None and by['old-untrusted-version']['evidence'][0]['stored_status']=='passed'


def test_versioned_API_without_observed_DB_versioned_authority_refused():
 cap,db=fixture();db['authority_pins']['photo.tenant_capabilities_versioned()']=None
 with pytest.raises(m.Refused,match='VERSIONED_AUTHORITY_NOT_OBSERVED'):report(cap,db)
