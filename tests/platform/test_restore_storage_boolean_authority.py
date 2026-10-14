"""Storage composition must honor direct activity denial before any adapter I/O."""
import pytest
from photo_worker.runtime.restore_storage import verify_minio_restore_storage


def test_storage_composer_explicit_false_refuses_before_client_or_spool(tmp_path):
    class NoClient:
        def __getattribute__(self,name):
            raise AssertionError('object-store touched before authority')
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        verify_minio_restore_storage(None,NoClient(),['photos'],tmp_path,
            lambda _:None,check_active=lambda:False)
    assert list(tmp_path.iterdir())==[]
