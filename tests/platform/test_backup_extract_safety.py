"""Actual archive-extraction boundaries; authored only, execution deferred."""
import io
import tarfile
import pytest
from photo_worker.runtime.backup_restore_operator import extract_empty_archive


def archive(tmp_path, entries):
    path=tmp_path/'checkpoint.tar'
    with tarfile.open(path,'w') as output:
        for name,kind,body in entries:
            entry=tarfile.TarInfo(name);entry.type=kind
            if kind==tarfile.REGTYPE:
                entry.size=len(body);output.addfile(entry,io.BytesIO(body))
            else:
                entry.linkname='../../outside';output.addfile(entry)
    return path


def target(tmp_path):
    root=tmp_path/'target';root.mkdir(mode=0o700);return root


def test_extract_materializes_private_regular_files_without_tar_permissions(tmp_path):
    path=archive(tmp_path,[('./',tarfile.DIRTYPE,b''),
        ('./nested',tarfile.DIRTYPE,b''),('./nested/file',tarfile.REGTYPE,b'photo')])
    root=target(tmp_path)
    extract_empty_archive(path,root,maximum_bytes=5,check_active=lambda:None)
    assert (root/'nested/file').read_bytes()==b'photo'
    assert (root/'nested/file').stat().st_mode&0o777==0o600
    assert (root/'nested').stat().st_mode&0o777==0o700


@pytest.mark.parametrize('name,kind', [
    ('../outside',tarfile.REGTYPE),('/outside',tarfile.REGTYPE),
    ('linked',tarfile.SYMTYPE),('linked',tarfile.LNKTYPE),
    ('device',tarfile.CHRTYPE),('pipe',tarfile.FIFOTYPE)])
def test_extract_refuses_path_escape_and_nonregular_entries(tmp_path,name,kind):
    path=archive(tmp_path,[(name,kind,b'photo')]);root=target(tmp_path)
    with pytest.raises(ValueError,match='RESTORE_ARCHIVE_ENTRY'):
        extract_empty_archive(path,root,maximum_bytes=100,check_active=lambda:None)
    assert list(root.iterdir())==[]
    assert not (tmp_path/'outside').exists()


def test_extract_refuses_canonical_duplicate_path_without_overwriting(tmp_path):
    path=archive(tmp_path,[('./file',tarfile.REGTYPE,b'first'),('file',tarfile.REGTYPE,b'second')])
    root=target(tmp_path)
    with pytest.raises(ValueError,match='RESTORE_ARCHIVE_LIMIT'):
        extract_empty_archive(path,root,maximum_bytes=100,check_active=lambda:None)
    assert (root/'file').read_bytes()==b'first'


def test_extract_refuses_nonempty_target_and_byte_overflow(tmp_path):
    path=archive(tmp_path,[('file',tarfile.REGTYPE,b'photo')]);root=target(tmp_path)
    (root/'existing').write_bytes(b'keep')
    with pytest.raises(ValueError,match='RESTORE_TARGET_NOT_EMPTY_PRIVATE'):
        extract_empty_archive(path,root,maximum_bytes=100,check_active=lambda:None)
    assert (root/'existing').read_bytes()==b'keep'
    (root/'existing').unlink()
    with pytest.raises(ValueError,match='RESTORE_ARCHIVE_LIMIT'):
        extract_empty_archive(path,root,maximum_bytes=4,check_active=lambda:None)
    assert list(root.iterdir())==[]


def test_extract_observes_revocation_before_first_write(tmp_path):
    path=archive(tmp_path,[('file',tarfile.REGTYPE,b'photo')]);root=target(tmp_path)
    with pytest.raises(RuntimeError,match='RESTORE_OPERATOR_INACTIVE'):
        extract_empty_archive(path,root,maximum_bytes=100,check_active=lambda:False)
    assert list(root.iterdir())==[]
