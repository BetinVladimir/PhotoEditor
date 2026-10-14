"""Private version-pinned preparation of an accepted semantic ImageResult."""
from hashlib import sha256
from contextlib import closing
from psycopg.pq import TransactionStatus
from photo_worker.operations.optical.contracts import ImageResult
from .adoption import AdoptionInvalid


def read_exposure_presentation_result(connection, objects, *, tenant_id, target_id,
                                      allowed_bucket, max_bytes=1024*1024):
    """Read canonical root outside a transaction and recheck authority after IO.

    This is preparation metadata, not a public URL or derivative receipt.
    """
    if not connection.autocommit or not allowed_bucket:
        raise ValueError('Autocommit connection and trusted bucket required')
    if type(max_bytes) is not int or not 1 <= max_bytes <= 4*1024*1024:
        raise ValueError('Invalid presentation document limit')
    def source():
        if connection.info.transaction_status != TransactionStatus.IDLE:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_TRANSACTION_ACTIVE')
        rows=connection.execute(
            'SELECT * FROM photo.semantic_exposure_presentation_source(%s,%s)',
            (tenant_id,target_id)).fetchall()
        if len(rows)!=1:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        return rows[0]
    before=source()
    if (before[8]!=allowed_bucket or not isinstance(before[9],str) or not before[9]
            or not isinstance(before[10],str) or before[10] in ('','null')
            or type(before[11]) is not int or not 1<=before[11]<=max_bytes
            or before[12]!='application/json'):
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_INVALID')
    response=objects.get_object(before[8],before[9],version_id=before[10])
    try:
        with closing(response):
            data=bytearray()
            while len(data)<=before[11]:
                limit=min(65536,before[11]+1-len(data))
                block=response.read(limit)
                if not isinstance(block,bytes) or len(block)>limit:
                    raise AdoptionInvalid('SEMANTIC_PRESENTATION_STREAM_INVALID')
                if not block:break
                data.extend(block)
            if len(data)!=before[11] or sha256(data).hexdigest()!=before[7]:
                raise AdoptionInvalid('SEMANTIC_PRESENTATION_CONTENT_MISMATCH')
    finally:
        release=getattr(response,'release_conn',None)
        if release is not None:release()
    try:
        result=ImageResult.model_validate_json(bytes(data))
    except ValueError as exc:
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_CONTRACT_INVALID') from exc
    if source()!=before:
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
    return result


def read_exposure_presentation_working_image(connection, objects, *, tenant_id,
                                              target_id, allowed_bucket):
    """Resolve WorkingImage exclusively from the validated accepted root."""
    from photo_worker.contracts.images import WorkingImage
    root=read_exposure_presentation_result(connection,objects,tenant_id=tenant_id,
                                           target_id=target_id,allowed_bucket=allowed_bucket)
    def location():
        if connection.info.transaction_status != TransactionStatus.IDLE:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_TRANSACTION_ACTIVE')
        rows=connection.execute(
            'SELECT * FROM photo.semantic_exposure_presentation_evidence(%s,%s,%s)',
            (tenant_id,target_id,root.working_image.artifact_id)).fetchall()
        if len(rows)!=1:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        return rows[0]
    before=location()
    if (before[0]!=allowed_bucket or before[3]!=root.working_image.sha256
            or type(before[4]) is not int or not 1<=before[4]<=1024*1024
            or before[5:]!=('application/json','working_image')):
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_INVALID')
    response=objects.get_object(before[0],before[1],version_id=before[2])
    try:
        with closing(response):
            data=bytearray()
            while len(data)<=before[4]:
                limit=min(65536,before[4]+1-len(data))
                block=response.read(limit)
                if not isinstance(block,bytes) or len(block)>limit:
                    raise AdoptionInvalid('SEMANTIC_PRESENTATION_STREAM_INVALID')
                if not block:break
                data.extend(block)
            if len(data)!=before[4] or sha256(data).hexdigest()!=before[3]:
                raise AdoptionInvalid('SEMANTIC_PRESENTATION_CONTENT_MISMATCH')
    finally:
        release=getattr(response,'release_conn',None)
        if release is not None:release()
    try:
        working=WorkingImage.model_validate_json(bytes(data))
    except ValueError as exc:
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_CONTRACT_INVALID') from exc
    if location()!=before:
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
    return working


