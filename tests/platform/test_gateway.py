import hashlib
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
import json
from pathlib import Path
import ssl
import subprocess
from threading import Thread
from datetime import datetime,timedelta,timezone
from uuid import uuid4
import pytest
from photo_worker.runtime.gateway import Gateway,GatewayError,client_tls
from photo_worker.runtime.database import Lease
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.wire import TaskCommand


@pytest.fixture(scope='module')
def tls_files(tmp_path_factory):
    root=tmp_path_factory.mktemp('gateway-tls')
    def run(*args):
        subprocess.run(['openssl',*args],cwd=root,check=True,capture_output=True,timeout=15)
    run('req','-x509','-newkey','rsa:2048','-nodes','-days','1','-subj','/CN=PhotoEditor Test CA','-keyout','ca.key','-out','ca.pem')
    for name,usage in [('server','serverAuth'),('client','clientAuth')]:
        run('req','-new','-newkey','rsa:2048','-nodes','-subj','/CN='+name,'-keyout',name+'.key','-out',name+'.csr')
        (root/(name+'.ext')).write_text('basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage='+usage+'\nsubjectAltName=DNS:localhost,IP:127.0.0.1\n')
        run('x509','-req','-in',name+'.csr','-CA','ca.pem','-CAkey','ca.key','-CAcreateserial','-days','1','-extfile',name+'.ext','-out',name+'.pem')
        (root/(name+'.key')).chmod(0o600)
    return root


@pytest.fixture
def endpoint(tls_files):
    data=b'pixels'*10000
    state={'data':data,'sha':hashlib.sha256(data).hexdigest(),'artifact':uuid4(),'status':200,'uploaded':None,'peers':[]}
    class Handler(BaseHTTPRequestHandler):
        def log_message(self,*args):pass
        def do_GET(self):
            state['peers'].append(self.connection.getpeercert())
            self.send_response(state['status'])
            self.send_header('Content-Length',str(len(state['data'])))
            self.send_header('X-Photo-SHA256',state['sha'])
            self.send_header('X-Photo-Artifact-ID',str(state['artifact']))
            self.send_header('Location','https://must-not-follow.invalid/object')
            self.end_headers()
            try:self.wfile.write(state['data'])
            except OSError:pass
        def do_POST(self):
            state['credential_calls']=state.get('credential_calls',0)+1
            state['credential_body']=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
            payload=json.dumps(state['credentials']).encode()
            self.send_response(state.get('credential_status',200))
            self.send_header('Content-Type','application/json')
            self.send_header('Content-Length',str(len(payload)))
            self.end_headers();self.wfile.write(payload)
        def do_PUT(self):
            state['uploaded']=self.rfile.read(int(self.headers['Content-Length']))
            state['headers']=dict(self.headers)
            state['path']=self.path
            payload=json.dumps(dict(artifact_id=str(uuid4()),sha256=hashlib.sha256(state['uploaded']).hexdigest())).encode()
            self.send_response(201)
            self.send_header('Content-Type','application/json')
            self.send_header('Content-Length',str(len(payload)))
            self.end_headers();self.wfile.write(payload)
    server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
    context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version=ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(str(tls_files/'server.pem'),str(tls_files/'server.key'))
    context.load_verify_locations(str(tls_files/'ca.pem'))
    context.verify_mode=ssl.CERT_REQUIRED
    server.socket=context.wrap_socket(server.socket,server_side=True)
    thread=Thread(target=server.serve_forever,daemon=True);thread.start()
    try:yield f'https://127.0.0.1:{server.server_port}',state
    finally:server.shutdown();server.server_close();thread.join(timeout=3)


