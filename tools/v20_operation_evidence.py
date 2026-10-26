"""Read-only evidence export. API availability is copied, never recomputed."""
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
import argparse, hashlib, json, os, re, signal, stat, sys
from urllib.parse import urlsplit
from urllib.request import Request, build_opener, HTTPRedirectHandler, ProxyHandler
from uuid import UUID

MAX_BYTES=8<<20
BACKENDS={'cpu','cuda','mps'}
SCOPE_SHA='7d8d61e19c7b010e0b2409ddd0e724e1187ee9c4c98277de0a40b785a0a19832'
API_KEYS={'operation','resource_class','model_id','supported_backends','available_backends','available','status','reason','operation_version'}
class Refused(Exception):pass

def require(ok,code):
    if not ok:raise Refused(code)
def digest(raw):return hashlib.sha256(raw).hexdigest()
def uid(x):
    require(type(x)is str,'UUID_TYPE')
    try:value=UUID(x)
    except ValueError:raise Refused('UUID_INVALID')from None
    require(str(value)==x and value.int!=0,'UUID_INVALID');return x
def timestamp(x):
    if x is None:return None
    require(type(x)is str and len(x)<=40,'TIME_INVALID')
    try:v=datetime.fromisoformat(x.replace('Z','+00:00'))
    except ValueError:raise Refused('TIME_INVALID')from None
    require(v.tzinfo is not None,'TIME_INVALID');return x

def strict_json(raw):
    require(2<=len(raw)<=MAX_BYTES,'JSON_BOUNDS')
    def pairs(items):
        out={}
        for k,v in items:require(k not in out,'DUPLICATE_JSON_KEY');out[k]=v
        return out
    try:return json.loads(raw,object_pairs_hook=pairs,parse_constant=lambda _:(_ for _ in ()).throw(Refused('JSON_NONFINITE')))
    except(UnicodeError,ValueError):raise Refused('JSON_INVALID')from None

def read_file(path,limit=MAX_BYTES):
    p=Path(path);fd=os.open(p,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(fd,'rb')as f:
        s=os.fstat(f.fileno());require(stat.S_ISREG(s.st_mode)and s.st_uid in(0,os.getuid())and not s.st_mode&0o022 and 0<s.st_size<=limit,'INPUT_FILE_INVALID');raw=f.read(limit+1);after=os.fstat(f.fileno())
    require(len(raw)==s.st_size and(s.st_ino,s.st_size,s.st_mtime_ns,s.st_ctime_ns)==(after.st_ino,after.st_size,after.st_mtime_ns,after.st_ctime_ns),'INPUT_CHANGED');return raw

def scope_rows(raw):
    require(digest(raw)==SCOPE_SHA,'SCOPE_PIN');v=strict_json(raw);rows=v['operations'];require(len(rows)==117 and len({r['operation']for r in rows})==117,'EXACT117_SCOPE')
    counts={'product':0,'foundation':0,'bulk_control':0,'removed':0};out=[]
    for r in rows:
        state=r['scope_status'];kind='foundation'if state=='foundation_no_independent_current_product_producer'else'bulk_control'if state=='mvp_bulk_control_not_taskcommand'else'removed'if state=='removed_historical_not_current_operation'else'product';counts[kind]+=1;out.append({'operation':r['operation'],'package':r['declared_package'],'identity_kind':kind,'scope_status':state})
    require(counts=={'product':109,'foundation':6,'bulk_control':1,'removed':1},'SCOPE_ACCOUNTING');return out,counts

@contextmanager
def deadline(seconds):
    require(type(seconds)in(int,float)and 0<seconds<=30,'DEADLINE_INVALID');old=signal.getsignal(signal.SIGALRM);timer=signal.getitimer(signal.ITIMER_REAL);require(timer==(0.0,0.0),'ACTIVE_DEADLINE')
    def expired(*_):raise Refused('OVERALL_DEADLINE')
    signal.signal(signal.SIGALRM,expired)
    try:signal.setitimer(signal.ITIMER_REAL,seconds);yield
    finally:signal.setitimer(signal.ITIMER_REAL,0);signal.signal(signal.SIGALRM,old)

class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self,*a,**k):raise Refused('API_REDIRECT_REFUSED')

