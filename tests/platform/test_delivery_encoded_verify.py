from hashlib import sha256
from uuid import uuid4

import numpy as np
import pytest
import tifffile
from PIL import Image

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.export import EncodeParams, EncodedImage
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.v2.delivery_encoded_verify import verify_delivery_encoded_file


def fixture(path, format='jpeg'):
    profile = b'ICC pinned by authorized Params'
    params = EncodeParams(image=ArtifactRef(artifact_id=uuid4(), sha256='a'*64),
        format=format, icc_id='srgb_v1', icc_sha256=sha256(profile).hexdigest(),
        quality=90 if format == 'jpeg' else None, alpha='matte',
        matte_rgb=[1., 1., 1.], metadata_policy='strip')
    source = WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(), sha256='b'*64),
        canvas=Canvas(width=4, height=3, orientation=1, space_id='oriented:'+'c'*64, source_sha256='c'*64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1', icc_sha256='d'*64, encoding='gamma24'),
        dtype='float32', alpha=None, transform_chain=TransformChain.create([]))
    if format == 'tiff':
        tifffile.imwrite(path, np.zeros((3, 4, 3), dtype=np.uint16), photometric='rgb', metadata=None,
            extratags=[(34675, 'B', len(profile), profile, False)])
    else:
        Image.new('RGB', (4, 3), 'red').save(path, format='JPEG', icc_profile=profile)
    manifest = EncodedImage(artifact=ArtifactRef(artifact_id=uuid4(), sha256=sha256(path.read_bytes()).hexdigest()),
        format=format, width=4, height=3, icc_id=params.icc_id, icc_sha256=params.icc_sha256,
        alpha=False, metadata_policy='strip')
    return params, source, manifest, profile


@pytest.mark.parametrize('format', ['jpeg', 'tiff'])
def test_delivery_encoded_verifies_physical_raster(tmp_path, format):
    path = tmp_path / 'encoded.bin'
    params, source, manifest, _ = fixture(path, format)
    result = verify_delivery_encoded_file(path, params, source, manifest, max_bytes=100000, max_pixels=12)
    assert result.sha256 == manifest.artifact.sha256
    assert result.bytes == path.stat().st_size
    assert (result.width, result.height, result.alpha) == (4, 3, False)


@pytest.mark.parametrize('scenario,code', [
    ('sha', 'SHA_MISMATCH'), ('manifest', 'MANIFEST_MISMATCH'),
    ('bytes', 'BYTE_LIMIT'), ('pixels', 'PIXEL_LIMIT'),
    ('icc', 'ICC_MISMATCH'), ('metadata', 'METADATA_NOT_STRIPPED'),
    ('container', 'CONTAINER_MISMATCH'),
])
def test_delivery_encoded_rejects_substitutions(tmp_path, scenario, code):
    path = tmp_path / 'encoded.bin'
    params, source, manifest, profile = fixture(path)
    if scenario == 'sha':
        path.write_bytes(path.read_bytes() + b'x')
    if scenario == 'manifest':
        manifest = manifest.model_copy(update={'width': 5})
    if scenario in ('icc', 'metadata', 'container'):
        kwargs = {'icc_profile': b'other' if scenario == 'icc' else profile}
        if scenario == 'metadata':
            exif = Image.Exif(); exif[315] = 'private author'; kwargs['exif'] = exif
        Image.new('RGB', (4, 3)).save(path, format='PNG' if scenario == 'container' else 'JPEG', **kwargs)
        manifest = manifest.model_copy(update={'artifact': ArtifactRef(artifact_id=manifest.artifact.artifact_id,
            sha256=sha256(path.read_bytes()).hexdigest())})
    with pytest.raises(ValueError, match=code):
        verify_delivery_encoded_file(path, params, source, manifest,
            max_bytes=1 if scenario == 'bytes' else 100000,
            max_pixels=11 if scenario == 'pixels' else 12)


def test_delivery_encoded_cancellation_precedes_io(tmp_path):
    def cancelled():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        verify_delivery_encoded_file(tmp_path/'absent', None, None, None,
            max_bytes=1, max_pixels=1, check_active=cancelled)


