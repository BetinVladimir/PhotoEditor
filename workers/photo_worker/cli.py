from __future__ import annotations
import json,platform,sys
from pathlib import Path
from typing import Annotated
import typer
from photo_worker.contracts.style import StyleV2
app=typer.Typer(no_args_is_help=True)

@app.command('backup-create')
def backup_create(config:Path):
    """Pause installed services and publish an exact-version encrypted checkpoint.

    Host operator only. Failure intentionally leaves maintenance in place.
    Credentials are read from dedicated environment variables, never arguments.
    """
    import os, signal, threading
    import psycopg
    from photo_worker.runtime.backup_operator import load_operator_config, minio_client, create_checkpoint
    cancelled=threading.Event()
    previous={}
    def stop(signum,frame):cancelled.set()
    def active():return not cancelled.is_set()
    for name in (signal.SIGINT,signal.SIGTERM):
        previous[name]=signal.signal(name,stop)
    try:
        settings=load_operator_config(config)
        source=minio_client(settings['source_endpoint'],os.environ.get('PHOTO_BACKUP_SOURCE_ACCESS_KEY'),os.environ.get('PHOTO_BACKUP_SOURCE_SECRET_KEY'))
        offhost=minio_client(settings['offhost_endpoint'],os.environ.get('PHOTO_BACKUP_OFFHOST_ACCESS_KEY'),os.environ.get('PHOTO_BACKUP_OFFHOST_SECRET_KEY'))
        dsn=os.environ.get('PHOTO_BACKUP_DATABASE_URL')
        if not dsn:raise ValueError('BACKUP_DATABASE_INSTALLATION')
        with psycopg.connect(dsn,connect_timeout=5,autocommit=True) as connection:
            result=create_checkpoint(settings,connection,source,offhost,check_active=active)
        typer.echo(json.dumps(result))
    except Exception:
        typer.echo('Backup failed. Writers may remain paused; inspect operator state before resuming.',err=True)
        raise typer.Exit(1)
    finally:
        for name,handler in previous.items():signal.signal(name,handler)

@app.command('backup-restore')
def backup_restore(config:Path,backup_id:str,publication_version:str):
    """Materialize an authenticated checkpoint into isolated empty storage.

    Completes reconciliation without opening writers. Final probe/cutover is a
    separately authorized operator phase; a restored backup never grants it.
    """
    import os, signal, threading
    import psycopg
    from photo_worker.runtime.backup_operator import minio_client
    from photo_worker.runtime.backup_restore_operator import (
        load_restore_operator_config,read_checkpoint,restore_physical_checkpoint,reconcile_checkpoint,bind_operator_postgres,connect_restored_postgres)
    from photo_worker.runtime.backup_operator_lock import restore_operator_lease
    cancelled=threading.Event();previous={}
    def stop(signum,frame):cancelled.set()
    def active():
        if cancelled.is_set():raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
        return True
    try:
        for name in (signal.SIGINT,signal.SIGTERM):previous[name]=signal.signal(name,stop)
        settings=load_restore_operator_config(config)
        with restore_operator_lease(settings,check_active=active):
            remote=minio_client(settings['offhost_endpoint'],os.environ.get('PHOTO_BACKUP_OFFHOST_ACCESS_KEY'),os.environ.get('PHOTO_BACKUP_OFFHOST_SECRET_KEY'))
            manifest=read_checkpoint(remote,settings['offhost_bucket'],backup_id,publication_version,
                settings['public_key_file'],settings['public_key_sha256'],active)
            target=restore_physical_checkpoint(settings,manifest,remote,check_active=active)
            source=minio_client(settings['source_endpoint'],os.environ.get('PHOTO_RESTORE_SOURCE_ACCESS_KEY'),os.environ.get('PHOTO_RESTORE_SOURCE_SECRET_KEY'))
            restored_dsn=os.environ.get('PHOTO_RESTORE_DATABASE_URL')
            consent_dsn=os.environ.get('PHOTO_RESTORE_CONSENT_DATABASE_URL')
            if not restored_dsn or not consent_dsn or restored_dsn==consent_dsn:
                raise ValueError('RESTORE_DATABASE_INSTALLATION')
            restored_options=bind_operator_postgres(settings,target,restored_dsn)
            restored_options.pop('connect_timeout',None)
            restored_options['options']='-c statement_timeout=5000 -c lock_timeout=1000'
            restored_options.update(connect_timeout=5,autocommit=True)
            with connect_restored_postgres(restored_options,check_active=active) as connection:
                with psycopg.connect(consent_dsn,connect_timeout=5,autocommit=True,
                        options="-c statement_timeout=5000 -c lock_timeout=1000") as consents:
                    evidence,receipt=reconcile_checkpoint(settings,manifest,target,connection,consents,source,check_active=active)
            typer.echo(json.dumps({'state':'reconciled_writers_paused','writers_authorized':False,
                'run_id':str(evidence.preflight.run_id),'report':str(receipt.path),'sha256':receipt.sha256}))
    except Exception:
        typer.echo('Restore failed; isolated writers remain disabled. Preserve evidence and inspect operator state.',err=True)
        raise typer.Exit(1)
    finally:
        for name,handler in previous.items():signal.signal(name,handler)