def gateway(tls_files,endpoint,**overrides):
    now=datetime.now(timezone.utc)
    command=TaskCommand.model_validate_json(json.dumps(dict(schema_version=1,event_id=str(uuid4()),tenant_id=str(uuid4()),run_id=str(uuid4()),task_id=str(uuid4()),attempt=1,operation='asset.verify',operation_version='1',resource_class='media',correlation_id=str(uuid4()),inputs=[],params=dict(artifact_id=str(uuid4()),sha256='a'*64),model=None,timeout_seconds=300,traceparent='')))
    lease=Lease(command,uuid4(),3,now+timedelta(seconds=60),now+timedelta(seconds=300),{},'b'*64)
    config=dict(origin=endpoint[0],lease=lease,tls=client_tls(tls_files/'ca.pem',tls_files/'client.pem',tls_files/'client.key'),token=lambda:'test.payload.signature',check_active=lambda:None,max_download_bytes=1000000,max_upload_bytes=1000000,allowed_output_roles=frozenset({'verified'}))
    config.update(overrides)
    return Gateway(**config)


def test_mtls_stream_and_upload(tls_files,endpoint,tmp_path):
    g=gateway(tls_files,endpoint);_,state=endpoint
    ref=ArtifactRef(artifact_id=state['artifact'],sha256=state['sha'])
    with g.open_stream(ref) as stream:
        chunks=[]
        while block:=stream.read(4096):chunks.append(block)
    assert b''.join(chunks)==state['data'] and state['peers'][0]
    path=tmp_path/'output.bin';path.write_bytes(b'normalized output')
    output=g.upload(path,'verified','application/octet-stream')
    assert output.sha256==hashlib.sha256(path.read_bytes()).hexdigest()
    assert state['uploaded']==path.read_bytes()
    assert state['headers']['X-Photo-Fence']=='3'
    assert state['headers']['Authorization']=='Bearer test.payload.signature'
    assert state['path'].endswith('/outputs/verified')


def test_no_redirect_or_unbounded_download(tls_files,endpoint):
    _,state=endpoint;ref=ArtifactRef(artifact_id=state['artifact'],sha256=state['sha'])
    state['status']=302
    with pytest.raises(GatewayError,match='RESPONSE_INVALID'):gateway(tls_files,endpoint).open_stream(ref)
    state['status']=200
    with pytest.raises(GatewayError,match='DISK_QUOTA'):gateway(tls_files,endpoint,max_download_bytes=2).open_stream(ref)
    state['sha']='f'*64
    with pytest.raises(GatewayError,match='HASH_MISMATCH'):gateway(tls_files,endpoint).open_stream(ref)


def test_tls_client_certificate_required(tls_files,endpoint):
    _,state=endpoint;ref=ArtifactRef(artifact_id=state['artifact'],sha256=state['sha'])
    context=ssl.create_default_context(cafile=str(tls_files/'ca.pem'))
    with pytest.raises(GatewayError,match='CONFIG_INVALID'):gateway(tls_files,endpoint,tls=context).open_stream(ref)


def test_upload_role_limit_and_lease_rejection(tls_files,endpoint,tmp_path):
    _,state=endpoint;g=gateway(tls_files,endpoint,max_upload_bytes=2)
    path=tmp_path/'output';path.write_bytes(b'large')
    with pytest.raises(ValueError):g.upload(path,'../other','image/png')
    with pytest.raises(GatewayError,match='DISK_QUOTA'):g.upload(path,'verified','image/png')
    state['status']=403
    ref=ArtifactRef(artifact_id=state['artifact'],sha256=state['sha'])
    with pytest.raises(GatewayError,match='LEASE_LOST'):g.open_stream(ref)