@pytest.mark.parametrize('scenario', ['valid', 'short', 'overflow', 'foreign_role', 'extra_output', 'revoked'])
def test_delivery_encode_preparation_stream_and_authority(tmp_path, scenario):
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.v2.adoption import ArtifactRef as Ref, ArtifactEvidence, AdoptionInvalid
    from photo_worker.v2.delivery_encoded_prepare import prepare_delivery_encode
    path = tmp_path / 'encoded.bin'
    params, source, manifest, _ = fixture(path, 'tiff')
    source_raw = canonical_bytes(source)
    params = params.model_copy(update={'image': ArtifactRef(artifact_id=params.image.artifact_id,
        sha256=sha256(source_raw).hexdigest())})
    params_raw, manifest_raw = canonical_bytes(params), canonical_bytes(manifest)
    params_ref = Ref(uuid4(), sha256(params_raw).hexdigest())
    source_ref = Ref(params.image.artifact_id, params.image.sha256)
    root = Ref(uuid4(), sha256(manifest_raw).hexdigest())
    raster = Ref(manifest.artifact.artifact_id, manifest.artifact.sha256)
    bodies = {params_ref: params_raw, source_ref: source_raw, root: manifest_raw, raster: path.read_bytes()}
    evidence = {ref: ArtifactEvidence(ref, 'image/tiff' if ref == raster else 'application/json', len(body),
        'encoded_manifest' if ref == root else 'encoded_image' if ref == raster else None,
        'output' if ref in (root, raster) else 'input') for ref, body in bodies.items()}

    class Reader:
        streamed = False
        closed = False
        def evidence(self, ref):
            item = evidence[ref]
            if ref == raster and (scenario == 'foreign_role' or scenario == 'revoked' and self.streamed):
                return ArtifactEvidence(item.ref, item.media_type, item.bytes, 'foreign', item.source)
            return item
        def read_json(self, ref, maximum):
            return bodies[ref]
        def staged_outputs(self):
            return {root, raster, params_ref} if scenario == 'extra_output' else {root, raster}
        def iter_bytes(self, ref, maximum):
            try:
                body = bodies[ref]
                if scenario == 'short': body = body[:-1]
                if scenario == 'overflow': body += b'x'
                yield body
                self.streamed = True
            finally:
                self.closed = True

    reader = Reader()
    scratch = tmp_path / 'scratch'; scratch.mkdir()
    if scenario == 'valid':
        result = prepare_delivery_encode(reader, params_ref, root, scratch_root=scratch,
            max_bytes=100000, max_pixels=12)
        assert result.raster.sha256 == raster.sha256
        assert result.manifest_ref == root
        assert result.params_jcs == params_raw
    else:
        with pytest.raises(AdoptionInvalid):
            prepare_delivery_encode(reader, params_ref, root, scratch_root=scratch,
                max_bytes=100000, max_pixels=12)
    assert list(scratch.iterdir()) == []
    if scenario not in ('foreign_role', 'extra_output'):
        assert reader.closed


@pytest.mark.parametrize('mode',['valid','missing-authority','substituted','extra-camera','strip-mismatch'])
def test_delivery_post_metadata_physical_verification(tmp_path,mode):
    from photo_worker.contracts.metadata import MetadataDocument
    path=tmp_path/'copyright.jpg'
    params,source,manifest,profile=fixture(path)
    document=MetadataDocument(author='Author',copyright='Copyright')
    exif=Image.Exif();exif[274]=1;exif[315]='substitution' if mode=='substituted' else 'Author';exif[33432]='Copyright'
    if mode=='extra-camera':exif[272]='private camera'
    Image.new('RGB',(4,3),'red').save(path,format='JPEG',icc_profile=profile,exif=exif)
    manifest=manifest.model_copy(update={'metadata_policy':'strip' if mode=='strip-mismatch' else 'copyright_only',
        'artifact':ArtifactRef(artifact_id=manifest.artifact.artifact_id,sha256=sha256(path.read_bytes()).hexdigest())})
    kwargs={} if mode=='missing-authority' else {'metadata_document':document}
    if mode=='valid':
        got=verify_delivery_encoded_file(path,params,source,manifest,max_bytes=100000,max_pixels=12,**kwargs)
        assert got.sha256==manifest.artifact.sha256
    else:
        with pytest.raises(ValueError):verify_delivery_encoded_file(path,params,source,manifest,max_bytes=100000,max_pixels=12,**kwargs)


@pytest.mark.parametrize('extra',[False,True])
def test_delivery_tiff_copyright_preserves_16bit_verification(tmp_path,extra):
    from photo_worker.contracts.metadata import MetadataDocument
    path=tmp_path/'copyright.tiff'
    params,source,manifest,profile=fixture(path,'tiff')
    tags=[(34675,'B',len(profile),profile,False),(315,'s',0,'Author',False),(33432,'s',0,'Copyright',False)]
    if extra:tags.append((272,'s',0,'private camera',False))
    tifffile.imwrite(path,np.zeros((3,4,3),dtype=np.uint16),photometric='rgb',metadata=None,software=None,extratags=tags)
    manifest=manifest.model_copy(update={'metadata_policy':'copyright_only',
        'artifact':ArtifactRef(artifact_id=manifest.artifact.artifact_id,sha256=sha256(path.read_bytes()).hexdigest())})
    def verify():
        return verify_delivery_encoded_file(path,params,source,manifest,max_bytes=100000,max_pixels=12,
            metadata_document=MetadataDocument(author='Author',copyright='Copyright'))
    if extra:
        with pytest.raises(ValueError):verify()
    else:
        result=verify()
        assert result.sha256==manifest.artifact.sha256 and (result.width,result.height)==(4,3)


