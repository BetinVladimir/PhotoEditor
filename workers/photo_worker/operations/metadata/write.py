"""Allowlisted metadata writer. ExifTool is invoked as argv with shell disabled."""
from contextlib import ExitStack
import hashlib,os,shutil,subprocess
from pathlib import Path
from PIL import Image
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataDocument,MetadataParams,MetadataReport
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult
from photo_worker.runtime.image_fingerprint import decoded_image_fingerprint
from .verify import verify_written_metadata
from .resources import validate_metadata_resources
from .process import run_metadata_tool


def build_args(executable:str,path:Path,manifest:EncodedImage,document:MetadataDocument,params:MetadataParams)->tuple[list[str],list[str]]:
    args=[executable,'-overwrite_original','-EXIF:all=','-XMP:all=','-IPTC:all=','-ThumbnailImage=','-PreviewImage=','-Orientation#=1',f'-ExifImageWidth#={manifest.width}',f'-ExifImageHeight#={manifest.height}'];copied=[]
    def add(field,tag,value):
        if value is not None:args.append(f'-{tag}={value}');copied.append(field)
    add('author','Artist',document.author);add('copyright','Copyright',document.copyright)
    if params.policy=='safe_camera':
        add('camera_make','Make',document.camera_make);add('camera_model','Model',document.camera_model);add('lens','LensModel',document.lens);add('datetime_original','DateTimeOriginal',document.datetime_original)
        if params.copy_gps and document.gps is not None:
            # EXIF stores magnitudes separately from hemisphere/altitude signs.
            # Explicit refs avoid relying on ExifTool's inferred defaults.
            gps=document.gps
            args.extend([f'-GPSLatitude#={abs(gps.latitude)}',
                f'-GPSLatitudeRef={"S" if gps.latitude<0 else "N"}',
                f'-GPSLongitude#={abs(gps.longitude)}',
                f'-GPSLongitudeRef={"W" if gps.longitude<0 else "E"}'])
            if gps.altitude_m is not None:
                args.extend([f'-GPSAltitude#={abs(gps.altitude_m)}',
                    f'-GPSAltitudeRef#={1 if gps.altitude_m<0 else 0}'])
            copied.append('gps')
    args.extend(['--',str(path)])
    return args,copied


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,executable:str,runner=None)->OperationResult:
    try:
        p=artifacts.params(MetadataParams);encoded=artifacts.contract(p.encoded,EncodedImage);document=artifacts.contract(p.document,MetadataDocument)
        if ctx.lease.execution_profile.get('backend')!='cpu' or list(ctx.lease.command.inputs)!=[p.encoded,encoded.artifact,p.document] or encoded.metadata_policy!='strip':raise ValueError('Metadata binding mismatch')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    suffix={'jpeg':'.jpg','png':'.png','tiff':'.tiff'}[encoded.format];ctx.progress(0,4)
    with ExitStack() as stack:
        source=stack.enter_context(artifacts.input(encoded.artifact,suffix=suffix))
        media={'jpeg':'image/jpeg','png':'image/png','tiff':'image/tiff'}[encoded.format]
        output_limit=None
        if 'resource_estimate' in ctx.lease.execution_profile:
            try:
                output_limit=validate_metadata_resources(ctx.lease.execution_profile,encoded.width,encoded.height,source.path.stat().st_size)
            except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
        try:
            with source.path.open('rb') as original:
                original_pixels=decoded_image_fingerprint(original,media,preserve_orientation=False,check_active=ctx.check_active)
        except (OSError,ValueError) as exc:raise OperationFailure('INPUT_INVALID') from exc
        with artifacts.scratch.reserve(source.path.stat().st_size+1048576) as reservation:
            path=reservation.path('metadata',suffix);shutil.copyfile(source.path,path);args,copied=build_args(executable,path,encoded,document,p)
            try:
                ctx.check_active()
                completed=(run_metadata_tool(args,ctx.check_active) if runner is None else
                    runner(args,shell=False,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=120,check=False))
                ctx.check_active()
            except subprocess.TimeoutExpired as exc:raise OperationFailure('TIMEOUT',True) from exc
            except (OSError,subprocess.SubprocessError) as exc:raise OperationFailure('DEPENDENCY_UNAVAILABLE',True) from exc
            if completed.returncode!=0:raise OperationFailure('METADATA_WRITE_FAILED')
            reservation.validate_file(path)
            if output_limit is not None and path.stat().st_size>output_limit:
                raise OperationFailure('METADATA_OUTPUT_TOO_LARGE')
            try:
                with Image.open(path) as check:
                    check.verify()
                with Image.open(path) as check:
                    if check.size!=(encoded.width,encoded.height) or check.getexif().get(274,1)!=1:raise ValueError('Metadata raster changed')
                    profile=check.info.get('icc_profile')
                    if profile is None or hashlib.sha256(profile).hexdigest()!=encoded.icc_sha256:raise ValueError('Metadata ICC changed')
                    verify_written_metadata(check,document,p,check_active=ctx.check_active)
                with path.open('rb') as rewritten:
                    if decoded_image_fingerprint(rewritten,media,preserve_orientation=False,check_active=ctx.check_active)!=original_pixels:
                        raise ValueError('Metadata decoded pixels changed')
            except (OSError,ValueError) as exc:raise OperationFailure('METADATA_VERIFY_FAILED') from exc
            ctx.check_active()
            output=artifacts.stage(path,reservation,'encoded_image',media);written=path.stat().st_size
    result=encoded.model_copy(update={'artifact':output,'metadata_policy':p.policy})
    ctx.check_active()
    manifest_ref=artifacts.stage_contract(result,'encoded_manifest')
    report=MetadataReport(encoded=p.encoded,document=p.document,policy=p.policy,copied_fields=copied,
        orientation=1,stale_previews_removed=True,icc_preserved=True)
    ctx.check_active()
    report_ref=artifacts.stage_contract(report,'metadata_report')
    ctx.progress(4,4)
    return OperationResult(outputs=[output,manifest_ref,report_ref],
        bytes_written=written+len(canonical_bytes(result))+len(canonical_bytes(report)))


def metadata_operations(config:ArtifactConfig,executable:str|None=None)->list[Operation]:
    executable=executable or shutil.which('exiftool')
    if executable is None or not Path(executable).is_file() or not os.access(executable,os.X_OK):return []
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'encoded_image','encoded_manifest','metadata_report'})) as a:return execute(ctx,a,executable)
    return [Operation('metadata.write','cpu',run)]