@app.command('backup-prune')
def backup_prune(config:Path):
    """Withdraw expired checkpoints, then delete their exact encrypted versions."""
    import os
    from photo_worker.runtime.backup_operator import load_operator_config,minio_client,prune_operator_checkpoints
    try:
        settings=load_operator_config(config)
        client=minio_client(settings['offhost_endpoint'],os.environ.get('PHOTO_BACKUP_OFFHOST_ACCESS_KEY'),os.environ.get('PHOTO_BACKUP_OFFHOST_SECRET_KEY'))
        result=prune_operator_checkpoints(settings,client,check_active=lambda:True)
        typer.echo(json.dumps(result))
    except Exception:
        typer.echo('Retention failed; withdrawn checkpoints remain unavailable and cleanup can be retried.',err=True)
        raise typer.Exit(1)

@app.command('backup-accept')
def backup_accept(config:Path,accept_config:Path,accept_config_sha256:str):
    """Execute isolated acceptance using independently reviewed report/probe pins."""
    import os,signal,threading
    import psycopg
    from psycopg import sql
    from photo_worker.runtime.backup_restore_operator import load_restore_operator_config,bind_operator_postgres
    from photo_worker.runtime.backup_host import HostCommands,PinnedTool,ComposeMaintenance
    from photo_worker.runtime.backup_accept_operator import run_operator_restore_acceptance
    from photo_worker.runtime.backup_operator_lock import restore_operator_lease
    from contextlib import ExitStack
    lease=ExitStack()
    cancelled=threading.Event();previous={}
    def stop(signum,frame):cancelled.set()
    def active():
        if cancelled.is_set():raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
        return True
    try:
        for name in (signal.SIGINT,signal.SIGTERM):previous[name]=signal.signal(name,stop)
        settings=load_restore_operator_config(config)
        lease.enter_context(restore_operator_lease(settings,check_active=active))
        commands=HostCommands({k:PinnedTool(**v) for k,v in settings['tools'].items()},check_active=active)
        target=ComposeMaintenance(commands,settings['compose_file'],settings['compose_sha256'],settings['project'],settings['compose_rendered_sha256'])
        dsn=os.environ.get('PHOTO_RESTORE_DATABASE_URL');control_dsn=os.environ.get('PHOTO_RESTORE_CONTROL_DATABASE_URL')
        if not dsn or not control_dsn:raise ValueError('RESTORE_ACCEPT_DATABASE_INSTALLATION')
        restored=bind_operator_postgres(settings,target,dsn)
        control=bind_operator_postgres(settings,target,control_dsn,control=True)
        restored.pop('connect_timeout',None);control.pop('connect_timeout',None)
        for options in (restored, control):
            options['options']='-c statement_timeout=5000 -c lock_timeout=1000'
        with psycopg.connect(**control,connect_timeout=5,autocommit=True) as administrator:
            row=administrator.execute("SELECT system_identifier::text,(SELECT rolsuper FROM pg_roles WHERE rolname=current_user) FROM pg_control_system()").fetchone()
            if row!=(settings['system_identifier'],True):raise ValueError('RESTORE_ACCEPT_CONTROL_AUTHORITY')
            try:
                administrator.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS true').format(sql.Identifier(settings['database'])))
                with psycopg.connect(**restored,connect_timeout=5,autocommit=True) as connection:
                    administrator.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS false').format(sql.Identifier(settings['database'])))
                    result=run_operator_restore_acceptance(accept_config,accept_config_sha256,
                        settings=settings,connection=connection,client=None,target=target,check_active=active,
                        cleanup_connection=administrator)
                active()
            finally:
                administrator.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS false').format(sql.Identifier(settings['database'])))
        acceptance,receipt=result
        typer.echo(json.dumps({'state':'accepted_isolated_writers_paused','writers_authorized':False,
            'run_id':str(acceptance.replay.run_id),'report':str(receipt.path),'sha256':receipt.sha256}))
    except Exception:
        typer.echo('Acceptance failed; production cutover is not authorized.',err=True);raise typer.Exit(1)
    finally:
        try:
            lease.close()
        finally:
            for name,handler in previous.items():signal.signal(name,handler)

