"""Explicit operator installation of the existing native renderer.

This is ordinary worker/scheduler authority, never the privileged restore path.
A hashed local descriptor is configuration, not physical qualification evidence.
SQL and per-task admission independently retain their own qualification gates.
"""
import os
import re
import stat
from hashlib import sha256
from pathlib import Path
from uuid import UUID
import psycopg
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.v2.render_native_installation import NativeRenderInstallation
from photo_worker.v2.render_qualification_pin import RenderQualificationPin
from .application_role import require_application_role


def _document(pin, maximum=1 << 20):
    if (type(pin) is not dict or set(pin) != {'path','sha256'}
            or not isinstance(pin['path'],str) or not Path(pin['path']).is_absolute()
            or not isinstance(pin['sha256'],str) or re.fullmatch('[0-9a-f]{64}',pin['sha256']) is None):
        raise ValueError('NATIVE_RENDER_FILE_PIN_INVALID')
    path=Path(pin['path'])
    if path.resolve(strict=True)!=path:raise ValueError('NATIVE_RENDER_FILE_PATH_INVALID')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(fd,'rb') as stream:
        info=os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or not 2<=info.st_size<=maximum:
            raise ValueError('NATIVE_RENDER_FILE_BOUNDS')
        raw=stream.read(maximum+1)
    if len(raw)!=info.st_size or sha256(raw).hexdigest()!=pin['sha256']:
        raise ValueError('NATIVE_RENDER_FILE_CHANGED')
    value=parse_json(raw,maximum)
    if canonical_bytes(value)!=raw:raise ValueError('NATIVE_RENDER_FILE_NOT_CANONICAL')
    return value


def _calibration(value,pin,icc):
    keys={'qualification','evidence_sha','icc_lock_sha','tile_size','alpha_mode','samples'}
    qualification={k:v for k,v in pin.__dict__.items() if k!='calibration_sha'}
    if (type(value) is not dict or set(value)!=keys or value['qualification']!=qualification
            or value['icc_lock_sha']!=icc or re.fullmatch('[0-9a-f]{64}',str(value['evidence_sha'])) is None
            or type(value['tile_size']) is not int or value['tile_size'] not in (256,512,1024)
            or value['alpha_mode'] not in ('require_float32','normalize_uint16')
            or type(value['samples']) is not list or not 1<=len(value['samples'])<=256):
        raise ValueError('NATIVE_RENDER_CALIBRATION_INVALID')
    seen=set();peak={k:0 for k in ('ram_bytes','scratch_bytes','vram_bytes','output_bytes')}
    for sample in value['samples']:
        if type(sample) is not dict or set(sample)!={'width','height','provider_peak'}:
            raise ValueError('NATIVE_RENDER_CALIBRATION_SAMPLE_INVALID')
        w,h=sample['width'],sample['height'];provider=sample['provider_peak']
        if (type(w) is not int or type(h) is not int or w<1 or h<1 or w*h>200000000
                or (w,h) in seen or type(provider) is not dict or set(provider)!=set(peak)
                or any(type(v) is not int or not 0<=v<=2**53-1 for v in provider.values())):
            raise ValueError('NATIVE_RENDER_CALIBRATION_SAMPLE_INVALID')
        seen.add((w,h))
        for k,v in provider.items():peak[k]=max(peak[k],v)
    # Upper-bound every selected calibration sample, including the documented
    # ceil(1.2*pixel-ratio) output/scratch envelope. No extrapolation is admitted
    # by the per-task frozen resource profile.
    for k in ('scratch_bytes','output_bytes'):peak[k]=(peak[k]*6+4)//5
    if any(v>2**53-1 for v in peak.values()):raise ValueError('NATIVE_RENDER_CALIBRATION_OVERFLOW')
    return peak



def _selected_peak(calibration,width,height,profile):
    if (type(width) is not int or type(height) is not int or width<1 or height<1 or width*height>200000000
            or profile.get('tile_size')!=calibration['tile_size'] or profile.get('alpha_mode','require_float32')!=calibration['alpha_mode']):
        raise ValueError('NATIVE_RENDER_CALIBRATION_PROFILE_MISMATCH')
    covering=[s for s in calibration['samples'] if s['width']>=width and s['height']>=height]
    if not covering:raise ValueError('NATIVE_RENDER_CALIBRATION_COVERAGE_MISSING')
    sample=min(covering,key=lambda s:(s['width']*s['height'],s['width'],s['height']))
    peak=dict(sample['provider_peak']);denominator=sample['width']*sample['height']*5
    for key in ('scratch_bytes','output_bytes'):
        peak[key]=(peak[key]*width*height*6+denominator-1)//denominator
    if any(v>2**53-1 for v in peak.values()):raise ValueError('NATIVE_RENDER_CALIBRATION_OVERFLOW')
    return peak

