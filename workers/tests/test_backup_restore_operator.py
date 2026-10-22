from hashlib import sha256
import io
import tarfile
import pytest

from photo_worker.runtime.backup_restore_operator import extract_empty_archive


def archive(tmp_path,name,kind=tarfile.REGTYPE,body=b'bytes',link=''):
    path=tmp_path/'archive.tar'
    with tarfile.open(path,'w') as writer:
        entry=tarfile.TarInfo(name);entry.type=kind;entry.linkname=link
        entry.size=len(body) if kind==tarfile.REGTYPE else 0
        writer.addfile(entry,io.BytesIO(body) if kind==tarfile.REGTYPE else None)
    return path


def test_regular_data_preserved(tmp_path):
    target=tmp_path/'target';target.mkdir(mode=0o700)
    path=archive(tmp_path,'./bucket/object/version/xl.meta')
    extract_empty_archive(path,target,maximum_bytes=100,check_active=lambda:True)
    assert (target/'bucket/object/version/xl.meta').read_bytes()==b'bytes'


@pytest.mark.parametrize('name,kind,link',[
    ('../outside',tarfile.REGTYPE,''),('/absolute',tarfile.REGTYPE,''),
    ('link',tarfile.SYMTYPE,'/etc/passwd'),('hardlink',tarfile.LNKTYPE,'other'),
    ('device',tarfile.CHRTYPE,''),
])
def test_archive_authority_refuses_traversal_and_special_entries(tmp_path,name,kind,link):
    target=tmp_path/'target';target.mkdir(mode=0o700)
    with pytest.raises(ValueError,match='ARCHIVE_ENTRY'):
        extract_empty_archive(archive(tmp_path,name,kind,link=link),target,maximum_bytes=100,check_active=lambda:True)
    assert not list(target.iterdir())


def test_nonempty_target_never_overwritten(tmp_path):
    target=tmp_path/'target';target.mkdir(mode=0o700);(target/'existing').write_bytes(b'original')
    with pytest.raises(ValueError,match='TARGET_NOT_EMPTY'):
        extract_empty_archive(archive(tmp_path,'existing'),target,maximum_bytes=100,check_active=lambda:True)
    assert (target/'existing').read_bytes()==b'original'


def test_revoked_operator_does_not_extract_file(tmp_path):
    target=tmp_path/'target';target.mkdir(mode=0o700)
    with pytest.raises(RuntimeError,match='OPERATOR_INACTIVE'):
        extract_empty_archive(archive(tmp_path,'object'),target,maximum_bytes=100,check_active=lambda:False)
    assert not list(target.iterdir())


def test_uncompressed_byte_limit_precedes_file_write(tmp_path):
    target=tmp_path/'target';target.mkdir(mode=0o700)
    with pytest.raises(ValueError,match='ARCHIVE_LIMIT'):
        extract_empty_archive(archive(tmp_path,'object',body=b'too-large'),target,maximum_bytes=3,check_active=lambda:True)
    assert not list(target.iterdir())