@app.command('backup-cutover')
def backup_cutover(config:Path,approval_config:Path,approval_config_sha256:str,confirm_run_id:str):
    """Activate a reviewed restoration only after explicit run-ID approval."""
    import os,signal,threading
    from contextlib import ExitStack
    import psycopg
    from psycopg import sql
    from photo_worker.runtime.backup_restore_operator import load_restore_operator_config,bind_operator_postgres
    from photo_worker.runtime.backup_host import HostCommands,PinnedTool,ComposeMaintenance
    from photo_worker.runtime.backup_operator import minio_client
    from photo_worker.runtime.backup_operator_lock import restore_operator_lease
    from photo_worker.runtime.backup_cutover_operator import run_operator_restore_cutover
    cancelled=threading.Event();previous={};lease=ExitStack();activated=False
    def stop(signum,frame):cancelled.set()
    def active():
        if cancelled.is_set():raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
        return True
    try:
        for name in (signal.SIGINT,signal.SIGTERM):previous[name]=signal.signal(name,stop)
        settings=load_restore_operator_config(config)
        lease.enter_context(restore_operator_lease(settings,check_active=active))
        commands=HostCommands({k:PinnedTool(**v) for k,v in settings['tools'].items()},check_active=active)
        target=ComposeMaintenance(commands,settings['compose_file'],settings['compose_sha256'],settings['project'],settings['compose_rendered_sha256'])
        dsn=os.environ.get('PHOTO_RESTORE_DATABASE_URL');control_dsn=os.environ.get('PHOTO_RESTORE_CONTROL_DATABASE_URL')
        if not dsn or not control_dsn:raise ValueError('RESTORE_CUTOVER_DATABASE_INSTALLATION')
        restored=bind_operator_postgres(settings,target,dsn)
        control=bind_operator_postgres(settings,target,control_dsn,control=True)
        restored.pop('connect_timeout',None);control.pop('connect_timeout',None)
        for options in (restored,control):
            options['options']='-c statement_timeout=5000 -c lock_timeout=1000'
        remote=minio_client(settings['offhost_endpoint'],os.environ.get('PHOTO_BACKUP_OFFHOST_ACCESS_KEY'),os.environ.get('PHOTO_BACKUP_OFFHOST_SECRET_KEY'))
        with psycopg.connect(**control,connect_timeout=5,autocommit=True) as administrator:
            row=administrator.execute("SELECT system_identifier::text,(SELECT rolsuper FROM pg_roles WHERE rolname=current_user) FROM pg_control_system()").fetchone()
            if row!=(settings['system_identifier'],True):raise ValueError('RESTORE_CUTOVER_CONTROL_AUTHORITY')
            try:
                administrator.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS true').format(sql.Identifier(settings['database'])))
                with psycopg.connect(**restored,connect_timeout=5,autocommit=True) as connection:
                    administrator.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS false').format(sql.Identifier(settings['database'])))
                    result=run_operator_restore_cutover(approval_config,approval_config_sha256,
                        settings=settings,connection=connection,target=target,offhost=remote,
                        confirmation=confirm_run_id,check_active=active)
                    activated=True
            finally:
                if not activated:
                    administrator.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS false').format(sql.Identifier(settings['database'])))
        typer.echo(json.dumps(result))
    except Exception:
        typer.echo('Cutover did not complete; preserve approval and restoration evidence.',err=True);raise typer.Exit(1)
    finally:
        try:
            lease.close()
        finally:
            for name,handler in previous.items():signal.signal(name,handler)

