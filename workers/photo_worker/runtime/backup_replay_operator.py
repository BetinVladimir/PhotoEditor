"""Actual broker-only recovery replay; never grants writer/cutover approval."""
from contextlib import contextmanager
from hashlib import sha256
import hmac
from http.server import BaseHTTPRequestHandler
import os
from pathlib import Path
import re
import secrets
import signal
import socketserver
import subprocess
import tempfile
import threading
import time
from urllib.parse import urlsplit
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes,parse_json
from .backup_host import private_file,PinnedTool,ComposeMaintenance,HostCommands
from .backup_operator_lock import restore_operator_locked
from .backup_restore_operator import (assert_isolated_compose,assert_isolated_probe_compose,
    assert_probe_endpoint,stop_isolated_probe_phase,bind_operator_postgres)
from .restore_recovery_load import _decode
from .restore_recovery import RestoreComputeRecoveryEvidence
from .restore_replay_receipt import verify_restore_replay_files
from .restore_key_installation import _read_private


def load_replay_operator_config(path,pin,settings):
    if not isinstance(pin,str) or re.fullmatch('[0-9a-f]{64}',pin) is None:
        raise ValueError('RESTORE_REPLAY_OPERATOR_PIN')
    raw=private_file(path,maximum=16384)
    if sha256(raw).hexdigest()!=pin:raise ValueError('RESTORE_REPLAY_OPERATOR_PIN')
    value=parse_json(raw,max_bytes=16384)
    keys={'schema_version','binary','recovery','receipt_root','run_id','build_sha256','maximum_events','vhost'}
    if not isinstance(value,dict) or set(value)!=keys or value['schema_version']!='restore-replay-operator/v1':
        raise ValueError('RESTORE_REPLAY_OPERATOR_CONFIG')
    for name in ('binary','recovery'):
        if not isinstance(value[name],dict) or set(value[name])!={'path','sha256'}:
            raise ValueError('RESTORE_REPLAY_OPERATOR_CONFIG')
        if not isinstance(value[name]['sha256'],str) or re.fullmatch('[0-9a-f]{64}',value[name]['sha256']) is None:
            raise ValueError('RESTORE_REPLAY_OPERATOR_CONFIG')
    if (not isinstance(value['build_sha256'],str) or re.fullmatch('[0-9a-f]{64}',value['build_sha256']) is None
            or value['build_sha256']!=settings['build_sha256']
            or type(value['maximum_events']) is not int or not 1<=value['maximum_events']<=10000000
            or not isinstance(value['vhost'],str) or re.fullmatch('[a-zA-Z0-9_-]{1,128}',value['vhost']) is None
            or str(UUID(value['run_id']))!=value['run_id'] or UUID(value['run_id']).int==0):
        raise ValueError('RESTORE_REPLAY_OPERATOR_SCOPE')
    root=Path(value['receipt_root'])
    if (not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir()
            or root.stat().st_mode&0o077 or root.stat().st_uid!=os.geteuid()):
        raise ValueError('RESTORE_REPLAY_RECEIPT_ROOT')
    if os.path.lexists(root/(value['run_id']+'.replay.json')) or os.path.lexists(root/(value['run_id']+'.replay.pending')):
        raise ValueError('RESTORE_REPLAY_RECEIPT_EXISTS')
    return value