def native_render_from_environment(config, *, role='photo_worker', getenv=os.environ.get):
    prefix='PHOTO_NATIVE_RENDER' if role=='photo_worker' else 'PHOTO_ADOPTER_NATIVE_RENDER'
    enabled=getenv(prefix+'_ENABLED','false')
    if enabled not in ('true','false'):raise ValueError('NATIVE_RENDER_ENABLE_INVALID')
    if enabled=='false':return None
    if role not in ('photo_worker','photo_scheduler'):raise ValueError('NATIVE_RENDER_ROLE_INVALID')
    descriptor={'path':getenv(prefix+'_CONFIG',''),'sha256':getenv(prefix+'_CONFIG_SHA','')}
    value=_document(descriptor)
    keys={'schema_version','profile','tenant_id','project_id','qualification','runtime','calibration',
          'icc_lock_sha','max_input_bytes','max_mask_bytes','max_tile_bytes','max_document_bytes'}
    if type(value) is not dict or set(value)!=keys or type(value['schema_version']) is not int or value['schema_version']!=1:
        raise ValueError('NATIVE_RENDER_CONFIG_INVALID')
    pin=RenderQualificationPin.from_value(value['qualification'])
    profile=value['profile'];expected='cpu' if profile=='cpu_precompose' else 'gpu'
    # gpu_render names the existing logical task class; its qualified provider
    # may run on CPU. CPU-class workers remain precompose-only.
    if profile not in ('cpu_precompose','gpu_render') or profile=='cpu_precompose' and pin.backend!='cpu':
        raise ValueError('NATIVE_RENDER_PROFILE_INVALID')
    if role=='photo_worker' and (config.resource_class!=expected or config.backend!=pin.backend):
        raise ValueError('NATIVE_RENDER_WORKER_PROFILE_MISMATCH')
    tenant,project=UUID(value['tenant_id']),UUID(value['project_id'])
    icc=value['icc_lock_sha']
    if tenant.int==0 or project.int==0 or not isinstance(icc,str) or re.fullmatch('[0-9a-f]{64}',icc) is None:
        raise ValueError('NATIVE_RENDER_SCOPE_INVALID')
    for key in ('max_input_bytes','max_mask_bytes','max_tile_bytes','max_document_bytes'):
        if type(value[key]) is not int or not 1<=value[key]<=2**53-1:raise ValueError('NATIVE_RENDER_BUDGET_INVALID')
    if type(value['runtime']) is not dict or type(value['calibration']) is not dict:
        raise ValueError('NATIVE_RENDER_CONFIG_PIN_MISMATCH')
    if value['runtime'].get('sha256')!=pin.runtime_lock_sha or value['calibration'].get('sha256')!=pin.calibration_sha:
        raise ValueError('NATIVE_RENDER_CONFIG_PIN_MISMATCH')
    _document(value['runtime']);calibration=_document(value['calibration'])
    peak=_calibration(calibration,pin,icc)
    if pin.backend=='cpu' and peak['vram_bytes']!=0:raise ValueError('NATIVE_RENDER_CPU_VRAM_INVALID')
    worker=config.worker_id if role=='photo_worker' else None
    operation='editor.layers_precompose' if profile=='cpu_precompose' else 'render.compose'
    def verify(current):
        if current!=pin:return False
        # Re-read immutable files on every live installation check. The normal
        # service login must pass existing nonprivileged-role attestation.
        if _document(descriptor)!=value or _document(value['calibration'])!=calibration:return False
        with psycopg.connect(config.database_url,connect_timeout=5,
                options='-c role='+role+' -c statement_timeout=5000 -c lock_timeout=1000') as connection:
            require_application_role(connection,role)
            row=connection.execute('SELECT photo.native_render_installation_ready(%s,%s,%s,%s,%s::jsonb,%s)',
                (tenant,project,worker,operation,canonical_bytes(pin.__dict__).decode(),icc)).fetchone()
        return row is not None and len(row)==1 and row[0] is True
    installation=NativeRenderInstallation(runtime_path=Path(value['runtime']['path']),qualification_pin=pin,
        icc_lock_sha=icc,verify_qualification=verify,provider_peak=peak,
        provider_peak_for=lambda width,height,profile:_selected_peak(calibration,width,height,profile),
        **{k:value[k] for k in ('max_input_bytes','max_mask_bytes','max_tile_bytes','max_document_bytes')})
    installation.verify()
    return installation