@app.command('backup-replay')
def backup_replay(config:Path,replay_config:Path,replay_config_sha256:str):
    """Run the pinned Go outbox replay in a broker-only isolated phase."""
    import os,signal,threading
    import psycopg
    from photo_worker.runtime.backup_restore_operator import load_restore_operator_config,bind_operator_postgres
    from photo_worker.runtime.backup_host import HostCommands,PinnedTool,ComposeMaintenance
    from photo_worker.runtime.backup_replay_operator import run_operator_restore_replay
    cancelled=threading.Event();previous={}
    def stop(signum,frame):cancelled.set()
    def active():
        if cancelled.is_set():raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
        return True
    try:
        for name in (signal.SIGINT,signal.SIGTERM):previous[name]=signal.signal(name,stop)
        settings=load_restore_operator_config(config)
        commands=HostCommands({k:PinnedTool(**v) for k,v in settings['tools'].items()},check_active=active)
        target=ComposeMaintenance(commands,settings['compose_file'],settings['compose_sha256'],settings['project'],settings['compose_rendered_sha256'])
        dsn=os.environ.get('PHOTO_RESTORE_CONTROL_DATABASE_URL')
        if not dsn:raise ValueError('RESTORE_REPLAY_CONTROL_INSTALLATION')
        control=bind_operator_postgres(settings,target,dsn,control=True);control.pop('connect_timeout',None)
        control['options']='-c statement_timeout=5000 -c lock_timeout=1000'
        with psycopg.connect(**control,connect_timeout=5,autocommit=True) as connection:
            row=connection.execute("SELECT system_identifier::text,(SELECT rolsuper FROM pg_roles WHERE rolname=current_user) FROM pg_control_system()").fetchone()
            if row!=(settings['system_identifier'],True):raise ValueError('RESTORE_REPLAY_CONTROL_AUTHORITY')
            result=run_operator_restore_replay(replay_config,replay_config_sha256,
                settings=settings,connection=connection,target=target,check_active=active)
        active();typer.echo(json.dumps(result))
    except Exception:
        typer.echo('Replay failed; isolated writers remain disabled.',err=True);raise typer.Exit(1)
    finally:
        for name,handler in previous.items():signal.signal(name,handler)

@app.command('backup-status')
def backup_status(config:Path):
    """Report signed checkpoint freshness; this is not an RPO drill result."""
    from photo_worker.runtime.backup_operator import load_operator_config,checkpoint_freshness
    try:result=checkpoint_freshness(load_operator_config(config),check_active=lambda:True)
    except Exception:
        typer.echo('Backup freshness unavailable',err=True);raise typer.Exit(1)
    typer.echo(json.dumps(result))
    if not result['fresh']:raise typer.Exit(1)

@app.command('validate-config')
def validate_config(path:Path,allow_example:bool=False):
    """Validate a portable style without touching source images."""
    try:style=StyleV2.from_bytes(path.read_bytes(),allow_example=allow_example)
    except (OSError,ValueError) as exc:
        typer.echo(str(exc),err=True);raise typer.Exit(2)
    typer.echo(json.dumps({'valid':True,'config_hash':style.config_hash,'mode':style.mode}))

