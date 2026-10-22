"""Explicit operator approval after real restore acceptance; no auto promotion."""
from datetime import datetime,timezone
from hashlib import sha256
import os
from pathlib import Path
from uuid import UUID,uuid4

from photo_worker.contracts.base import canonical_bytes,parse_json
from .backup_host import ComposeMaintenance,HostCommands,MUTATORS,private_file
from .backup_restore_operator import assert_isolated_compose,read_checkpoint
from .restore_recovery_load import load_pinned_restore_recovery,_decode
from .restore_acceptance import RestoreAcceptanceEvidence
from .restore_acceptance_report import verify_restore_acceptance_report
from .restore_report import RestoreReportReceipt
from .backup_operator_lock import restore_operator_locked


def _exclusive_event(root,name,body):
    import stat
    root=Path(root)
    if (not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir()
            or root.stat().st_mode&0o077 or root.stat().st_uid not in (0,os.geteuid())):
        raise ValueError('RESTORE_APPROVAL_EVENT_ROOT')
    if not isinstance(name,str) or name in ('','.','..') or Path(name).name!=name:
        raise ValueError('RESTORE_APPROVAL_EVENT_NAME')
    raw=canonical_bytes(body);path=root/name
    if len(raw)>16<<20:raise ValueError('RESTORE_APPROVAL_EVENT_SIZE')
    directory=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    pending='.'+name+'.'+uuid4().hex+'.pending'
    owned=False
    try:
        initial=os.fstat(directory)
        def bound():
            current=os.stat(root,follow_symlinks=False)
            if (not stat.S_ISDIR(current.st_mode) or current.st_mode&0o077
                    or current.st_uid not in (0,os.geteuid())
                    or (current.st_dev,current.st_ino)!=(initial.st_dev,initial.st_ino)):
                raise ValueError('RESTORE_APPROVAL_EVENT_ROOT_CHANGED')
        bound()
        descriptor=os.open(pending,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600,dir_fd=directory)
        owned=True
        with os.fdopen(descriptor,'wb') as stream:
            stream.write(raw);stream.flush();os.fsync(stream.fileno());written=os.fstat(stream.fileno())
        bound()
        try:os.link(pending,name,src_dir_fd=directory,dst_dir_fd=directory,follow_symlinks=False)
        except FileExistsError:
            bound()
            if private_file(path,maximum=16<<20)!=raw:raise ValueError('RESTORE_APPROVAL_EVENT_CONFLICT')
        else:
            published=os.stat(name,dir_fd=directory,follow_symlinks=False)
            if (published.st_dev,published.st_ino)!=(written.st_dev,written.st_ino):
                raise ValueError('RESTORE_APPROVAL_EVENT_PUBLICATION')
        os.unlink(pending,dir_fd=directory);owned=False
        os.fsync(directory);bound()
    finally:
        if owned:
            os.unlink(pending,dir_fd=directory)
            os.fsync(directory)
        os.close(directory)
    return path


def _source_stopped(commands,project):
    ids=commands.run('docker',['ps','--filter','label=com.docker.compose.project='+project,'--format','{{.ID}}']).decode().splitlines()
    import re
    if len(ids)>256 or any(re.fullmatch('[0-9a-f]{12,64}',i) is None for i in ids):
        raise ValueError('RESTORE_SOURCE_CONTAINER_PROJECTION')
    for identifier in ids:
        rows=parse_json(commands.run('docker',['inspect',identifier]),max_bytes=16<<20)
        labels=rows[0].get('Config',{}).get('Labels',{})
        if labels.get('com.docker.compose.project')!=project:raise ValueError('RESTORE_SOURCE_PROJECT')
        service=labels.get('com.docker.compose.service')
        if service not in {'postgres','minio','rabbitmq'}:
            raise ValueError('RESTORE_SOURCE_WRITERS_RUNNING')