@pytest.mark.parametrize('mode',['valid','missing-authority','wrong-policy','extra-author','changed-camera'])
def test_delivery_approved_projection_actual_jpeg(tmp_path,mode):
    from photo_worker.contracts.metadata import MetadataDocument
    path=tmp_path/'approved.jpg'
    params,source,manifest,profile=fixture(path)
    exif=Image.Exif();exif[274]=1;exif[272]='Changed' if mode=='changed-camera' else 'Camera'
    if mode=='extra-author':exif[315]='Unapproved Author'
    Image.new('RGB',(4,3),'red').save(path,format='JPEG',icc_profile=profile,exif=exif)
    manifest=manifest.model_copy(update={'metadata_policy':'safe_camera',
        'artifact':ArtifactRef(artifact_id=manifest.artifact.artifact_id,sha256=sha256(path.read_bytes()).hexdigest())})
    kwargs={} if mode=='missing-authority' else {'metadata_document':MetadataDocument(camera_model='Camera'),
        'metadata_policy':'copyright_only' if mode=='wrong-policy' else 'safe_camera'}
    if mode=='valid':
        result=verify_delivery_encoded_file(path,params,source,manifest,max_bytes=100000,max_pixels=12,**kwargs)
        assert result.sha256==manifest.artifact.sha256
    else:
        with pytest.raises(ValueError):
            verify_delivery_encoded_file(path,params,source,manifest,max_bytes=100000,max_pixels=12,**kwargs)


@pytest.mark.parametrize('mode',['valid','widened-fields','wrong-version','extra-camera','wrong-manifest'])
def test_delivery_physical_verification_uses_task_frozen_metadata(tmp_path,mode):
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.metadata import MetadataDocument
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.v2.delivery_metadata_freeze import freeze_delivery_metadata
    from photo_worker.v2.delivery_metadata_encoded_verify import verify_delivery_metadata_encoded_file
    path=tmp_path/'frozen.jpg'
    params,source,manifest,profile=fixture(path)
    binding=VersionBinding(asset_version_id=uuid4(),source=params.image,canvas_hash=sha256(canonical_bytes(source.canvas)).hexdigest())
    metadata=canonical_bytes(MetadataDocument(author='Author',camera_model='Camera'))
    metadata_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(metadata).hexdigest())
    raw=freeze_delivery_metadata(binding,metadata_ref,metadata,'approved_exif',approved_fields=('author',))
    exif=Image.Exif();exif[315]='Author'
    if mode=='extra-camera':exif[272]='Camera'
    Image.new('RGB',(4,3),'red').save(path,format='JPEG',icc_profile=profile,exif=exif)
    manifest=manifest.model_copy(update={'metadata_policy':'copyright_only' if mode=='wrong-manifest' else 'safe_camera',
        'artifact':ArtifactRef(artifact_id=manifest.artifact.artifact_id,sha256=sha256(path.read_bytes()).hexdigest())})
    if mode=='wrong-version':binding=binding.model_copy(update={'asset_version_id':uuid4()})
    def run():return verify_delivery_metadata_encoded_file(path,params,source,manifest,raw,sha256(raw).hexdigest(),
        binding,metadata_ref,metadata,policy='approved_exif',
        approved_fields=('author','camera_model') if mode=='widened-fields' else ('author',),max_bytes=100000,max_pixels=12)
    if mode=='valid':assert run().sha256==manifest.artifact.sha256
    else:
        with pytest.raises(ValueError):run()