@app.command()
def doctor(model_root:Path|None=typer.Option(None),model_lock:Path|None=typer.Option(None),backend:str='cpu',output:Path|None=typer.Option(None)):
    """Report installed runtime facts, never claim unexecuted model/backend tests."""
    import importlib.metadata
    modules=['numpy','scipy','pydantic','rfc8785','psycopg','pika','minio','torch','pyvips','rawpy']
    versions={}
    for name in modules:
        try:versions[name]=importlib.metadata.version(name)
        except importlib.metadata.PackageNotFoundError:versions[name]=None
    result={'python':platform.python_version(),'platform':platform.platform(),
        'packages':versions,'model_compatibility':'not_measured','python_target_met':sys.version_info[:2]==(3,11)}
    if (model_root is None)!=(model_lock is None):
        typer.echo('model-root and model-lock must be supplied together',err=True);raise typer.Exit(2)
    if model_root is not None:
        from photo_worker.models.doctor import run,write_report
        try:report=run(model_root,model_lock,backend)
        except Exception:
            typer.echo('Model doctor failed; inspect the canonical report on a trusted host',err=True);raise typer.Exit(1)
        active=[x for x in report['models'] if x['status']!='unsupported']
        if any(x['status']=='failed' for x in active):
            result['model_compatibility']='failed'
        elif not active:
            result['model_compatibility']='unsupported'
        elif all(x['status']=='passed' and x['lock_backend_status']=='passed' for x in active):
            result['model_compatibility']='ready'
        else:
            result['model_compatibility']='unqualified'
        result['model_report_sha256']=report['report_sha256'];result['model_lock_id']=report['lock_id'];result['backend']=backend
        if output is not None:write_report(output,report)
    typer.echo(json.dumps(result,indent=2))
    if result['model_compatibility']=='failed':raise typer.Exit(1)

@app.command('models-bundle')
def models_bundle(model_lock:Path,model_root:Path,destination:Path,model_id:Annotated[list[str],typer.Option('--model-id')]):
    """Export selected verified weights for an offline installation."""
    from photo_worker.models.bundle import create_bundle
    try:manifest=create_bundle(model_lock,model_root,model_id,destination)
    except (OSError,ValueError):
        typer.echo('Model bundle rejected; check the lock, installed hashes and destination',err=True)
        raise typer.Exit(2)
    typer.echo(json.dumps({'bundle':str(destination.absolute()),'lock_id':manifest['lock_id'],
                           'models':[item['id'] for item in manifest['models']]}))

@app.command('models-verify-bundle')
def models_verify_bundle(bundle:Path):
    """Verify every file in an offline bundle without installing or loading models."""
    from photo_worker.models.bundle import verify_bundle
    try:manifest=verify_bundle(bundle)
    except (OSError,ValueError):
        typer.echo('Model bundle verification failed',err=True)
        raise typer.Exit(2)
    typer.echo(json.dumps({'valid':True,'lock_id':manifest['lock_id'],
                           'models':[item['id'] for item in manifest['models']]}))

@app.command('capacity-policy')
def capacity_policy(path:Path):
    """Validate a node capacity policy and print its canonical SHA-256."""
    from photo_worker.runtime.capacity_policy import load_capacity_policy
    try:policy=load_capacity_policy(path)
    except (OSError,ValueError):
        typer.echo('Invalid capacity policy',err=True)
        raise typer.Exit(2)
    typer.echo(json.dumps({'valid':True,'sha256':policy.sha256}))