from contextlib import contextmanager

@contextmanager
def exposure_presentation_pixels(connection, objects, *, tenant_id, target_id,
                                 allowed_bucket, scratch_root, max_bytes, icc_registry,
                                 check_active=lambda:None):
    """Yield read-only accepted RGB pixels; mapping cannot outlive scratch."""
    from pathlib import Path
    from tempfile import TemporaryDirectory
    from photo_worker.runtime.arrays import read_array
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1 or not callable(check_active):
        raise ValueError('Invalid presentation scratch budget')
    check_active()
    working=read_exposure_presentation_working_image(connection,objects,
        tenant_id=tenant_id,target_id=target_id,allowed_bucket=allowed_bucket)
    source_profile=icc_registry.resolve(working.color.profile_id)
    if source_profile.sha256!=working.color.icc_sha256:
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_ICC_MISMATCH')
    def location():
        check_active()
        if connection.info.transaction_status!=TransactionStatus.IDLE:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_TRANSACTION_ACTIVE')
        rows=connection.execute('SELECT * FROM photo.semantic_exposure_presentation_evidence(%s,%s,%s)',
            (tenant_id,target_id,working.pixels.artifact_id)).fetchall()
        if len(rows)!=1:raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        return rows[0]
    before=location()
    if (before[0]!=allowed_bucket or before[3]!=working.pixels.sha256
            or type(before[4]) is not int or not 1<=before[4]<=max_bytes
            or before[5:]!=('application/x-npy','pixels')):
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_INVALID')
    with TemporaryDirectory(prefix='semantic-preview-',dir=Path(scratch_root)) as directory:
        path=Path(directory)/'pixels.npy'
        response=objects.get_object(before[0],before[1],version_id=before[2])
        try:
            with closing(response),path.open('xb') as output:
                count=0;digest=sha256()
                while count<=before[4]:
                    check_active()
                    limit=min(65536,before[4]+1-count)
                    block=response.read(limit)
                    if not isinstance(block,bytes) or len(block)>limit:
                        raise AdoptionInvalid('SEMANTIC_PRESENTATION_STREAM_INVALID')
                    if not block:break
                    count+=len(block)
                    if count>before[4]:raise AdoptionInvalid('SEMANTIC_PRESENTATION_CONTENT_MISMATCH')
                    output.write(block);digest.update(block)
                if count!=before[4] or digest.hexdigest()!=before[3]:
                    raise AdoptionInvalid('SEMANTIC_PRESENTATION_CONTENT_MISMATCH')
        finally:
            release=getattr(response,'release_conn',None)
            if release is not None:release()
        if location()!=before:raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        with read_array(path,(working.canvas.height,working.canvas.width,3),working.dtype,check_active) as pixels:
            yield working,pixels
        check_active()


@contextmanager
def exposure_presentation_alpha(connection, objects, *, tenant_id, target_id,
                                allowed_bucket, scratch_root, max_bytes,
                                check_active=lambda:None):
    """Yield inherited opacity unchanged, with strict geometry and pinned IO."""
    from pathlib import Path
    from tempfile import TemporaryDirectory
    from photo_worker.runtime.arrays import read_alpha_array
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1 or not callable(check_active):
        raise ValueError('Invalid presentation scratch budget')
    check_active()
    working=read_exposure_presentation_working_image(connection,objects,
        tenant_id=tenant_id,target_id=target_id,allowed_bucket=allowed_bucket)
    if working.alpha is None:
        yield None
        return
    def location():
        check_active()
        if connection.info.transaction_status!=TransactionStatus.IDLE:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_TRANSACTION_ACTIVE')
        rows=connection.execute('SELECT * FROM photo.semantic_exposure_presentation_input(%s,%s,%s)',
            (tenant_id,target_id,working.alpha.artifact_id)).fetchall()
        if len(rows)!=1:raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        return rows[0]
    before=location()
    if (before[0]!=allowed_bucket or before[3]!=working.alpha.sha256
            or type(before[4]) is not int or not 1<=before[4]<=max_bytes
            or before[5] not in ('application/x-npy','application/octet-stream')):
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_INVALID')
    with TemporaryDirectory(prefix='semantic-alpha-',dir=Path(scratch_root)) as directory:
        path=Path(directory)/'alpha.npy'
        response=objects.get_object(before[0],before[1],version_id=before[2])
        try:
            with closing(response),path.open('xb') as output:
                count=0;digest=sha256()
                while count<=before[4]:
                    check_active()
                    limit=min(65536,before[4]+1-count)
                    block=response.read(limit)
                    if not isinstance(block,bytes) or len(block)>limit:
                        raise AdoptionInvalid('SEMANTIC_PRESENTATION_STREAM_INVALID')
                    if not block:break
                    count+=len(block)
                    if count>before[4]:raise AdoptionInvalid('SEMANTIC_PRESENTATION_CONTENT_MISMATCH')
                    output.write(block);digest.update(block)
                if count!=before[4] or digest.hexdigest()!=before[3]:
                    raise AdoptionInvalid('SEMANTIC_PRESENTATION_CONTENT_MISMATCH')
        finally:
            release=getattr(response,'release_conn',None)
            if release is not None:release()
        if location()!=before:raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        with read_alpha_array(path,(working.canvas.height,working.canvas.width),check_active) as alpha:
            yield alpha
        check_active()


