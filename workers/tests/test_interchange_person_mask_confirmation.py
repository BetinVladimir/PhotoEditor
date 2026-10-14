from uuid import uuid4
import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.interchange.person_mask_confirmation import SQLImportPersonMaskConfirmation
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.interchange_plan import InterchangeImportParams
from test_interchange_import_adapter import adapter_fixture
from test_interchange_import_stage import adopted_fixture


def _mask_and_binding():
    _, sources, _, raw = adopted_fixture()
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.base import canonical_hash
    image = WorkingImage.model_validate_json(raw)
    source = sources.entries[0]
    binding = VersionBinding(asset_version_id=source.asset_version_id,
        source=source.master, canvas_hash=canonical_hash(image.canvas))
    mask = MaskRef(artifact=ArtifactRef(artifact_id=uuid4(), sha256='c'*64),
        canvas=image.canvas, transform_chain=image.transform_chain,
        object_id=None, selector='person', confidence=1,
        model_lock_id='d'*64, protected=True)
    return mask, binding


def test_import_person_mask_confirmation_uses_current_leased_sql():
    ctx, artifacts, admission, _, docs = adapter_fixture()
    mask, binding = _mask_and_binding()
    authority = admission.admit(ctx, artifacts.params(InterchangeImportParams))
    calls = []
    class Database:
        def interchange_import_person_mask_ready(self):
            return True
        def interchange_import_person_mask_live(self, *args):
            calls.append(args)
            return 7 if len(calls) == 1 else None
    confirm = SQLImportPersonMaskConfirmation(Database())
    assert confirm(ctx, artifacts, authority, mask, binding) == 7
    assert calls[0] == (ctx.lease, docs.params_jcs, authority.workflow_id,
        authority.project_id, binding.asset_version_id,
        mask.artifact.artifact_id, mask.artifact.sha256)
    assert confirm(ctx, artifacts, authority, mask, binding) is None


def test_import_person_mask_confirmation_requires_worker_sql_abi():
    class Database:
        def interchange_import_person_mask_ready(self):
            return False
        def interchange_import_person_mask_live(self, *_):
            pytest.fail('uninstalled SQL was called')
    with pytest.raises(ValueError, match='INSTALLATION'):
        SQLImportPersonMaskConfirmation(Database())