def api_get(origin,tenant,token,opener=None):
    uid(tenant);u=urlsplit(origin);require(u.scheme=='https'or u.scheme=='http'and u.hostname in('localhost','127.0.0.1','::1'),'API_ORIGIN');require(bool(u.hostname)and not u.username and not u.password and u.path in('','/')and not u.query and not u.fragment,'API_ORIGIN')
    require(type(token)is str and 1<=len(token)<=4096 and re.fullmatch(r'[A-Za-z0-9._~-]+',token)is not None,'TOKEN_FORMAT')
    request=Request(origin.rstrip('/')+'/api/v1/tenants/'+tenant+'/capabilities',headers={'Authorization':'Bearer '+token,'Accept':'application/json'},method='GET')
    client=opener or build_opener(ProxyHandler({}),NoRedirect())
    with client.open(request,timeout=5)as response:
        require(response.status==200,'API_STATUS');raw=response.read(MAX_BYTES+1);require(len(raw)<=MAX_BYTES,'API_RESPONSE_BOUND');return strict_json(raw),digest(raw)

QUAL_SQL="""SELECT jsonb_build_object('qualification_id',q.id,'tenant_id',q.tenant_id,'project_id',q.project_id,'operation',q.operation,'version',q.operation_version,'backend',q.backend,'stored_status',q.status,'trusted_import',q.trusted_import,'report_artifact_id',q.report_artifact_id,'report_sha',q.report_sha,'model_lock_sha',q.model_lock_sha,'runtime_lock_sha',q.runtime_lock_sha,'device_profile_sha',q.device_profile_sha,'dataset_sha',q.dataset_sha,'created_at',q.created_at,'report_expires_at',a.expires_at,'report_ready',coalesce(a.state='ready'AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL,false)) FROM photo.model_qualifications q LEFT JOIN photo.artifacts a ON a.tenant_id=q.tenant_id AND a.id=q.report_artifact_id WHERE q.tenant_id=%s AND q.project_id=%s ORDER BY q.id LIMIT 4097"""
WORKER_SQL="""SELECT jsonb_build_object('worker_id',w.worker_id,'tenant_id',w.tenant_id,'enabled',w.enabled,'backends',w.backends,'lock_id',nullif(w.lock_id,''),'capacity_policy_sha',w.capacity_policy_sha,'qualified_operations_sha256',encode(sha256(convert_to(w.qualified_operations::text,'UTF8')),'hex'),'last_seen',w.last_seen,'worker_fresh',photo.capacity_policy_worker_fresh(w),'node_fresh',coalesce(photo.capacity_policy_node_fresh(n),false)) FROM photo.worker_registrations w LEFT JOIN photo.resource_nodes n ON n.node_id=w.node_id WHERE w.tenant_id=%s ORDER BY w.worker_id LIMIT 257"""

def db_get(connection,tenant,project,expected_login):
    uid(tenant);uid(project);require(re.fullmatch(r'[a-z_][a-z0-9_]{0,62}',expected_login)is not None,'OBSERVER_LOGIN')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5000'");connection.execute("SET LOCAL lock_timeout='1000'")
        login,readonly,clock,head=connection.execute("SELECT current_user,current_setting('transaction_read_only'),clock_timestamp(),(SELECT max(version_id)FROM public.goose_db_version WHERE is_applied)").fetchone();require(login==expected_login and readonly=='on','OBSERVER_IDENTITY_OR_READONLY')
        require(connection.execute('SELECT EXISTS(SELECT FROM photo.projects WHERE tenant_id=%s AND id=%s)',(tenant,project)).fetchone()==(True,),'PROJECT_SCOPE')
        authority_pins=connection.execute("SELECT jsonb_object_agg(n, CASE WHEN p.oid IS NULL THEN NULL ELSE encode(sha256(convert_to(p::text,'UTF8')),'hex') END) FROM (VALUES ('photo.tenant_capabilities()'),('photo.tenant_capabilities_versioned()'),('photo.v2_qualification_report_object(uuid,uuid,text)')) s(n) LEFT JOIN pg_proc p ON p.oid=to_regprocedure(s.n)").fetchone()[0]
        qualifications=[r[0]for r in connection.execute(QUAL_SQL,(tenant,project)).fetchall()];workers=[r[0]for r in connection.execute(WORKER_SQL,(tenant,)).fetchall()];require(len(qualifications)<=4096 and len(workers)<=256,'DB_ROW_BOUND')
    return {'tenant_id':tenant,'project_id':project,'database_observed_at':clock.isoformat(),'head':head,'authority_pins':authority_pins,'qualifications':qualifications,'workers':workers}

