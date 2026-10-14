"""Exact-version encoded export with production file-format verification."""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from tempfile import TemporaryDirectory
from uuid import uuid4
from .scratch import ScratchSpace
from PIL import Image
from photo_worker.contracts.export import EncodeParams
from photo_worker.operations.export.encode import _verify
from .restore_objects import MinioRestoreObjects
from .restore_export_result import observe_fixture_encoded_export
from .restore_export_plan import verify_fixture_export_plan,verify_fixture_export_params
from .restore_export_geometry import verify_export_geometry
from .restore_export_memory import verify_fixture_export_memory
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestorePhysicalExport:
    result: object
    sha256: str
    bytes_verified: int
    width: int
    height: int
    alpha: bool
    icc_sha256: str


def verify_fixture_encoded_export(connection,client,buckets,target,binding,ticket,selection,asset_version_id,*,
        expected_database,params,source,width,height,alpha,scratch_root,check_active):
    # Params/geometry must be restored independently from the registered plan,
    # not inferred from the file being verified.
    if (not isinstance(params,EncodeParams) or type(alpha) is not bool
            or any(type(v) is not int or not 1<=v<=200000 for v in (width,height))
            or width*height>200000000 or not callable(check_active)):
        raise ValueError('RESTORE_EXPORT_PHYSICAL_INSTALLATION')
    require_restore_active(check_active)
    verify_export_geometry(params,source,asset_version_id,width,height,alpha)
    root=Path(scratch_root)
    if not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir():
        raise ValueError('RESTORE_EXPORT_SCRATCH_ROOT')
    def observe():return observe_fixture_encoded_export(connection,target,binding,ticket,selection,asset_version_id,
        expected_database=expected_database,check_active=check_active)
    before=observe();obj=before.object
    plan_sha=verify_fixture_export_params(connection,client,buckets,target.tenant_id,before,params,
        expected_database=expected_database,check_active=check_active)
    expected_media={'jpeg':'image/jpeg','png':'image/png','tiff':'image/tiff'}[params.format]
    if before.media_type!=expected_media or not 1<=obj.size<=1073741824:
        raise ValueError('RESTORE_EXPORT_PHYSICAL_MEDIA')
    memory_sha=verify_fixture_export_memory(connection,target.tenant_id,before,source,width,height,
        expected_database=expected_database,check_active=check_active)
    store=MinioRestoreObjects(client,buckets,check_active=check_active)
    with TemporaryDirectory(prefix='restore-export-',dir=root) as directory, \
            ScratchSpace(Path(directory),target.tenant_id,uuid4(),1,obj.size) as scratch, \
            scratch.reserve(obj.size) as reservation:
        suffix={'jpeg':'.jpg','png':'.png','tiff':'.tiff'}[params.format]
        path=reservation.planned_path('encoded',suffix);digest=sha256();size=0
        with path.open('xb') as output,store.open_version(obj) as stream:
            while True:
                require_restore_active(check_active);block=stream.read(min(65536,obj.size-size+1))
                if not block:break
                size+=len(block)
                if size>obj.size:raise ValueError('RESTORE_EXPORT_PHYSICAL_SIZE')
                digest.update(block)
                if output.write(block)!=len(block):raise OSError('RESTORE_EXPORT_SHORT_WRITE')
        reservation.validate_file(path)
        if size!=obj.size or digest.hexdigest()!=obj.sha256:raise ValueError('RESTORE_EXPORT_PHYSICAL_SHA')
        if params.format in ('jpeg','png'):
            with Image.open(path) as header:
                if header.format!=params.format.upper() or header.size!=(width,height):
                    raise ValueError('RESTORE_EXPORT_PHYSICAL_ENCODING')
        require_restore_active(check_active);_verify(path,params,width,height,alpha);require_restore_active(check_active)
        if (observe()!=before or verify_fixture_export_plan(connection,target.tenant_id,before,params,
                expected_database=expected_database,check_active=check_active)!=plan_sha
                or verify_fixture_export_memory(connection,target.tenant_id,before,source,width,height,
                    expected_database=expected_database,check_active=check_active)!=memory_sha):
            raise ValueError('RESTORE_EXPORT_PHYSICAL_CHANGED')
    require_restore_active(check_active);return RestorePhysicalExport(before,obj.sha256,size,width,height,alpha,params.icc_sha256)
