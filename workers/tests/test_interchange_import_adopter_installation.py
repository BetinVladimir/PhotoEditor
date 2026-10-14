"""Process-owned import adopter installation refuses incomplete authority."""

from hashlib import sha256
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_adopter_installation import SchedulerImportRecipeAdoptionInstallation


def installed(tmp_path, *, runtime=lambda *_args: True):
    schemas = canonical_bytes({'$defs': {}})
    return SchedulerImportRecipeAdoptionInstallation(
        SimpleNamespace(autocommit=True), schemas_jcs=schemas,
        schemas_sha=sha256(schemas).hexdigest(), work_icc_sha='a' * 64,
        scratch_root=Path(tmp_path), scratch_bytes=1 << 20,
        max_file_bytes=1 << 20, qualify_runtime=runtime,
        check_active=lambda: None)


def test_import_adopter_requires_process_runtime_and_pinned_schemas(tmp_path):
    with pytest.raises(ValueError, match='INSTALLATION'):
        installed(tmp_path, runtime=None)
    schemas = canonical_bytes({'$defs': {}})
    with pytest.raises(ValueError, match='INSTALLATION'):
        SchedulerImportRecipeAdoptionInstallation(
            SimpleNamespace(autocommit=True), schemas_jcs=schemas,
            schemas_sha='0' * 64, work_icc_sha='a' * 64,
            scratch_root=Path(tmp_path), scratch_bytes=1 << 20,
            max_file_bytes=1 << 20, qualify_runtime=lambda *_args: True,
            check_active=lambda: None)


def test_import_adopter_rejects_foreign_context_before_sql_or_io(tmp_path):
    factory = installed(tmp_path)
    context = SimpleNamespace(operation='v2.interchange.export', version='1',
                              attempt=1, fence=1, result_id=uuid4())
    with pytest.raises(AdoptionInvalid, match='CONTEXT'):
        with factory(object(), uuid4(), uuid4(), context):
            pytest.fail('foreign context yielded a qualifier')
