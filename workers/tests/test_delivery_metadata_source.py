from hashlib import sha256
from uuid import uuid4
from PIL import Image
import sys
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.delivery_metadata_source import prepare_delivery_metadata_source


@pytest.mark.parametrize('mode',['valid','sha','size','mime','symlink','hardlink','cancel'])
def test_delivery_metadata_source_verified_bytes(tmp_path,mode):
    path=(tmp_path/'original.jpg').resolve()
    exif=Image.Exif();exif[315]='Author';exif[305]='private software'
    Image.new('RGB',(4,3)).save(path,exif=exif)
    size=path.stat().st_size
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
    if mode=='sha':ref=ref.model_copy(update={'sha256':'a'*64})
    if mode=='symlink':
        link=tmp_path/'link';link.symlink_to(path);path=link
    if mode=='hardlink':(tmp_path/'alias').hardlink_to(path)
    def active():
        if mode=='cancel':raise RuntimeError('cancelled')
    def run():return prepare_delivery_metadata_source(path,ref,'image/png' if mode=='mime' else 'image/jpeg',size+1 if mode=='size' else size,max_bytes=100000,check_active=active)
    if mode=='valid':
        result=run()
        assert result.original_bytes==size and result.metadata_sha256==sha256(result.metadata_jcs).hexdigest()
        assert b'Author' in result.metadata_jcs and b'private software' not in result.metadata_jcs
    else:
        with pytest.raises(RuntimeError if mode=='cancel' else (ValueError,OSError)):run()


def test_raw_source_hash_and_filetype_are_checked_before_return(tmp_path):
    path=(tmp_path/'original.dng').resolve();path.write_bytes(b'raw fixture bytes')
    executable=tmp_path/'exiftool';executable.write_text('#!'+sys.executable+'\n'
        'import sys\nsys.stdout.write(\'[{"File:FileType":"DNG","IFD0:Artist":"A"}]\')\n')
    executable.chmod(0o700)
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
    result=prepare_delivery_metadata_source(path,ref,'image/x-adobe-dng',path.stat().st_size,
        max_bytes=100000,raw_executable=str(executable),raw_tool_sha=sha256(executable.read_bytes()).hexdigest())
    assert b'"author":"A"' in result.metadata_jcs
    with pytest.raises(ValueError,match='SOURCE_SHA'):
        prepare_delivery_metadata_source(path,ref.model_copy(update={'sha256':'a'*64}),
            'image/x-adobe-dng',path.stat().st_size,max_bytes=100000,raw_executable=str(executable))
    with pytest.raises(ValueError,match='RAW_TOOL_UNAVAILABLE'):
        prepare_delivery_metadata_source(path,ref,'image/x-adobe-dng',path.stat().st_size,
            max_bytes=100000)
    with pytest.raises(ValueError,match='TOOL_CHANGED'):
        prepare_delivery_metadata_source(path,ref,'image/x-adobe-dng',path.stat().st_size,
            max_bytes=100000,raw_executable=str(executable),raw_tool_sha='a'*64)