@contextmanager
def live_replay_authority(path,scope,assert_current):
    """Actual operator checks per request; no stored boolean readiness witness."""
    path=Path(path)
    parent=path.parent
    if (not path.is_absolute() or parent.resolve(strict=True)!=parent
            or not parent.is_dir() or parent.stat().st_mode&0o777!=0o700
            or parent.stat().st_uid!=os.geteuid() or not callable(assert_current)):
        raise ValueError('RESTORE_REPLAY_AUTHORITY_INSTALLATION')
    closing=threading.Event()
    class Handler(BaseHTTPRequestHandler):
        def log_message(self,*args):pass
        def do_POST(self):
            try:
                lengths=self.headers.get_all('Content-Length',[])
                length=lengths[0] if len(lengths)==1 else ''
                if (closing.is_set() or self.path!='/check' or not length.isdigit() or not 1<=int(length)<=1024
                        or self.headers.get('Content-Type')!='application/json'
                        or self.headers.get('Transfer-Encoding') is not None):
                    raise ValueError('RESTORE_REPLAY_AUTHORITY_REQUEST')
                value=parse_json(self.rfile.read(int(length)),max_bytes=1024)
                if (not isinstance(value,dict) or set(value)!={'token','run_id','database'}
                        or not isinstance(value['token'],str)
                        or not hmac.compare_digest(value['token'],scope['token'])
                        or value['run_id']!=scope['run_id'] or value['database']!=scope['database']):
                    raise ValueError('RESTORE_REPLAY_AUTHORITY_SCOPE')
                if assert_current() is False or closing.is_set():
                    raise ValueError('RESTORE_REPLAY_AUTHORITY_REFUSED')
                body=canonical_bytes({'authorized':True,'token':scope['token']})
                self.send_response(200);self.send_header('Content-Type','application/json')
                self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
            except Exception:
                try:
                    self.send_response(403);self.send_header('Content-Length','0');self.end_headers()
                except (BrokenPipeError,ConnectionError,OSError):pass
    class Server(socketserver.UnixStreamServer):
        def __init__(self,*args):
            self.handlers=set();self.handlers_lock=threading.Lock()
            self.capacity=threading.BoundedSemaphore(4)
            super().__init__(*args)
        def get_request(self):
            stream,address=super().get_request();stream.settimeout(5);return stream,address
        def process_request(self,request,address):
            # Slow/abandoned clients cannot allocate unbounded handler threads.
            # Exhaustion closes the connection; it never grants authority.
            if not self.capacity.acquire(blocking=False):
                self.shutdown_request(request)
                return
            def serve():
                try:self.finish_request(request,address)
                except Exception:pass
                finally:
                    try:self.shutdown_request(request)
                    finally:
                        with self.handlers_lock:self.handlers.discard(threading.current_thread())
                        self.capacity.release()
            worker=threading.Thread(target=serve,name='restore-replay-authority-request',daemon=True)
            with self.handlers_lock:self.handlers.add(worker)
            try:worker.start()
            except BaseException:
                with self.handlers_lock:self.handlers.discard(worker)
                self.capacity.release();self.shutdown_request(request)
                raise
    server=Server(str(path),Handler)
    thread=threading.Thread(target=server.serve_forever,kwargs={'poll_interval':.1},daemon=True)
    try:
        os.chmod(path,0o600);thread.start()
    except BaseException:
        server.server_close();path.unlink(missing_ok=True)
        raise
    try:yield
    finally:
        closing.set();server.shutdown();server.server_close();thread.join(timeout=2)
        deadline=time.monotonic()+10
        with server.handlers_lock:handlers=tuple(server.handlers)
        for handler in handlers:handler.join(timeout=max(0,deadline-time.monotonic()))
        Path(path).unlink(missing_ok=True)
        if thread.is_alive() or any(handler.is_alive() for handler in handlers):
            raise RuntimeError('RESTORE_REPLAY_AUTHORITY_NOT_STOPPED')


def run_pinned_replay_binary(binary,environment,*,assert_current,check_active,timeout=900):
    """No shell, inherited credentials, diagnostic disclosure or receipt synthesis."""
    if type(timeout) is not int or not 1<=timeout<=3600:
        raise ValueError('RESTORE_REPLAY_TIMEOUT_INSTALLATION')
    def guard():
        if check_active() is False:raise RuntimeError('RESTORE_REPLAY_INACTIVE')
        if assert_current() is False:raise RuntimeError('RESTORE_REPLAY_AUTHORITY_REFUSED')
        if check_active() is False:raise RuntimeError('RESTORE_REPLAY_INACTIVE')
    PinnedTool(**binary).verify();guard()
    with tempfile.TemporaryFile() as output:
        process=subprocess.Popen([binary['path']],stdin=subprocess.DEVNULL,stdout=output,
            stderr=output,start_new_session=True,env=environment)
        started=time.monotonic();next_guard=started
        try:
            while process.poll() is None:
                if check_active() is False:raise RuntimeError('RESTORE_REPLAY_INACTIVE')
                now=time.monotonic()
                if now-started>timeout:raise TimeoutError('RESTORE_REPLAY_TIMEOUT')
                if os.fstat(output.fileno()).st_size>16<<20:raise ValueError('RESTORE_REPLAY_OUTPUT_LIMIT')
                if now>=next_guard:guard();next_guard=now+2
                time.sleep(.1)
            if check_active() is False:raise RuntimeError('RESTORE_REPLAY_INACTIVE')
            if os.fstat(output.fileno()).st_size>16<<20:raise ValueError('RESTORE_REPLAY_OUTPUT_LIMIT')
            if process.returncode!=0:raise RuntimeError('RESTORE_REPLAY_PROCESS_FAILED')
            guard()
        finally:
            if process.poll() is None:
                try:os.killpg(process.pid,signal.SIGKILL)
                except ProcessLookupError:pass
                try:process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    raise RuntimeError('RESTORE_REPLAY_PROCESS_DID_NOT_STOP') from None