@app.command('run')
def run():
    """Consume the provisioned worker queue using installed atomic operations."""
    from photo_worker.runtime.service import WorkerConfig,WorkerConfigurationError,run_worker
    try:
        from photo_worker.runtime.semantic_assembly_config import semantic_assembly_from_environment
        config=WorkerConfig.from_env()
        assembly=semantic_assembly_from_environment(config)
        from photo_worker.runtime.exposure_config import local_exposure_from_environment
        exposure=local_exposure_from_environment(config)
        from photo_worker.runtime.preview_config import exposure_preview_from_environment
        exposure_preview=exposure_preview_from_environment(config)
        from photo_worker.runtime.shown_exposure_config import shown_exposure_from_environment
        shown_exposure=shown_exposure_from_environment(config)
        from photo_worker.runtime.removal_preview_config import removal_preview_from_environment
        removal_preview=removal_preview_from_environment(config)
        from photo_worker.runtime.background_cleanup_preview_config import background_cleanup_preview_from_environment
        background_cleanup_preview=background_cleanup_preview_from_environment(config)
        from photo_worker.runtime.retouch_preview_config import retouch_preview_from_environment
        retouch_preview=retouch_preview_from_environment(config)
        from photo_worker.runtime.shown_removal_config import shown_removal_from_environment
        shown_removal=shown_removal_from_environment(config)
        from photo_worker.runtime.shown_background_cleanup_config import shown_background_cleanup_from_environment
        shown_background_cleanup=shown_background_cleanup_from_environment(config)
        from photo_worker.runtime.shown_retouch_config import shown_retouch_from_environment
        shown_retouch=shown_retouch_from_environment(config)
        from photo_worker.runtime.donor_config import donor_installation_from_environment
        donor=donor_installation_from_environment(config)
        from photo_worker.runtime.negative_config import negative_installation_from_environment
        negative=negative_installation_from_environment(config)
        from photo_worker.runtime.relight_config import relight_installation_from_environment
        relight=relight_installation_from_environment(config)
        from photo_worker.runtime.local_portrait_config import local_portrait_installation_from_environment
        local_portrait=local_portrait_installation_from_environment(config)
        from photo_worker.runtime.reflection_config import reflection_installation_from_environment
        reflection=reflection_installation_from_environment(config)
        from photo_worker.runtime.glare_config import glare_donor_installation_from_environment
        glare=glare_donor_installation_from_environment(config)
        from photo_worker.runtime.perspective_config import perspective_installation_from_environment
        perspective=perspective_installation_from_environment(config)
        from photo_worker.runtime.perspective_lines_config import perspective_lines_installation_from_environment
        perspective_lines=perspective_lines_installation_from_environment(config)
        from photo_worker.runtime.multi_config import multi_installation_from_environment
        multi=tuple(item for operation in ('optical.panorama','optical.focus_stack')
            if (item:=multi_installation_from_environment(config,operation)) is not None)
        from photo_worker.runtime.learning_config import learning_evaluation_from_environment
        learning_evaluation=learning_evaluation_from_environment(config)
        from photo_worker.runtime.native_render_config import native_render_from_environment
        native_render=native_render_from_environment(config)
        from photo_worker.runtime.semantic_scene_config import semantic_scene_from_environment
        semantic_scene=semantic_scene_from_environment(config)
        installations={}
        if semantic_scene is not None:installations['semantic_artifact_installation_factory']=semantic_scene
        if native_render is not None:installations['render_renderer']=native_render
        if learning_evaluation is not None:
            installations['learning_evaluation_installation']=learning_evaluation
        if multi:installations['multi_installations']=multi
        if perspective_lines is not None:installations['perspective_lines_installation']=perspective_lines
        if perspective is not None:installations['perspective_installation']=perspective
        if relight is not None:installations['relight_installation']=relight
        if local_portrait is not None:installations['local_portrait_installation']=local_portrait
        if reflection is not None:installations['reflection_installation']=reflection
        if glare is not None:installations['glare_installation']=glare
        if negative is not None:installations['negative_installation']=negative
        if donor is not None:installations['donor_installation']=donor
        if assembly is not None:installations['semantic_assembly_installation_factory']=assembly
        if shown_exposure is not None:installations['shown_exposure_installation_factory']=shown_exposure
        if exposure_preview is not None:installations['exposure_preview_installation_factory']=exposure_preview
        if removal_preview is not None:installations['removal_preview_installation_factory']=removal_preview
        if background_cleanup_preview is not None:installations['background_cleanup_preview_installation_factory']=background_cleanup_preview
        if retouch_preview is not None:installations['retouch_preview_startup']=retouch_preview
        if shown_removal is not None:installations['shown_removal_installation_factory']=shown_removal
        if shown_background_cleanup is not None:installations['shown_background_cleanup_installation_factory']=shown_background_cleanup
        if shown_retouch is not None:installations['shown_retouch_installation_factory']=shown_retouch
        if exposure is not None:installations['local_exposure_installation_factory']=exposure
        run_worker(config,**installations)
    except WorkerConfigurationError as exc:
        typer.echo(str(exc),err=True);raise typer.Exit(2)
    except Exception:
        # Broker/DB exceptions may contain credentials. Never print their text.
        typer.echo('Worker stopped: configuration, dependency or shutdown failure',err=True)
        raise typer.Exit(1)

@app.command('verify-region-runtime')
def verify_region_runtime(path: Path, expected_sha: str):
    """Verify externally pinned Region build bytes without claiming qualification."""
    from photo_worker.operations.region.runtime_lock import load_region_runtime_lock
    from photo_worker.runtime.executor import OperationFailure
    try:
        lock = load_region_runtime_lock(path, expected_sha)
    except (OSError, ValueError, OperationFailure):
        typer.echo('Region runtime verification failed', err=True)
        raise typer.Exit(2)
    typer.echo(json.dumps({'valid': True, 'sha256': expected_sha,
        'components': len(lock.components), 'qualification': 'not_measured'}))


