"""Completed import confirmation uses current scheduler result and live grant."""

from types import SimpleNamespace
from uuid import uuid4

from photo_worker.v2.interchange_import_adoption_mask_confirmation import SchedulerImportPersonMaskConfirmation
from test_interchange_person_mask_confirmation import _mask_and_binding


def test_scheduler_import_mask_confirmation_requires_live_epoch_and_stable_recheck():
    mask, binding = _mask_and_binding()
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    rows, queries = [(4,), (4,), (5,)], []
    class Connection:
        autocommit = True
        def execute(self, query, args):
            queries.append((query, args))
            return SimpleNamespace(fetchone=lambda: rows.pop(0))
    authority = SchedulerImportPersonMaskConfirmation(Connection(), scope, lambda: None)
    assert authority(object(), mask, binding) is True
    assert authority(object(), mask, binding) is True
    assert authority(object(), mask, binding) is False
    assert len(queries) == 3
    assert all('v30_import_adoption_person_mask_live' in query for query, _ in queries)
    assert queries[0][1] == scope + (binding.asset_version_id,
        mask.artifact.artifact_id, mask.artifact.sha256)


def test_scheduler_import_mask_confirmation_rejects_missing_grant_and_extra_veto():
    mask, binding = _mask_and_binding()
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    class Connection:
        autocommit = True
        def __init__(self):
            self.epoch = None
        def execute(self, *_args):
            return SimpleNamespace(fetchone=lambda: (self.epoch,))
    connection = Connection()
    authority = SchedulerImportPersonMaskConfirmation(connection, scope,
        lambda: None, lambda *_args: False)
    assert authority(object(), mask, binding) is False
    connection.epoch = 4
    assert authority(object(), mask, binding) is False
    assert authority(object(), mask.model_copy(update={'object_id': uuid4()}), binding) is False