def test_task_credentials_refresh_and_identity(tls_files,endpoint):
    from photo_worker.runtime.credentials import TaskCredentials
    g=gateway(tls_files,endpoint);lease=g._lease;_,state=endpoint
    identity=dict(tenant_id=str(lease.command.tenant_id),run_id=str(lease.command.run_id),task_id=str(lease.command.task_id),
                  worker_id=str(lease.worker_id),attempt=lease.command.attempt,fence=str(lease.fence))
    state['credentials']=dict(token='test.payload.signature',token_type='Bearer',expires_at=(datetime.now(timezone.utc)+timedelta(seconds=30)).isoformat(),identity=identity)
    provider=TaskCredentials(endpoint[0],lease,g._tls,lambda:None)
    assert provider()=='test.payload.signature'
    assert provider()=='test.payload.signature' and state['credential_calls']==1
    assert state['credential_body']==dict(attempt=1,fence='3')
    provider.invalidate();assert provider()=='test.payload.signature' and state['credential_calls']==2
    provider.invalidate();state['credentials']['identity']['tenant_id']=str(uuid4())
    with pytest.raises(GatewayError,match='RESPONSE_INVALID'):provider()
    state['credentials']['identity']['tenant_id']=str(lease.command.tenant_id)
    state['credentials']['expires_at']=(datetime.now(timezone.utc)-timedelta(seconds=1)).isoformat()
    with pytest.raises(GatewayError,match='RESPONSE_INVALID'):provider()
    state['credential_status']=403
    with pytest.raises(GatewayError,match='LEASE_LOST'):provider()
    assert not provider._token


def test_task_artifacts_owned_files_and_cleanup(tls_files,endpoint,tmp_path):
    from photo_worker.contracts.base import Contract
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.runtime.executor import ExecutionContext
    from photo_worker.runtime.scratch import ScratchSpace,ScratchError
    class Manifest(Contract):
        width:int
    g=gateway(tls_files,endpoint);_,state=endpoint
    state['data']=b'{"width":42}';state['sha']=hashlib.sha256(state['data']).hexdigest()
    ref=ArtifactRef(artifact_id=state['artifact'],sha256=state['sha'])
    g._lease.command.params=ref
    ctx=ExecutionContext(g._lease)
    scratch=ScratchSpace(tmp_path,ctx.lease.command.tenant_id,ctx.lease.command.task_id,1,4096,min_free_bytes=0)
    directory=scratch.directory
    with TaskArtifacts(ctx,g,scratch) as artifacts:
        assert artifacts.params(Manifest).width==42
        with artifacts.input(ref,suffix='.json') as local:
            assert local.path.read_bytes()==state['data']
        with pytest.raises(ValueError,match='declared'):artifacts.input(ArtifactRef(artifact_id=uuid4(),sha256='a'*64))
        with artifacts.stream(ref) as streamed:
            assert streamed.read(65536)==state['data']
            assert streamed.read(65536)==b''
        assert streamed.closed
        with pytest.raises(ValueError,match='declared'):
            with artifacts.stream(ArtifactRef(artifact_id=uuid4(),sha256='a'*64)):
                pytest.fail('undeclared stream opened')
        with pytest.raises(RuntimeError,match='consumer stopped'):
            with artifacts.stream(ref) as aborted:
                assert aborted.read(1)==state['data'][:1]
                raise RuntimeError('consumer stopped')
        assert aborted.closed

        staged=artifacts.stage_contract(Manifest(width=42),'verified')
        assert staged.sha256==hashlib.sha256(b'{"width":42}').hexdigest()
        assert state['uploaded']==b'{"width":42}'
        outside=tmp_path/'outside.json';outside.write_bytes(b'{}')
        with scratch.reserve(2) as reserved:
            with pytest.raises(ScratchError,match='owned'):artifacts.stage(outside,reserved,'verified','application/json')
    assert not directory.exists() and outside.read_bytes()==b'{}'
    with pytest.raises(ScratchError,match='closed'):artifacts.params(Manifest)


def test_task_artifact_factory_requires_admitted_budget(tls_files,endpoint,tmp_path):
    from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
    from photo_worker.runtime.executor import ExecutionContext
    g=gateway(tls_files,endpoint);ctx=ExecutionContext(g._lease)
    config=ArtifactConfig(tmp_path,endpoint[0],g._tls,100,100,100)
    with pytest.raises(ValueError,match='Missing admitted'):TaskArtifacts.create(ctx,config,frozenset({'verified'}))
    ctx.lease.execution_profile['resource_estimate']=dict(scratch_bytes=101,output_bytes=100)
    with pytest.raises(ValueError,match='exceeds'):TaskArtifacts.create(ctx,config,frozenset({'verified'}))
    assert not list(tmp_path.iterdir())
