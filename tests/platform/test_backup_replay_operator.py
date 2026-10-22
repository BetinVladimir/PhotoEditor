"""Authored isolated replay bootstrap scenarios; no tools/restore are executed."""
from contextlib import contextmanager
from hashlib import sha256
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import backup_replay_operator as m


@contextmanager
def private_root():
    # AF_UNIX paths must fit macOS as well as Linux.
    with tempfile.TemporaryDirectory(prefix='rr-', dir='/tmp') as raw:
        path=Path(raw).resolve();path.chmod(0o700)
        yield path


def fixture_config(root):
    run=str(uuid4())
    return {'schema_version':'restore-replay-operator/v1',
        'binary':{'path':str(root/'photo'),'sha256':'a'*64},
        'recovery':{'path':str(root/(run+'.json')),'sha256':'b'*64},
        'receipt_root':str(root),'run_id':run,'build_sha256':'c'*64,
        'maximum_events':10,'vhost':'isolated-restore'}


@pytest.mark.parametrize('mode',['valid','wrong_pin','extra','boolean_limit','root_vhost','dangling_receipt'])
def test_replay_operator_config_independent_pin_and_closed_fields(mode):
    with private_root() as root:
        value=fixture_config(root)
        if mode=='extra':value['authorized']=True
        if mode=='boolean_limit':value['maximum_events']=True
        if mode=='root_vhost':value['vhost']='/'
        if mode=='dangling_receipt':(root/(value['run_id']+'.replay.json')).symlink_to(root/'absent')
        raw=canonical_bytes(value);path=root/'operator.json';path.write_bytes(raw);path.chmod(0o600)
        pin='0'*64 if mode=='wrong_pin' else sha256(raw).hexdigest()
        if mode=='valid':
            assert m.load_replay_operator_config(path,pin,{'build_sha256':'c'*64})==value
        else:
            with pytest.raises(ValueError):m.load_replay_operator_config(path,pin,{'build_sha256':'c'*64})


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self,path):
        super().__init__('localhost',timeout=2);self.path=path
    def connect(self):
        self.sock=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout);self.sock.connect(str(self.path))


@pytest.mark.parametrize('mode',['valid','false_guard','wrong_token','extra','duplicate','trailing','wrong_type','no_json_header'])
def test_live_authority_requires_actual_current_guard_and_exact_scope(mode):
    with private_root() as root:
        path=root/'a.sock';scope={'token':'a'*64,'run_id':str(uuid4()),'database':'restored'};calls=[]
        def guard():
            calls.append('live');return mode!='false_guard'
        body=dict(scope)
        if mode=='wrong_token':body['token']='b'*64
        if mode=='extra':body['authorized']=True
        if mode=='wrong_type':body['database']=True
        raw=canonical_bytes(body)
        if mode=='duplicate':raw=raw[:-1]+b',"database":"restored"}'
        if mode=='trailing':raw+=b' {}'
        with m.live_replay_authority(path,scope,guard):
            assert path.stat().st_mode&0o777==0o600
            client=UnixHTTP(path)
            headers={} if mode=='no_json_header' else {'Content-Type':'application/json'}
            if mode=='no_json_header':
                # The server intentionally rejects headers without reading a
                # forbidden body. Send this small negative request in one write
                # so a split http.client header/body send cannot race its close.
                from http.client import HTTPResponse
                client.connect()
                request=b'POST /check HTTP/1.1\r\nHost: localhost\r\nContent-Length: '+str(len(raw)).encode()+b'\r\n\r\n'+raw
                client.sock.sendall(request)
                response=HTTPResponse(client.sock);response.begin()
            else:
                client.request('POST','/check',body=raw,headers=headers)
                response=client.getresponse()
            payload=response.read();client.close()
            if mode=='valid':
                assert response.status==200 and json.loads(payload)=={'authorized':True,'token':scope['token']}
                assert calls==['live']
            else:
                assert response.status==403 and payload==b''
                assert calls==(['live'] if mode=='false_guard' else [])
        assert not path.exists()


class ReplayProcess:
    pid=4242
    def __init__(self,running=False,code=0):self.running=running;self.returncode=code;self.waits=[]
    def poll(self):return None if self.running else self.returncode
    def wait(self,timeout):self.waits.append(timeout);self.running=False;return self.returncode


def fake_launch(monkeypatch,process):
    calls=[]
    monkeypatch.setattr(m.PinnedTool,'verify',lambda self:None)
    monkeypatch.setattr(m.subprocess,'Popen',lambda args,**kw:calls.append((args,kw)) or process)
    return calls


def test_pinned_child_no_shell_no_inherited_environment_and_final_guard(monkeypatch):
    process=ReplayProcess();calls=fake_launch(monkeypatch,process);guards=[]
    environment={'PHOTO_ROLE':'restore-replay','DATABASE_URL':'redacted'}
    m.run_pinned_replay_binary({'path':'/pinned/photo','sha256':'a'*64},environment,
        assert_current=lambda:guards.append('current'),check_active=lambda:True)
    assert len(calls)==1 and calls[0][0]==['/pinned/photo']
    assert calls[0][1]['env'] is environment and calls[0][1]['start_new_session'] is True
    assert calls[0][1]['stdin']==subprocess.DEVNULL
    assert guards==['current','current']


@pytest.mark.parametrize('mode',['cancel','false_guard','bad_timeout'])
def test_child_refused_before_spawn(monkeypatch,mode):
    calls=fake_launch(monkeypatch,ReplayProcess())
    with pytest.raises((ValueError,RuntimeError)):
        m.run_pinned_replay_binary({'path':'/pinned/photo','sha256':'a'*64},{},
            assert_current=lambda:mode!='false_guard',check_active=lambda:mode!='cancel',
            timeout=True if mode=='bad_timeout' else 1)
    assert calls==[]