@restore_operator_locked()
def run_operator_restore_replay(config_path,config_sha256,*,settings,connection,target,check_active):
    """Control connection is superuser in a different DB of the isolated cluster.

    Starts only RabbitMQ, then the actual pinned Go queue bootstrap/replay roles.
    Finally stops broker and disables/terminates target connections on every exit.
    """
    from psycopg import sql
    def active():
        if check_active() is False:raise RuntimeError('RESTORE_REPLAY_INACTIVE')
        return True
    active();value=load_replay_operator_config(config_path,config_sha256,settings)
    raw=_read_private(value['recovery']['path'],8<<20,active)
    if sha256(raw).hexdigest()!=value['recovery']['sha256']:raise ValueError('RESTORE_REPLAY_RECOVERY_PIN')
    report=parse_json(raw,max_bytes=8<<20)
    report_keys={'schema_version','writers_authorized','build_sha256','backup_at','last_wal_lsn','started_at','finished_at','recovery'}
    if (not isinstance(report,dict) or set(report)!=report_keys or report['schema_version']!='restore-recovery-report/v1'
            or canonical_bytes(report)!=raw or report.get('build_sha256')!=value['build_sha256'] or report.get('writers_authorized') is not False):
        raise ValueError('RESTORE_REPLAY_RECOVERY_SCOPE')
    evidence=_decode(RestoreComputeRecoveryEvidence,report['recovery'])
    if (str(evidence.preflight.run_id)!=value['run_id'] or evidence.outbox.database!=settings['database']
            or Path(value['recovery']['path']).name!=value['run_id']+'.json'):
        raise ValueError('RESTORE_REPLAY_RECOVERY_SCOPE')
    dsn=os.environ.get('PHOTO_RESTORE_REPLAY_DATABASE_URL');broker=os.environ.get('PHOTO_RESTORE_REPLAY_AMQP_URL')
    if not dsn or not broker:raise ValueError('RESTORE_REPLAY_TRANSPORT_INSTALLATION')
    # Bind DB before enabling any connection. Hostaddr blocks libpq environment drift.
    database=bind_operator_postgres(settings,target,dsn)
    if database.get('sslmode')!='verify-full' or not database.get('user'):
        raise ValueError('RESTORE_REPLAY_TLS_DATABASE_REQUIRED')
    parsed=urlsplit(broker)
    if (parsed.scheme!='amqps' or parsed.path!='/'+value['vhost'] or parsed.query or parsed.fragment
            or not parsed.hostname or not parsed.username or not parsed.password):
        raise ValueError('RESTORE_REPLAY_TLS_BROKER_REQUIRED')
    assert_isolated_compose(target)
    if target.running()!={'postgres','minio'}:raise ValueError('RESTORE_REPLAY_STORAGE_PHASE')
    probe=ComposeMaintenance(target.commands,settings['probe_compose_file'],settings['probe_compose_sha256'],
        settings['project'],settings['probe_compose_rendered_sha256'])
    proposed=parse_json(probe.command('config','--format','json'),max_bytes=16<<20)
    current=parse_json(target.command('config','--format','json'),max_bytes=16<<20)
    assert_isolated_probe_compose(probe)
    for service in ('postgres','minio'):
        if any(current['services'][service].get(k)!=proposed['services'][service].get(k)
                for k in ('image','volumes','environment','command','user','networks')):
            raise ValueError('RESTORE_REPLAY_STORAGE_DRIFT')
    application='photo-restore-replay-'+value['run_id']
    database['application_name']=application
    from psycopg.conninfo import make_conninfo
    # Explicit PG options do not inherit a caller-selected search path/role.
    database.pop('options',None)
    replay_dsn=make_conninfo(**database)
    db_lock=threading.Lock()
    guard_scope=threading.local()
    def guard_active():
        active()
        if time.monotonic()>=getattr(guard_scope,'deadline',0):
            raise TimeoutError('RESTORE_REPLAY_AUTHORITY_TIMEOUT')
        return True
    guard_commands=HostCommands(target.commands.tools,check_active=guard_active,
        timeout=5,maximum_output=target.commands.maximum_output)
    guard_probe=ComposeMaintenance(guard_commands,settings['probe_compose_file'],
        settings['probe_compose_sha256'],settings['project'],settings['probe_compose_rendered_sha256'])
    locked=False
    def guard():
        guard_scope.deadline=time.monotonic()+8
        guard_active();assert_isolated_probe_compose(guard_probe)
        if guard_probe.running()!={'postgres','minio','rabbitmq'}:raise ValueError('RESTORE_REPLAY_CONSUMERS_RUNNING')
        host=parsed.hostname;host='['+host+']' if ':' in host else host
        assert_probe_endpoint(guard_probe,'amqps://'+host+':'+str(parsed.port or 5671),
            'rabbitmq',schemes={'amqps'},ports={5671})
        if not db_lock.acquire(timeout=1):raise TimeoutError('RESTORE_REPLAY_CONTROL_BUSY')
        try:
            guard_active()
            with connection.transaction():
                connection.execute("SET LOCAL statement_timeout='1s'")
                row=connection.execute("SELECT current_database(),system_identifier::text,(SELECT rolsuper FROM pg_roles WHERE rolname=current_user) FROM pg_control_system()").fetchone()
                if row[0]==settings['database'] or row[1:]!=(settings['system_identifier'],True):
                    raise ValueError('RESTORE_REPLAY_CONTROL_AUTHORITY')
                count,bad=connection.execute("""SELECT count(*),coalesce(bool_or(usename<>%s OR application_name<>%s),false)
                    FROM pg_stat_activity WHERE datname=%s AND backend_type='client backend'""",
                    (database['user'],application,settings['database'])).fetchone()
                if bad or count>2:raise ValueError('RESTORE_REPLAY_DATABASE_CONSUMERS')
        finally:db_lock.release()
        guard_active()
    try:
        with connection.transaction():
            row=connection.execute("SELECT current_database(),system_identifier::text,(SELECT rolsuper FROM pg_roles WHERE rolname=current_user) FROM pg_control_system()").fetchone()
            if row[0]==settings['database'] or row[1:]!=(settings['system_identifier'],True):
                raise ValueError('RESTORE_REPLAY_CONTROL_AUTHORITY')
            locked=connection.execute('SELECT pg_try_advisory_lock(1346916180,34)').fetchone()[0]
            if not locked:raise ValueError('RESTORE_REPLAY_OPERATOR_BUSY')
        probe.command('up','--detach','--no-deps','rabbitmq')
        guard()
        with connection.transaction():
            connection.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS true').format(sql.Identifier(settings['database'])))
        with tempfile.TemporaryDirectory(prefix='replay-',dir=settings['scratch_root']) as temporary:
            root=Path(temporary);socket=root/'authority.sock'
            if len(str(socket).encode())>100:raise ValueError('RESTORE_REPLAY_SOCKET_PATH_LIMIT')
            token=secrets.token_hex(32)
            installation={'schema_version':'restore-replay-process/v1','socket_path':str(socket),'socket_token':token,
                'database':settings['database'],'vhost':value['vhost'],'maximum_events':value['maximum_events'],
                'receipt_root':value['receipt_root'],'run_id':value['run_id'],
                'recovery_report_path':value['recovery']['path'],'recovery_sha256':value['recovery']['sha256'],
                'build_sha256':value['build_sha256']}
            payload=canonical_bytes(installation);path=root/'process.json'
            descriptor=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
            with os.fdopen(descriptor,'wb') as stream:stream.write(payload);stream.flush();os.fsync(stream.fileno())
            environment={'PATH':'/usr/bin:/bin','HOME':str(Path.home()),'LC_ALL':'C','APP_ENV':'development',
                'DATABASE_URL':replay_dsn,'AMQP_URL':broker,
                'PHOTO_RESTORE_REPLAY_INSTALLATION_FILE':str(path),
                'PHOTO_RESTORE_REPLAY_INSTALLATION_SHA256':sha256(payload).hexdigest()}
            with live_replay_authority(socket,{'token':token,'run_id':value['run_id'],'database':settings['database']},guard):
                run_pinned_replay_binary(value['binary'],{**environment,'PHOTO_ROLE':'bootstrap-queue'},assert_current=guard,check_active=active)
                run_pinned_replay_binary(value['binary'],{**environment,'PHOTO_ROLE':'restore-replay'},assert_current=guard,check_active=active)
            replay_path=Path(value['receipt_root'])/(value['run_id']+'.replay.json')
            receipt_raw=_read_private(replay_path,16384,active);receipt_sha=sha256(receipt_raw).hexdigest()
            verified=verify_restore_replay_files(value['recovery']['path'],value['recovery']['sha256'],
                replay_path,receipt_sha,evidence=evidence,build_sha256=value['build_sha256'],check_active=active)
            guard()
        return {'state':'replayed_isolated_writers_paused','writers_authorized':False,
            'run_id':str(verified.run_id),'report':str(replay_path),'sha256':receipt_sha,'confirmed':verified.confirmed}
    finally:
        try:
            # A competing operator owns its broker/database phase; never stop it.
            if locked:stop_isolated_probe_phase(settings,probe,connection)
        finally:
            if locked:
                with connection.transaction():connection.execute('SELECT pg_advisory_unlock(1346916180,34)')