@contextmanager
def exposure_presentation_image(connection, objects, *, tenant_id, target_id,
                                allowed_bucket, scratch_root, max_bytes,
                                icc_registry, check_active=lambda:None):
    """Prepare RGB and inherited alpha within one total disk budget."""
    from contextlib import ExitStack
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1:
        raise ValueError('Invalid presentation scratch budget')
    check_active()
    working=read_exposure_presentation_working_image(connection,objects,
        tenant_id=tenant_id,target_id=target_id,allowed_bucket=allowed_bucket)
    sizes=[]
    refs=[(working.pixels,'semantic_exposure_presentation_evidence')]
    if working.alpha is not None:refs.append((working.alpha,'semantic_exposure_presentation_input'))
    for ref,function in refs:
        if connection.info.transaction_status!=TransactionStatus.IDLE:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_TRANSACTION_ACTIVE')
        rows=connection.execute('SELECT * FROM photo.'+function+'(%s,%s,%s)',
            (tenant_id,target_id,ref.artifact_id)).fetchall()
        if len(rows)!=1 or rows[0][0]!=allowed_bucket or rows[0][3]!=ref.sha256:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        size=rows[0][4]
        if type(size) is not int or size<1:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_INVALID')
        sizes.append(size)
    if sum(sizes)>max_bytes:
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_SCRATCH_EXCEEDED')
    common=dict(tenant_id=tenant_id,target_id=target_id,allowed_bucket=allowed_bucket,
                scratch_root=scratch_root,check_active=check_active)
    with ExitStack() as stack:
        image,pixels=stack.enter_context(exposure_presentation_pixels(connection,objects,
            **common,max_bytes=sizes[0],icc_registry=icc_registry))
        alpha=stack.enter_context(exposure_presentation_alpha(connection,objects,
            **common,max_bytes=sizes[1] if len(sizes)>1 else 1))
        current=read_exposure_presentation_working_image(connection,objects,
            tenant_id=tenant_id,target_id=target_id,allowed_bucket=allowed_bucket)
        if image!=working or current!=working:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        check_active()
        yield image,pixels,alpha