def _stop_approved_target(commands,project):
    # No mutable Compose dependency during compensation. Stop every approved
    # target writer via exact immutable project labels, preserving all volumes.
    cleanup=HostCommands(commands.tools,check_active=lambda:True,timeout=120)
    ids=cleanup.run('docker',['ps','--filter','label=com.docker.compose.project='+project,'--format','{{.ID}}']).decode().splitlines()
    import re
    if len(ids)>256 or any(re.fullmatch('[0-9a-f]{12,64}',i) is None for i in ids):
        raise ValueError('RESTORE_CUTOVER_CLEANUP_PROJECTION')
    stop=[];unknown=False
    for identifier in ids:
        row=parse_json(cleanup.run('docker',['inspect',identifier]),max_bytes=16<<20)[0]
        labels=row.get('Config',{}).get('Labels',{})
        if labels.get('com.docker.compose.project')!=project:raise ValueError('RESTORE_CUTOVER_CLEANUP_SCOPE')
        if labels.get('com.docker.compose.service') in MUTATORS|{'postgres','minio','rabbitmq'}:stop.append(identifier)
        else:unknown=True
    if stop:cleanup.run('docker',['stop','--time','90',*stop])
    for identifier in stop:
        row=parse_json(cleanup.run('docker',['inspect',identifier]),max_bytes=16<<20)[0]
        if row.get('State',{}).get('Running') is not False:
            raise ValueError('RESTORE_CUTOVER_COMPENSATION_UNCONFIRMED')
    remaining=cleanup.run('docker',['ps','--filter','label=com.docker.compose.project='+project,'--format','{{.ID}}']).decode().splitlines()
    if unknown or remaining:raise ValueError('RESTORE_CUTOVER_COMPENSATION_UNCONFIRMED')