@app.command('verify-reflection-runtime')
def verify_reflection_runtime(path: Path, expected_sha: str, backend: str):
    """Verify installed reflection code without asserting model qualification."""
    from photo_worker.operations.image.reflection_runtime_lock import load_reflection_runtime_lock
    try:
        lock = load_reflection_runtime_lock(path, expected_sha, backend)
    except (OSError, ValueError):
        typer.echo('Reflection runtime verification failed', err=True)
        raise typer.Exit(2)
    typer.echo(json.dumps({'valid': True, 'sha256': expected_sha,
        'backend': lock.backend, 'components': len(lock.components),
        'qualification': 'not_measured'}, sort_keys=True))


@app.command('verify-glare-donor-runtime')
def verify_glare_donor_runtime(path: Path, expected_sha: str):
    """Verify donor-glare code bytes, not corpus or model qualification."""
    from photo_worker.operations.portrait.glare_donor_runtime_lock import load_glare_donor_runtime_lock
    try:
        lock = load_glare_donor_runtime_lock(path, expected_sha)
    except (OSError, ValueError):
        typer.echo('Donor glare runtime verification failed', err=True)
        raise typer.Exit(2)
    typer.echo(json.dumps({'valid': True, 'sha256': expected_sha,
        'components': len(lock.components), 'qualification': 'not_measured'},
        sort_keys=True))


@app.command('v2-adopter')
def v2_adopter():
    """Adopt verified V2 result closures and finalize eligible V2 runs."""
    import signal
    from threading import Event
    from photo_worker.v2.service import AdopterConfig,run_adopter
    stop=Event()
    signal.signal(signal.SIGTERM,lambda *_:stop.set())
    signal.signal(signal.SIGINT,lambda *_:stop.set())
    try:
        from photo_worker.v2.portrait_donor_adopter_config import donor_adopter_from_environment
        donor=donor_adopter_from_environment()
        options={} if donor is None else {'donor_installation':donor}
        from photo_worker.v2.negative_adopter_config import negative_adopter_from_environment
        negative=negative_adopter_from_environment()
        if negative is not None:options['negative_installation']=negative
        from photo_worker.v2.relight_adopter_config import relight_adopter_from_environment
        relight=relight_adopter_from_environment()
        if relight is not None:options['relight_installation']=relight
        from photo_worker.v2.perspective_adopter_config import perspective_adopter_from_environment
        perspective=perspective_adopter_from_environment()
        from photo_worker.v2.perspective_lines_adopter_config import perspective_lines_adopter_from_environment
        perspective_lines=perspective_lines_adopter_from_environment()
        if perspective_lines is not None:options['perspective_lines_installation']=perspective_lines
        if perspective is not None:options['perspective_installation']=perspective
        from photo_worker.v2.multi_adopter_config import multi_adopter_from_environment
        multi=tuple(item for operation in ('optical.panorama','optical.focus_stack')
            if (item:=multi_adopter_from_environment(operation)) is not None)
        if multi:options['multi_installations']=multi
        from photo_worker.v2.interchange_import_adopter_config import import_adopter_operator_from_environment
        import_operator=import_adopter_operator_from_environment()
        if import_operator is not None:
            options['interchange_recipe_operator_factory']=import_operator
        adopter_config=AdopterConfig.from_env()
        from photo_worker.v2.semantic_scene_config import semantic_scene_adopter_from_environment
        semantic_scene=semantic_scene_adopter_from_environment(adopter_config)
        if semantic_scene is not None:
            options['semantic_scene_verifier'],options['semantic_ram_bytes']=semantic_scene
        from photo_worker.runtime.native_render_config import native_render_from_environment
        native_render=native_render_from_environment(adopter_config,role='photo_scheduler')
        if native_render is not None:
            options['render_replay']=native_render
            options['render_max_tile_bytes']=native_render.tile_bytes
        run_adopter(adopter_config,stop,**options)
    except ValueError:
        typer.echo('V2 adopter configuration invalid',err=True);raise typer.Exit(2)
    except Exception:
        # SQL and S3 errors may embed credentials or signed request details.
        typer.echo('V2 adopter stopped: dependency or runtime failure',err=True)
        raise typer.Exit(1)



