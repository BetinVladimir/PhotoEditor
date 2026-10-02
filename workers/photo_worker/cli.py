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
    try:run_worker(WorkerConfig.from_env())
    except WorkerConfigurationError as exc:
        typer.echo(str(exc),err=True);raise typer.Exit(2)
    except Exception:
        # Broker/DB exceptions may contain credentials. Never print their text.
        typer.echo('Worker stopped: configuration, dependency or shutdown failure',err=True)
        raise typer.Exit(1)

@app.command('v2-adopter')
def v2_adopter():
    """Adopt verified V2 result closures and finalize eligible V2 runs."""
    import signal
    from threading import Event
    from photo_worker.v2.service import AdopterConfig,run_adopter
    stop=Event()
    signal.signal(signal.SIGTERM,lambda *_:stop.set())
    signal.signal(signal.SIGINT,lambda *_:stop.set())
    try:run_adopter(AdopterConfig.from_env(),stop)
    except ValueError:
        typer.echo('V2 adopter configuration invalid',err=True);raise typer.Exit(2)
    except Exception:
        # SQL and S3 errors may embed credentials or signed request details.
        typer.echo('V2 adopter stopped: dependency or runtime failure',err=True)
        raise typer.Exit(1)

if __name__=='__main__':app()