@pytest.mark.parametrize('mode',['valid','missing-author','pixels','tool-error','cancel','caller-mutation'])
def test_frozen_metadata_physical_rewrite_and_cleanup(tmp_path,mode):
    import subprocess
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.metadata import MetadataDocument
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.v2.delivery_metadata_freeze import freeze_delivery_metadata
    from photo_worker.v2.delivery_metadata_rewrite import rewrite_delivery_metadata
    original=(tmp_path/'original.jpg').resolve();output=(tmp_path/'rewritten.jpg').resolve()
    params,source,encoded,profile=fixture(original)
    original_bytes=original.read_bytes()
    binding=VersionBinding(asset_version_id=uuid4(),source=params.image,canvas_hash=sha256(canonical_bytes(source.canvas)).hexdigest())
    metadata=canonical_bytes(MetadataDocument(author='Author'))
    metadata_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(metadata).hexdigest())
    raw=freeze_delivery_metadata(binding,metadata_ref,metadata,'copyright')
    def runner(args,active):
        active()
        if mode=='cancel':raise RuntimeError('cancelled')
        if mode=='tool-error':return subprocess.CompletedProcess(args,1)
        if mode=='caller-mutation':
            encoded.width=200
            params.icc_sha256='0'*64
            source.canvas.width=200
            binding.canvas_hash='0'*64
        exif=Image.Exif();exif[274]=1
        if mode!='missing-author':exif[315]='Author'
        if mode=='pixels':Image.new('RGB',(4,3),'blue').save(output,format='JPEG',icc_profile=profile,exif=exif)
        else:
            # Insert APP1 without JPEG pixel recompression for a controlled writer.
            body=output.read_bytes();payload=exif.tobytes()
            output.write_bytes(body[:2]+b'\xff\xe1'+(len(payload)+2).to_bytes(2,'big')+payload+body[2:])
        return subprocess.CompletedProcess(args,0)
    def run():return rewrite_delivery_metadata(original,output,'/qualified/exiftool',
        ArtifactRef(artifact_id=uuid4(),sha256='e'*64),encoded,params,source,uuid4(),raw,sha256(raw).hexdigest(),
        binding,metadata_ref,metadata,policy='copyright',max_bytes=100000,max_pixels=12,runner=runner)
    if mode in ('valid','caller-mutation'):
        result=run();assert result.sha256==sha256(output.read_bytes()).hexdigest()
        assert result.copied_fields==('author',)
    else:
        with pytest.raises(RuntimeError if mode=='cancel' else ValueError):run()
        assert not output.exists()
    assert original.read_bytes()==original_bytes


@pytest.mark.parametrize('byte_limit,pixel_limit',[(2**53,12),(100000,200000001),(True,12),(100000,True)])
def test_delivery_physical_limits_reject_unsafe_installation(tmp_path,byte_limit,pixel_limit):
    path=tmp_path/'limits.jpg';params,source,manifest,_=fixture(path)
    with pytest.raises(ValueError,match='LIMIT_INVALID'):
        verify_delivery_encoded_file(path,params,source,manifest,max_bytes=byte_limit,max_pixels=pixel_limit)


@pytest.mark.parametrize('mode',['valid','pixels','missing-author'])
def test_independent_metadata_file_pair(tmp_path,mode):
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.metadata import MetadataDocument,MetadataParams,MetadataReport
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.v2.delivery_metadata_freeze import freeze_delivery_metadata
    from photo_worker.v2.delivery_metadata_files import verify_delivery_metadata_files
    original=tmp_path/'before.jpg';output=tmp_path/'after.jpg'
    encode_params,image,before,profile=fixture(original)
    task=uuid4();encoded_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(canonical_bytes(before)).hexdigest())
    binding=VersionBinding(asset_version_id=uuid4(),source=encode_params.image,canvas_hash=sha256(canonical_bytes(image.canvas)).hexdigest())
    metadata=canonical_bytes(MetadataDocument(author='Author',camera_model='Private camera'))
    metadata_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(metadata).hexdigest())
    raw=freeze_delivery_metadata(binding,metadata_ref,metadata,'copyright')
    projection=canonical_bytes(MetadataDocument(author='Author'))
    from uuid import uuid5
    projected=ArtifactRef(artifact_id=uuid5(task,'delivery:metadata:projection:1'),sha256=sha256(projection).hexdigest())
    params=MetadataParams(encoded=encoded_ref,document=projected,policy='copyright_only')
    exif=Image.Exif();exif[274]=1
    if mode!='missing-author':exif[315]='Author'
    if mode=='pixels':Image.new('RGB',(4,3),'blue').save(output,format='JPEG',icc_profile=profile,exif=exif)
    else:
        body=original.read_bytes();payload=exif.tobytes()
        output.write_bytes(body[:2]+b'\xff\xe1'+(len(payload)+2).to_bytes(2,'big')+payload+body[2:])
    after=before.model_copy(update={'artifact':ArtifactRef(artifact_id=uuid4(),sha256=sha256(output.read_bytes()).hexdigest()),'metadata_policy':'copyright_only'})
    report=MetadataReport(encoded=encoded_ref,document=projected,policy='copyright_only',copied_fields=['author'],orientation=1,stale_previews_removed=True,icc_preserved=True)
    def run():return verify_delivery_metadata_files(original,output,task,encoded_ref,params,before,after,report,
        raw,sha256(raw).hexdigest(),binding,metadata_ref,metadata,policy='copyright',max_bytes=100000,max_pixels=12)
    if mode=='valid':assert run().raster.sha256==after.artifact.sha256
    else:
        with pytest.raises(ValueError):run()