def valid_capabilities(value,known):
    require(type(value)is dict and set(value)=={'operations'}and type(value['operations'])is list and len(value['operations'])<=4096,'CAPABILITY_SHAPE');out={}
    for row in value['operations']:
        require(type(row)is dict and set(row)==API_KEYS,'VERSIONED_CAPABILITY_FIELDS');n=row['operation'];version=row['operation_version'];require(type(version)is str and 1<=len(version)<=256,'API_VERSION_SHAPE');identity=(n,version);require(n in known and identity not in out,'UNKNOWN_OR_DUPLICATE_OPERATION_VERSION')
        require(row['status']in('ready','unqualified','unsupported')and type(row['available'])is bool,'API_STATUS_SHAPE');require(type(row['resource_class'])is str and re.fullmatch(r'[a-z_]{1,32}',row['resource_class'])is not None,'CLASS_SHAPE')
        require(row['model_id']is None or type(row['model_id'])is str and re.fullmatch(r'[a-zA-Z0-9_.-]{1,128}',row['model_id'])is not None,'MODEL_SHAPE');require(row['reason']is None or type(row['reason'])is str and re.fullmatch(r'[a-zA-Z0-9_]{1,128}',row['reason'])is not None,'REASON_SHAPE')
        for key in('supported_backends','available_backends'):
            v=row[key];require(type(v)is list and len(v)<=3 and len(set(v))==len(v)and all(type(x)is str and x in BACKENDS for x in v),'BACKEND_SHAPE')
        require(set(row['available_backends'])<=set(row['supported_backends']),'API_BACKEND_INCONSISTENT');require(row['available']==(row['status']=='ready')and row['available']==bool(row['available_backends']),'API_STATUS_INCONSISTENT');out[identity]=dict(row)
    return out