@restore_operator_locked()
def run_operator_restore_cutover(config_path,config_sha256,*,settings,connection,target,
        offhost,confirmation,check_active):
    """A separately reviewed closed approval config and explicit run-ID confirm.

    Actual acceptance/recovery/probe sources and current privacy authorities are
    re-read before an immutable approval event, then before/after enabling the
    approved runtime. Any ambiguous activation stops target services; no
    production data volume is deleted or rewritten by compensation.
    """
    from .backup_accept_operator import verify_operator_restore_authorities
    def active():
        if check_active() is False:raise RuntimeError('RESTORE_CUTOVER_INACTIVE')
        return True
    raw=private_file(config_path,maximum=2<<20)
    if sha256(raw).hexdigest()!=config_sha256:raise ValueError('RESTORE_CUTOVER_CONFIG_PIN')
    value=parse_json(raw,max_bytes=2<<20)
    keys={'schema_version','checkpoint_id','publication_version','checkpoint','recovery','replay','acceptance',
        'build_sha256','compose_file','compose_sha256','compose_rendered_sha256','approved_at','event_root'}
    if (not isinstance(value,dict) or set(value)!=keys or canonical_bytes(value)!=raw
            or value['schema_version']!='restore-cutover-approval/v1'
            or value['build_sha256']!=settings['build_sha256']):
        raise ValueError('RESTORE_CUTOVER_CONFIG')
    for field in ('checkpoint','recovery','replay','acceptance'):
        if not isinstance(value[field],dict) or set(value[field])!={'path','sha256'}:
            raise ValueError('RESTORE_CUTOVER_REPORT_PIN')
    approved=datetime.fromisoformat(value['approved_at'])
    if approved.tzinfo is None or approved>datetime.now(timezone.utc):raise ValueError('RESTORE_APPROVAL_TIME')
    evidence,replay=load_pinned_restore_recovery(value['recovery']['path'],value['recovery']['sha256'],
        value['replay']['path'],value['replay']['sha256'],build_sha256=settings['build_sha256'],check_active=active)
    if confirmation!=str(replay.run_id) or str(UUID(confirmation))!=confirmation:
        raise ValueError('RESTORE_EXPLICIT_OPERATOR_CONFIRMATION_REQUIRED')
    # Terminal compensation forbids reusing the old accepted run. A fresh
    # recovery/replay/acceptance and separately reviewed approval are required.
    terminal=Path(value['event_root'])/(str(replay.run_id)+'.compensated.json')
    try:terminal.lstat()
    except FileNotFoundError:pass
    else:raise ValueError('RESTORE_COMPENSATED_RUN_REUSE_FORBIDDEN')
    accepted_raw=private_file(value['acceptance']['path'],maximum=262144)
    if sha256(accepted_raw).hexdigest()!=value['acceptance']['sha256']:
        raise ValueError('RESTORE_CUTOVER_ACCEPTANCE_PIN')
    accepted=parse_json(accepted_raw,max_bytes=262144)
    if (not isinstance(accepted,dict) or set(accepted)!={'schema_version','writers_authorized','acceptance'}
            or accepted['schema_version']!='restore-acceptance-report/v1' or accepted['writers_authorized'] is not False
            or canonical_bytes(accepted)!=accepted_raw):
        raise ValueError('RESTORE_CUTOVER_ACCEPTANCE')
    acceptance=_decode(RestoreAcceptanceEvidence,accepted['acceptance'])
    if acceptance.replay!=replay or acceptance.finished_at>approved:
        raise ValueError('RESTORE_APPROVAL_ACCEPTANCE_BINDING')
    checkpoint=read_checkpoint(offhost,settings['offhost_bucket'],value['checkpoint_id'],value['publication_version'],
        settings['public_key_file'],settings['public_key_sha256'],active)
    materialized_raw=private_file(value['checkpoint']['path'],maximum=16<<20)
    materialized=parse_json(materialized_raw,max_bytes=16<<20)
    expected_materialized={'schema_version':'restore-materialization-receipt/v1','run_id':str(replay.run_id),
        'recovery_sha256':value['recovery']['sha256'],'manifest':checkpoint,
        'manifest_sha256':sha256(canonical_bytes(checkpoint)).hexdigest(),
        'target_project':settings['project'],'data_directory':settings['expected_data_directory']}
    if (sha256(materialized_raw).hexdigest()!=value['checkpoint']['sha256']
            or canonical_bytes(materialized)!=materialized_raw or materialized!=expected_materialized):
        raise ValueError('RESTORE_APPROVAL_MATERIALIZATION_BINDING')
    if (checkpoint['database']!=settings['database'] or checkpoint['system_identifier']!=settings['system_identifier']
            or checkpoint['source_project']==settings['project']
            or evidence.preflight.storage.database.database!=settings['database']
            or checkpoint['recovery_target']['lsn']!=parse_json(private_file(value['recovery']['path']),max_bytes=8<<20)['last_wal_lsn']):
        raise ValueError('RESTORE_APPROVAL_CHECKPOINT_BINDING')
    approved_target=ComposeMaintenance(target.commands,value['compose_file'],value['compose_sha256'],
        settings['project'],value['compose_rendered_sha256'])
    before=parse_json(target.command('config','--format','json'),max_bytes=16<<20)
    after=parse_json(approved_target.command('config','--format','json'),max_bytes=16<<20)
    services=set(after.get('services',{})); allowed=MUTATORS|{'postgres','minio','rabbitmq','migrate','bootstrap-storage','bootstrap-queue'}
    if services-allowed or not {'postgres','minio','api','scheduler','object-gateway','caddy'}.issubset(services):
        raise ValueError('RESTORE_CUTOVER_SERVICE_SCOPE')
    for service in ('postgres','minio'):
        a=before['services'][service];b=after['services'][service]
        if any(a.get(k)!=b.get(k) for k in ('image','volumes','environment','command','user','networks')):
            raise ValueError('RESTORE_CUTOVER_STORAGE_DRIFT')
    def current():
        active();_source_stopped(target.commands,checkpoint['source_project'])
        if sha256(private_file(config_path,maximum=2<<20)).hexdigest()!=config_sha256:
            raise ValueError('RESTORE_APPROVAL_CHANGED')
        if sha256(private_file(value['checkpoint']['path'],maximum=16<<20)).hexdigest()!=value['checkpoint']['sha256']:
            raise ValueError('RESTORE_MATERIALIZATION_CHANGED')
        with connection.transaction():
            connection.execute("SET LOCAL statement_timeout='5s'")
            identity=connection.execute("SELECT current_database(),system_identifier::text,current_setting('data_directory'),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user) FROM pg_control_system()").fetchone()
            if identity!=(settings['database'],settings['system_identifier'],settings['expected_data_directory'],True):
                raise ValueError('RESTORE_CUTOVER_DATABASE_IDENTITY')
        return True
    def isolated():
        current();assert_isolated_compose(target);return True
    def authorities():
        return verify_operator_restore_authorities(settings,evidence,connection,
            assert_isolation=isolated,check_active=active)
    verify_restore_acceptance_report(RestoreReportReceipt(Path(value['acceptance']['path']),value['acceptance']['sha256']),
        acceptance,value['recovery']['path'],value['replay']['path'],recovery_evidence=evidence,
        check_active=active,assert_probe_isolation=isolated,verify_current_authorities=authorities)
    event={'schema_version':'restore-operator-approval/v1','run_id':str(replay.run_id),
        'checkpoint_id':checkpoint['backup_id'],'acceptance_sha256':value['acceptance']['sha256'],
        'approval_config_sha256':config_sha256,'operator_uid':os.getuid(),'approved_at':value['approved_at'],
        'writers_authorized':True,'target_project':settings['project']}
    path=_exclusive_event(value['event_root'],str(replay.run_id)+'.approval.json',event)
    activation_event={**event,'schema_version':'restore-operator-cutover/v1','state':'active'}
    activation_recorded=False
    from psycopg import sql
    try:
        current();authorities()
        with connection.transaction():connection.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS true').format(sql.Identifier(settings['database'])))
        names=sorted(services & (MUTATORS|{'rabbitmq'}))
        approved_target.command('up','--detach','--no-deps','--wait','--wait-timeout','180',*names)
        current()
        verify_operator_restore_authorities(settings,evidence,connection,
            assert_isolation=current,check_active=active)
        # No storage re-creation: only approved app/outgoing services were
        # selected; the original isolated operator connection stays bound.
        _exclusive_event(value['event_root'],str(replay.run_id)+'.cutover.json',
            activation_event)
        activation_recorded=True
        active()
        return {'state':'active','run_id':str(replay.run_id),'approval_event':str(path)}
    except BaseException:
        stopped=False
        try:
            _stop_approved_target(target.commands,settings['project']);stopped=True
        finally:
            publication_state='confirmed' if activation_recorded else 'absent'
            if not activation_recorded:
                try:
                    actual=private_file(Path(value['event_root'])/(str(replay.run_id)+'.cutover.json'),maximum=16<<20)
                    publication_state='confirmed' if actual==canonical_bytes(activation_event) else 'conflict'
                except FileNotFoundError:pass
                except (OSError,ValueError):publication_state='unconfirmed'
            # Append a terminal withdrawal even when cancellation follows the
            # durable activation event. An activation record alone is not live
            # authority after compensation; never rewrite either prior event.
            _exclusive_event(value['event_root'],str(replay.run_id)+'.compensated.json',
                {**event,'schema_version':'restore-operator-compensation/v1',
                    'state':'compensated' if stopped else 'compensation_unconfirmed',
                    'writers_authorized':False,'stop_confirmed':stopped,
                    'approval_event_sha256':sha256(canonical_bytes(event)).hexdigest(),
                    'activation_event_sha256':sha256(canonical_bytes(activation_event)).hexdigest(),
                    'activation_publication_confirmed':publication_state=='confirmed',
                    'activation_publication_state':publication_state,
                    'reason':'RESTORE_CUTOVER_ABORTED'})
        raise