@contextmanager
def encode_exposure_presentation_preview(connection, objects, *, tenant_id, target_id,
        allowed_bucket, scratch_root, max_bytes, icc_registry, execution_profile,
        variant='preview1600',check_active=lambda:None):
    """Encode an ephemeral color-managed preview; caller must separately publish."""
    from pathlib import Path
    from tempfile import TemporaryDirectory
    import numpy as np
    from PIL import Image
    from photo_worker.operations.media.derivative import dimensions,QUALITY,_write
    from photo_worker.operations.export.encode import _resize_rgba
    from photo_worker.operations.export.memory import admit_presentation
    from photo_worker.color.icc import LittleCMS
    if variant not in ('preview1600','preview2560') or type(max_bytes) is not int:
        raise ValueError('Invalid preview configuration')
    working=read_exposure_presentation_working_image(connection,objects,
        tenant_id=tenant_id,target_id=target_id,allowed_bucket=allowed_bucket)
    width,height=dimensions(working.canvas.width,working.canvas.height,variant)
    admit_presentation(execution_profile,working.canvas.width,working.canvas.height,
                       width,height,alpha=working.alpha is not None)
    encoded_budget=width*height*5+2*1024*1024
    if max_bytes<=encoded_budget:raise AdoptionInvalid('SEMANTIC_PRESENTATION_SCRATCH_EXCEEDED')
    source=icc_registry.resolve(working.color.profile_id)
    target=icc_registry.resolve('srgb_v1')
    with exposure_presentation_image(connection,objects,tenant_id=tenant_id,target_id=target_id,
            allowed_bucket=allowed_bucket,scratch_root=scratch_root,max_bytes=max_bytes-encoded_budget,
            icc_registry=icc_registry,check_active=check_active) as (image,rgb,alpha):
        if image!=working:raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        check_active()
        normalized=rgb.astype(np.float32)/65535 if rgb.dtype==np.uint16 else rgb
        opacity=alpha.astype(np.float32)/65535 if alpha is not None and alpha.dtype==np.uint16 else alpha
        resized,resized_alpha=_resize_rgba(normalized,opacity,width,height,encoding=image.color.encoding)
        converted=LittleCMS().convert(np.round(np.clip(resized,0,1)*65535).astype(np.uint16),source.data,target.data)
        converted=converted.astype(np.float32)/65535
        check_active()
        with TemporaryDirectory(prefix='semantic-encoded-',dir=Path(scratch_root)) as directory:
            path=Path(directory)/('preview.webp' if resized_alpha is not None else 'preview.jpg')
            _write(path,converted,resized_alpha,target.data,QUALITY[variant],strip_metadata=True)
            if not 1<=path.stat().st_size<=encoded_budget:
                raise AdoptionInvalid('SEMANTIC_PRESENTATION_SCRATCH_EXCEEDED')
            with Image.open(path) as encoded:
                encoded.verify()
            with Image.open(path) as encoded:
                if (encoded.size!=(width,height) or encoded.mode!=('RGBA' if alpha is not None else 'RGB')
                        or sha256(encoded.info.get('icc_profile',b'')).hexdigest()!=target.sha256
                        or any(encoded.info.get(k) for k in ('exif','xmp','comment','photoshop'))):
                    raise AdoptionInvalid('SEMANTIC_PRESENTATION_ENCODING_INVALID')
                encoded.load()
            current=read_exposure_presentation_working_image(connection,objects,tenant_id=tenant_id,target_id=target_id,allowed_bucket=allowed_bucket)
            if current!=working:raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
            check_active()
            yield path,'image/webp' if alpha is not None else 'image/jpeg'


def prepare_exposure_preview_plan(connection, objects, *, tenant_id, target_id,
                                  allowed_bucket, icc_registry, variant='preview1600'):
    """Compile existing media.preview inputs from an accepted exposure result.

    Scheduler must revalidate source and reserve resources when admitting this
    plan; preparation alone does not enqueue or authorize a derivative task.
    """
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from .render_derivative import compile_render_derivative
    def source():
        if not connection.autocommit or connection.info.transaction_status!=TransactionStatus.IDLE:
            raise AdoptionInvalid('SEMANTIC_PRESENTATION_TRANSACTION_ACTIVE')
        rows=connection.execute('SELECT * FROM photo.semantic_exposure_presentation_source(%s,%s)',
            (tenant_id,target_id)).fetchall()
        if len(rows)!=1:raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
        return rows[0]
    before=source()
    root=read_exposure_presentation_result(connection,objects,tenant_id=tenant_id,
        target_id=target_id,allowed_bucket=allowed_bucket)
    working=read_exposure_presentation_working_image(connection,objects,tenant_id=tenant_id,
        target_id=target_id,allowed_bucket=allowed_bucket)
    if icc_registry.resolve(working.color.profile_id).sha256!=working.color.icc_sha256:
        raise AdoptionInvalid('SEMANTIC_PRESENTATION_ICC_MISMATCH')
    plan=compile_render_derivative(ArtifactRef(artifact_id=before[6],sha256=before[7]),
        canonical_bytes(root),canonical_bytes(working),icc_registry.resolve('srgb_v1').sha256,variant)
    if source()!=before:raise AdoptionInvalid('SEMANTIC_PRESENTATION_SOURCE_STALE')
    return plan