PINS=('report_sha','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')
def assemble(scope,api,db,tenant,project,api_sha,annotations=None):
    identities,counts=scope_rows(scope);known={r['operation']for r in identities};caps=valid_capabilities(api,known);uid(tenant);uid(project);require(db['tenant_id']==tenant and db['project_id']==project,'CROSS_TENANT_PROJECT');timestamp(db['database_observed_at']);require(type(db['head'])is int and db['head']>0,'HEAD_SHAPE');require(type(db['qualifications'])is list and len(db['qualifications'])<=4096 and type(db['workers'])is list and len(db['workers'])<=256,'DB_ROW_BOUND')
    require(set(db['authority_pins'])=={'photo.tenant_capabilities()','photo.tenant_capabilities_versioned()','photo.v2_qualification_report_object(uuid,uuid,text)'}and all(v is None or type(v)is str and re.fullmatch('[0-9a-f]{64}',v)is not None for v in db['authority_pins'].values()),'AUTHORITY_PINS')
    require(db['authority_pins']['photo.tenant_capabilities_versioned()']is not None,'VERSIONED_AUTHORITY_NOT_OBSERVED')
    evidence={};seen=set()
    for q in db['qualifications']:
        require(q['tenant_id']==tenant and q['project_id']==project and q['operation']in known,'QUALIFICATION_SCOPE');qid=uid(q['qualification_id']);require(qid not in seen,'DUPLICATE_QUALIFICATION');seen.add(qid);uid(q['report_artifact_id']);require(q['backend']in BACKENDS and q['stored_status']in('passed','failed')and type(q['trusted_import'])is bool and type(q['report_ready'])is bool,'QUALIFICATION_SHAPE')
        for key in PINS:require(q[key]is None or type(q[key])is str and re.fullmatch('[0-9a-f]{64}',q[key])is not None,'QUALIFICATION_PIN')
        timestamp(q['created_at']);timestamp(q['report_expires_at']);require(type(q['version'])is str and 1<=len(q['version'])<=256,'VERSION_SHAPE')
        clean={k:q[k]for k in('qualification_id','version','backend','stored_status','trusted_import','report_artifact_id',*PINS,'created_at','report_expires_at','report_ready')};clean.update(provider_mode='not_recorded_by_generic_qualification',provider_mode_authority='none',receipt_links=[],evidence_status='stored_reference_not_reverified_no_readiness_authority');evidence[qid]=(q['operation'],clean)
    if annotations is not None:
        require(set(annotations)=={'schema','tenant_id','project_id','entries'}and annotations['schema']==1 and annotations['tenant_id']==tenant and annotations['project_id']==project and type(annotations['entries'])is list and len(annotations['entries'])<=4096,'ANNOTATION_SCOPE');used=set()
        for a in annotations['entries']:
            require(set(a)=={'qualification_id','report_sha','provider_mode','receipt_links'}and a['qualification_id']in evidence and a['qualification_id']not in used,'ANNOTATION_TARGET');used.add(a['qualification_id']);entry=evidence[a['qualification_id']][1];require(a['report_sha']==entry['report_sha']and entry['report_sha']is not None,'ANNOTATION_REPORT_PIN');require(type(a['provider_mode'])is str and re.fullmatch('[a-z][a-z0-9_]{0,63}',a['provider_mode'])is not None,'PROVIDER_MODE');require(type(a['receipt_links'])is list and len(a['receipt_links'])<=16,'RECEIPT_BOUND')
            for link in a['receipt_links']:
                require(set(link)=={'path','sha256'}and type(link['path'])is str and link['path'].startswith('work/')and len(link['path'])<=512 and not any(x in link['path']for x in('..','://','?','#','\\'))and re.fullmatch('[0-9a-f]{64}',link['sha256'])is not None,'RECEIPT_REFERENCE')
            entry.update(provider_mode=a['provider_mode'],provider_mode_authority='operator_annotation_not_verified_by_exporter',receipt_links=a['receipt_links'])
    workers=[]
    for w in db['workers']:
        require(w['tenant_id']==tenant,'WORKER_TENANT');uid(w['worker_id']);timestamp(w['last_seen']);require(all(type(w[k])is bool for k in('enabled','worker_fresh','node_fresh')),'WORKER_FLAGS');require(type(w['backends'])is list and len(w['backends'])<=3 and all(x in BACKENDS for x in w['backends']),'WORKER_BACKENDS')
        for key in('lock_id','capacity_policy_sha','qualified_operations_sha256'):require(w[key]is None or type(w[key])is str and re.fullmatch('[0-9a-f]{64}',w[key])is not None,'WORKER_PIN')
        workers.append({k:w[k]for k in('worker_id','enabled','backends','lock_id','capacity_policy_sha','qualified_operations_sha256','last_seen','worker_fresh','node_fresh')})
    rows=[]
    for identity in identities:
        n=identity['operation'];attached=[v for operation,v in evidence.values()if operation==n]
        versions=sorted({version for operation,version in caps if operation==n}|{q['version']for q in attached})
        observations=[]
        for version in versions:
            cap=caps.get((n,version));references=[q for q in attached if q['version']==version]
            observations.append({'operation_version':version,'availability_authority':'existing_authenticated_versioned_capabilities_API'if cap else'not_reported_by_API','availability':cap,'evidence':references,'evidence_completeness':'references_attached_not_acceptance'if references else'no_project_qualification_reference','backends':[{'backend':b,'API_reports_supported':b in cap['supported_backends'],'API_reports_available':b in cap['available_backends'],'qualification_ids':[q['qualification_id']for q in references if q['backend']==b]}for b in sorted(BACKENDS)]if cap else[]})
        rows.append({**identity,'versions':observations,'availability_merge_performed':False,'API_identity_reported':any(operation==n for operation,version in caps)})
    return {'schema':1,'status':'READONLY_OPERATION_EVIDENCE_EXPORT_NOT_ADMISSION_OR_QUALIFICATION','tenant_id':tenant,'project_id':project,'scope_sha256':digest(scope),'capability_response_sha256':api_sha,'database_head':db['head'],'observed_authority_function_pins':db['authority_pins'],'database_observed_at':db['database_observed_at'],'capture_jointly_atomic':False,'availability_not_recomputed':True,'qualification_reverified':False,'identity_counts':counts,'operations':rows,'worker_observations':workers}