@app.command('signer-verify-sdk-wheel')
def signer_verify_sdk_wheel(path: Path):
    """Verify the approved SDK distribution offline; do not install or qualify it."""
    from dataclasses import asdict
    from photo_worker.runtime.signer_wheel import verify_signer_sdk_wheel
    try:
        receipt = verify_signer_sdk_wheel(path)
    except (OSError, ValueError):
        typer.echo('Signer SDK wheel rejected: check the approved digest, path and file permissions', err=True)
        raise typer.Exit(2)
    typer.echo(json.dumps(asdict(receipt), sort_keys=True))


@app.command('signer-verify-deployment')
def signer_verify_deployment(root: Path,
                             runtime_file: Annotated[list[str], typer.Option('--runtime-file')],
                             runtime_lock: str = 'runtime-lock.json'):
    """Check pinned signer deployment files offline, without reading the PIN."""
    from photo_worker.runtime.signer_preflight import verify_signer_deployment_files
    files = {}
    for item in runtime_file:
        name, separator, path = item.partition('=')
        if not separator or not name or not path or name in files:
            typer.echo('Signer deployment file mapping invalid', err=True)
            raise typer.Exit(2)
        files[name] = path
    try:
        receipt = verify_signer_deployment_files(root, runtime_lock, files)
    except (OSError, ValueError):
        typer.echo('Signer deployment file preflight failed', err=True)
        raise typer.Exit(2)
    typer.echo(json.dumps(receipt, sort_keys=True))


@app.command('capacity-policy-install')
def capacity_policy_install(path: Path):
    """Install exact policy timing using PHOTO_OPERATOR_DATABASE_URL."""
    from photo_worker.runtime.capacity_policy_operator import provision_capacity_policy
    try:
        receipt = provision_capacity_policy(path)
    except Exception:
        typer.echo('Capacity policy installation failed; transaction not acknowledged', err=True)
        raise typer.Exit(1)
    typer.echo(json.dumps(receipt, sort_keys=True))


@app.command('capacity-policy-rotate')
def capacity_policy_rotate(path: Path, worker: str, principal: str, node: str,
                           expected_sha: str | None = typer.Option(None),
                           expect_legacy: bool = typer.Option(False)):
    """CAS worker policy; explicitly select expected SHA or an unpinned legacy worker."""
    from uuid import UUID
    from photo_worker.runtime.capacity_policy_operator import provision_capacity_policy
    if expect_legacy == (expected_sha is not None):
        typer.echo('Supply exactly one of --expected-sha or --expect-legacy', err=True)
        raise typer.Exit(2)
    try:
        receipt = provision_capacity_policy(path, rotate=True, worker_id=UUID(worker),
            principal=principal, node=node, expected_sha=expected_sha)
    except Exception:
        typer.echo('Capacity policy rotation failed; transaction not acknowledged', err=True)
        raise typer.Exit(1)
    typer.echo(json.dumps(receipt, sort_keys=True))


@app.command('capacity-policy-reconcile')
def capacity_policy_reconcile(path: Path, worker: str, principal: str, node: str,
                              expected_sha: str | None = typer.Option(None),
                              expect_legacy: bool = typer.Option(False)):
    """Read current policy pins after an uncertain commit; never mutate or resume."""
    from uuid import UUID
    from photo_worker.runtime.capacity_policy_operator import reconcile_capacity_policy
    if expect_legacy == (expected_sha is not None):
        typer.echo('Supply exactly one of --expected-sha or --expect-legacy', err=True)
        raise typer.Exit(2)
    try:
        receipt = reconcile_capacity_policy(path, worker_id=UUID(worker), principal=principal,
            node=node, expected_sha=expected_sha)
    except Exception:
        typer.echo('Capacity policy reconciliation failed; state is unconfirmed', err=True)
        raise typer.Exit(1)
    typer.echo(json.dumps(receipt, sort_keys=True))


if __name__ == '__main__':
    app()
