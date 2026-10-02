"""Validate an untrusted portable style package without executing package content."""
import hashlib
import stat
import zipfile
from pathlib import PurePosixPath
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.style import StyleV2
from photo_worker.contracts.style_import import PortableManifest,StyleImportParams,ImportedAsset,StyleImportReport
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult

MAX_PACKAGE=100*1024*1024
MAX_FILES=130

def _safe_name(name:str)->bool:
    p=PurePosixPath(name)
    return bool(name) and '\\' not in name and not p.is_absolute() and '..' not in p.parts and str(p)==name and (name in ('style.json','manifest.json') or (name.startswith('assets/') and len(p.parts)==2 and bool(p.name)))

def _zip(raw:bytes):
    from io import BytesIO
    with zipfile.ZipFile(BytesIO(raw)) as z:
        infos=z.infolist()
        names=[i.filename for i in infos]
        if len(infos)>MAX_FILES or len(names)!=len(set(names)) or 'style.json' not in names:raise ValueError('Invalid package entries')
        total=0;files={}
        for i in infos:
            if not _safe_name(i.filename) or i.is_dir() or i.flag_bits&1 or stat.S_ISLNK(i.external_attr>>16):raise ValueError('Unsafe package entry')
            total+=i.file_size
            if i.file_size<0 or total>MAX_PACKAGE:raise ValueError('Package expands beyond limit')
            data=z.read(i)
            if len(data)!=i.file_size:raise ValueError('Truncated package entry')
            files[i.filename]=data
        return files

def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<512*1024*1024:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(StyleImportParams)
        with artifacts.stream(params.package) as src:raw=src.read(MAX_PACKAGE+1)
        if len(raw)>MAX_PACKAGE:raise ValueError('Package too large')
        source_sha=hashlib.sha256(raw).hexdigest();kind='zip' if raw.startswith(b'PK\x03\x04') else 'json'
        files=_zip(raw) if kind=='zip' else {'style.json':raw}
        style=StyleV2.from_bytes(files['style.json'],allow_example=False).model_copy(update={'style_id':str(params.target_style_id)})
        manifest_raw=None
        if params.assets_manifest is not None:
            with artifacts.stream(params.assets_manifest) as src:manifest_raw=src.read(1048577)
        elif 'manifest.json' in files:manifest_raw=files['manifest.json']
        manifest=PortableManifest.model_validate(parse_json(manifest_raw)) if manifest_raw is not None else PortableManifest(assets=[])
        if kind=='json' and manifest.assets:raise ValueError('External portable assets are not embedded')
        declared={a.path for a in manifest.assets}
        if {n for n in files if n.startswith('assets/')}!=declared:raise ValueError('Asset manifest does not match package')
        portable={a.portable_asset_id for a in manifest.assets}
        for model in style.models:
            if '://' in model.id or '://' in model.weight_id or ('/' in model.weight_id and model.weight_id not in portable):raise ValueError('Remote model references are forbidden')
            if model.weight_id not in portable and (len(model.weight_id)!=64 or any(c not in '0123456789abcdef' for c in model.weight_id)):raise ValueError('Model weight must be a local lock or portable asset')
        ctx.progress(1,max(2,len(manifest.assets)+2));outputs=[];imported=[]
        for index,item in enumerate(manifest.assets):
            data=files[item.path]
            if hashlib.sha256(data).hexdigest()!=item.sha256:raise ValueError('Portable asset hash mismatch')
            with artifacts.scratch.reserve(len(data)) as reservation:
                path=reservation.path('style-asset',PurePosixPath(item.path).suffix or '.bin');path.write_bytes(data);reservation.validate()
                ref=artifacts.stage(path,reservation,'style_asset',item.media_type)
            outputs.append(ref);imported.append(ImportedAsset(portable_asset_id=item.portable_asset_id,artifact=ref,sha256=item.sha256,media_type=item.media_type));ctx.progress(index+2,len(manifest.assets)+2)
        style_ref=artifacts.stage_contract(style,'style_candidate');report=StyleImportReport(source_sha256=source_sha,config_hash=style.config_hash,package_kind=kind,assets=imported,warnings=['imported_profile_requires_explicit_acceptance'])
        report_ref=artifacts.stage_contract(report,'aggregate_report');outputs.extend([style_ref,report_ref]);ctx.progress(len(manifest.assets)+2,len(manifest.assets)+2)
        return OperationResult(outputs=outputs,review_required=True,bytes_written=sum(len(v) for v in files.values())+len(canonical_bytes(style))+len(canonical_bytes(report)))
    except (ValueError,zipfile.BadZipFile,KeyError) as exc:raise OperationFailure('INPUT_INVALID') from exc

def import_operations(config:ArtifactConfig)->list[Operation]:
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'style_candidate','aggregate_report','style_asset'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('style.import','cpu',run)]