def fd_text(number,limit):
    require(number>=3,'PRIVATE_INPUT_FD')
    with os.fdopen(os.dup(number),'rb')as f:raw=f.read(limit+1)
    require(0<len(raw)<=limit and b'\n'not in raw and b'\0'not in raw,'PRIVATE_INPUT_BOUND')
    try:return raw.decode('utf8')
    except UnicodeError:raise Refused('PRIVATE_INPUT_ENCODING')from None

def write_new(path,raw):
    p=Path(path);require(p.is_absolute()and '..'not in p.parts,'OUTPUT_PATH')
    for parent in(p.parent,*p.parent.parents):
        s=parent.lstat();require(stat.S_ISDIR(s.st_mode)and s.st_uid in(0,os.getuid())and not s.st_mode&0o022,'OUTPUT_PARENT')
    require(len(raw)<=MAX_BYTES,'EXPORT_BOUND');fd=os.open(p,os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
    with os.fdopen(fd,'wb')as f:f.write(raw);f.flush();os.fsync(f.fileno())
    fd=os.open(p.parent,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:os.fsync(fd)
    finally:os.close(fd)

def main():
    p=argparse.ArgumentParser();p.add_argument('--scope',required=True,type=Path);p.add_argument('--tenant',required=True);p.add_argument('--project',required=True);p.add_argument('--api-origin',required=True);p.add_argument('--token-fd',required=True,type=int);p.add_argument('--observer-dsn-fd',required=True,type=int);p.add_argument('--observer-login',required=True);p.add_argument('--annotations',type=Path);p.add_argument('--annotations-sha256');p.add_argument('--output',required=True,type=Path);a=p.parse_args();require((a.annotations is None)==(a.annotations_sha256 is None),'ANNOTATION_PIN_REQUIRED');scope=read_file(a.scope);scope_rows(scope)
    annotations=None
    if a.annotations:
        raw=read_file(a.annotations);require(digest(raw)==a.annotations_sha256,'ANNOTATION_PIN');annotations=strict_json(raw)
    with deadline(30):
        token=fd_text(a.token_fd,4096);dsn=fd_text(a.observer_dsn_fd,16384)
        api,api_sha=api_get(a.api_origin,a.tenant,token)
        import psycopg
        with psycopg.connect(dsn,connect_timeout=5,autocommit=True)as db:observed=db_get(db,a.tenant,a.project,a.observer_login)
        report=assemble(scope,api,observed,a.tenant,a.project,api_sha,annotations)
        report['api_origin']=a.api_origin.rstrip('/')
        report['exporter_source_sha256']=digest(Path(__file__).read_bytes())
    raw=(json.dumps(report,sort_keys=True,indent=2)+'\n').encode();write_new(a.output,raw);print(json.dumps({'status':report['status'],'output_sha256':digest(raw),'identities':117,'availability_not_recomputed':True}))
if __name__=='__main__':
    try:main()
    except BaseException:print(json.dumps({'status':'EVIDENCE_EXPORT_REFUSED','no_retry':True}),file=sys.stderr);raise SystemExit(1)
