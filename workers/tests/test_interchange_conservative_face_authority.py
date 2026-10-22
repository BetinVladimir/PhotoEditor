"""Face-bearing interchange never treats archived geometry as ancestry."""
from types import SimpleNamespace
from photo_worker.v2.interchange_conservative_faces import qualify_conservative_faces


def test_face_branch_requires_independent_original_producer_authority():
    def forbidden(*_):
        raise AssertionError('uninstalled face authority performed document IO')
    assert qualify_conservative_faces(None, None, None, None, forbidden, None, forbidden) is False


def test_face_branch_rejects_report_face_inventory_mismatch_before_io():
    report = SimpleNamespace(params=SimpleNamespace(faces=[object()]), faces=[])
    def forbidden(*_):
        raise AssertionError('mismatched inventory performed document IO')
    assert qualify_conservative_faces(report, None, None, None, forbidden, lambda *_: True, forbidden) is False
