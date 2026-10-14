"""Controlled writer tests; no installed ExifTool qualification is claimed."""
from contextlib import nullcontext
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

from PIL import Image
import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataDocument, MetadataParams
from photo_worker.operations.metadata.write import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.image_fingerprint import decoded_image_fingerprint


def ref():
    return ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)


@pytest.mark.parametrize('mode', ['valid','pixels','missing_author','changed_author','private_exif','late_cancel','tool_timeout','tool_missing'])
def test_metadata_writer_refuses_pixel_changes_before_publication(tmp_path, mode):
    source, output = tmp_path / 'source.png', tmp_path / 'metadata.png'
    icc = b'controlled-test-profile'
    with Image.new('RGB', (8, 8), (20, 30, 40)) as image:
        exif = Image.Exif()
        exif[274] = 7
        image.save(source, format='PNG', icc_profile=icc, exif=exif)
    encoded = EncodedImage(artifact=ref(), format='png', width=8, height=8,
                          icc_id='srgb_v1', icc_sha256=sha256(icc).hexdigest(),
                          alpha=False, metadata_policy='strip')
    params = MetadataParams(encoded=ref(), document=ref(), policy='copyright_only')
    document = MetadataDocument(author='Test')
    published = []
    writer_finished=False

    class Artifacts:
        scratch = SimpleNamespace(reserve=lambda size: nullcontext(SimpleNamespace(
            path=lambda *args: output, validate_file=lambda path: None)))

        def params(self, contract):
            return params

        def contract(self, reference, contract):
            return encoded if reference == params.encoded else document

        def input(self, reference, **kwargs):
            return nullcontext(SimpleNamespace(path=source))

        def stage(self, *args):
            published.append('image')
            return ref()

        def stage_contract(self, *args):
            published.append('document')
            return ref()

    def writer(*args, **kwargs):
        nonlocal writer_finished
        if mode=='tool_timeout':
            import subprocess
            raise subprocess.TimeoutExpired(args[0],120)
        if mode=='tool_missing':raise FileNotFoundError('qualified tool missing')
        with Image.open(output) as image:
            image.load()
            if mode=='pixels':
                image.putpixel((0, 0), (255, 0, 0))
            exif = Image.Exif()
            exif[274] = 1
            if mode!='missing_author':exif[315] = 'Changed' if mode=='changed_author' else 'Test'
            if mode=='private_exif':exif[305]='Private software' 
            image.save(output, format='PNG', icc_profile=icc, exif=exif)
        writer_finished=True
        return SimpleNamespace(returncode=0)

    def active():
        if mode=='late_cancel' and writer_finished:raise RuntimeError('lease lost after writer')

    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend': 'cpu'},
        command=SimpleNamespace(inputs=[params.encoded, encoded.artifact, params.document])),
        check_active=active, progress=lambda *args: None)
    if mode!='valid':
        with pytest.raises(RuntimeError if mode=='late_cancel' else OperationFailure) as failure:
            execute(ctx, Artifacts(), '/controlled/exiftool', runner=writer)
        assert published == []
        if mode in ('tool_timeout','tool_missing'):
            assert failure.value.error.code==('TIMEOUT' if mode=='tool_timeout' else 'DEPENDENCY_UNAVAILABLE')
            assert failure.value.error.retryable is True
    else:
        result = execute(ctx, Artifacts(), '/controlled/exiftool', runner=writer)
        assert len(result.outputs) == 3
        assert published == ['image', 'document', 'document']


def test_fingerprint_detects_palette_changes_with_identical_indices(tmp_path):
    paths = [tmp_path / 'one.png', tmp_path / 'two.png']
    for index, path in enumerate(paths):
        with Image.new('P', (8, 8), 0) as image:
            palette = [0] * 768
            palette[0] = index * 255
            image.putpalette(palette)
            image.save(path)
    with paths[0].open('rb') as one, paths[1].open('rb') as two:
        assert decoded_image_fingerprint(one, 'image/png') != decoded_image_fingerprint(two, 'image/png')


def test_fingerprint_checks_cancellation_before_decode():
    def cancelled():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        decoded_image_fingerprint(None, 'image/png', check_active=cancelled)



def test_fingerprint_detects_low_bit_rgb16_tiff_changes(tmp_path):
    import numpy as np
    import tifffile
    one,two=tmp_path/'one.tiff',tmp_path/'two.tiff'
    pixels=np.full((3,4,3),32768,dtype=np.uint16)
    tifffile.imwrite(one,pixels,photometric='rgb',metadata=None,software=None)
    pixels[0,0,0]+=1
    tifffile.imwrite(two,pixels,photometric='rgb',metadata=None,software=None)
    with one.open('rb') as original,two.open('rb') as changed:
        assert decoded_image_fingerprint(original,'image/tiff')!=decoded_image_fingerprint(changed,'image/tiff')


def test_fingerprint_tiff_pixel_identity_is_endian_independent(tmp_path):
    import numpy as np
    import tifffile
    one,two=tmp_path/'little.tiff',tmp_path/'big.tiff'
    pixels=np.arange(36,dtype=np.uint16).reshape(3,4,3)
    tifffile.imwrite(one,pixels,photometric='rgb',byteorder='<',metadata=None,software=None)
    tifffile.imwrite(two,pixels,photometric='rgb',byteorder='>',metadata=None,software=None)
    with one.open('rb') as original,two.open('rb') as rewritten:
        assert decoded_image_fingerprint(original,'image/tiff')==decoded_image_fingerprint(rewritten,'image/tiff')



@pytest.mark.parametrize('channels',[3,4])
def test_fingerprint_detects_low_bit_png16_color_and_alpha(tmp_path,channels):
    import cv2
    import numpy as np
    one,two=tmp_path/'one.png',tmp_path/'two.png'
    pixels=np.full((3,4,channels),32768,dtype=np.uint16)
    assert cv2.imwrite(str(one),pixels)
    pixels[0,0,channels-1]+=1
    assert cv2.imwrite(str(two),pixels)
    with one.open('rb') as original,two.open('rb') as changed:
        assert decoded_image_fingerprint(original,'image/png')!=decoded_image_fingerprint(changed,'image/png')


def test_fingerprint_png16_identity_survives_compression_change(tmp_path):
    import cv2
    import numpy as np
    one,two=tmp_path/'one.png',tmp_path/'two.png'
    pixels=np.arange(36,dtype=np.uint16).reshape(3,4,3)
    assert cv2.imwrite(str(one),pixels,[cv2.IMWRITE_PNG_COMPRESSION,0])
    assert cv2.imwrite(str(two),pixels,[cv2.IMWRITE_PNG_COMPRESSION,9])
    with one.open('rb') as original,two.open('rb') as rewritten:
        assert decoded_image_fingerprint(original,'image/png')==decoded_image_fingerprint(rewritten,'image/png')
