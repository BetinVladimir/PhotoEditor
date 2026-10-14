from __future__ import annotations
import json,platform,sys
from pathlib import Path
from typing import Annotated
import typer
from photo_worker.contracts.style import StyleV2
app=typer.Typer(no_args_is_help=True)

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
        installations={}
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
        run_adopter(AdopterConfig.from_env(),stop,**options)
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