def test_child_cancellation_kills_group_and_waits_with_bound(monkeypatch):
    process=ReplayProcess(running=True);fake_launch(monkeypatch,process);kills=[];active=iter([True,True,False])
    monkeypatch.setattr(m.os,'killpg',lambda pid,sig:kills.append((pid,sig)))
    with pytest.raises(RuntimeError,match='RESTORE_REPLAY_INACTIVE'):
        m.run_pinned_replay_binary({'path':'/pinned/photo','sha256':'a'*64},{},
            assert_current=lambda:True,check_active=lambda:next(active))
    assert kills==[(process.pid,m.signal.SIGKILL)] and process.waits==[5]


def test_nonzero_child_diagnostics_are_not_exposed(monkeypatch):
    fake_launch(monkeypatch,ReplayProcess(code=1))
    with pytest.raises(RuntimeError,match='^RESTORE_REPLAY_PROCESS_FAILED$'):
        m.run_pinned_replay_binary({'path':'/pinned/photo','sha256':'a'*64},{},
            assert_current=lambda:True,check_active=lambda:True)


def test_child_timeout_kills_group_with_bounded_wait(monkeypatch):
    process=ReplayProcess(running=True);fake_launch(monkeypatch,process);kills=[]
    clock=iter([0,2]);monkeypatch.setattr(m.time,'monotonic',lambda:next(clock))
    monkeypatch.setattr(m.os,'killpg',lambda pid,sig:kills.append(pid))
    with pytest.raises(TimeoutError,match='RESTORE_REPLAY_TIMEOUT'):
        m.run_pinned_replay_binary({'path':'/pinned/photo','sha256':'a'*64},{},
            assert_current=lambda:True,check_active=lambda:True,timeout=1)
    assert kills==[process.pid] and process.waits==[5]


def test_late_current_authority_refusal_cannot_return_success(monkeypatch):
    fake_launch(monkeypatch,ReplayProcess());guards=iter([True,False])
    with pytest.raises(RuntimeError,match='RESTORE_REPLAY_AUTHORITY_REFUSED'):
        m.run_pinned_replay_binary({'path':'/pinned/photo','sha256':'a'*64},{},
            assert_current=lambda:next(guards),check_active=lambda:True)


def test_live_authority_shutdown_joins_inflight_bounded_guard():
    import threading
    with private_root() as root:
        path=root/'a.sock';scope={'token':'a'*64,'run_id':str(uuid4()),'database':'restored'}
        entered=threading.Event();release=threading.Event();results=[]
        def guard():
            entered.set()
            if not release.wait(1):raise TimeoutError('fixture guard timeout')
        def request():
            client=UnixHTTP(path)
            try:
                client.request('POST','/check',body=canonical_bytes(scope),headers={'Content-Type':'application/json'})
                response=client.getresponse();results.append(response.status);response.read()
            finally:client.close()
        with m.live_replay_authority(path,scope,guard):
            worker=threading.Thread(target=request);worker.start()
            assert entered.wait(1)
            timer=threading.Timer(.05,release.set);timer.start()
        worker.join(2);timer.join(2)
        assert not worker.is_alive() and not path.exists()
        assert results==[403]  # Closing authority cannot grant a late success.


def test_live_authority_refuses_nonprivate_parent_before_binding():
    with private_root() as root:
        root.chmod(0o755);path=root/'a.sock'
        with pytest.raises(ValueError,match='RESTORE_REPLAY_AUTHORITY_INSTALLATION'):
            with m.live_replay_authority(path,{},lambda:True):
                raise AssertionError('public authority installed')
        assert not path.exists()


def test_live_authority_refuses_duplicate_length_headers_without_current_check():
    with private_root() as root:
        path=root/'a.sock';scope={'token':'a'*64,'run_id':str(uuid4()),'database':'restored'};calls=[]
        body=canonical_bytes(scope)
        with m.live_replay_authority(path,scope,lambda:calls.append(True)):
            with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as stream:
                stream.settimeout(2);stream.connect(str(path))
                stream.sendall(b'POST /check HTTP/1.0\r\nContent-Type: application/json\r\nContent-Length: '+str(len(body)).encode()+b'\r\nContent-Length: '+str(len(body)).encode()+b'\r\n\r\n'+body)
                assert b' 403 ' in stream.recv(1024).split(b'\r\n')[0]
        assert calls==[]


def test_live_authority_bounds_concurrent_handlers_and_recovers_capacity():
    import threading
    from concurrent.futures import ThreadPoolExecutor
    with private_root() as root:
        path=root/'a.sock';scope={'token':'a'*64,'run_id':str(uuid4()),'database':'restored'}
        release=threading.Event();full=threading.Event();lock=threading.Lock();calls=0
        def guard():
            nonlocal calls
            with lock:
                calls+=1
                if calls==4:full.set()
            if not release.wait(8):raise RuntimeError('guard timed out')
            return True
        def request():
            client=UnixHTTP(path);client.timeout=10
            try:
                client.request('POST','/check',body=canonical_bytes(scope),headers={'Content-Type':'application/json'})
                response=client.getresponse();response.read();return response.status
            finally:client.close()
        with m.live_replay_authority(path,scope,guard),ThreadPoolExecutor(max_workers=4) as pool:
            pending=[pool.submit(request) for _ in range(4)]
            try:
                assert full.wait(5)
                with pytest.raises((http.client.HTTPException,ConnectionError,OSError)):
                    request()
                assert calls==4
            finally:release.set()
            assert [future.result(timeout=5) for future in pending]==[200]*4
            assert request()==200
