from types import SimpleNamespace
import pytest
from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
from photo_worker.operations.semantic.preview_removal_installation import sql_removal_preview_factory


def options(tmp_path):
    return dict(database=SimpleNamespace(
        admit_semantic_removal_preview=lambda *a:True,
        removal_preview_protection_origin=lambda *a:True,
        removal_preview_report_evidence=lambda *a:{}),
        objects=SimpleNamespace(get_object=lambda *a:None), bucket='photos',
        icc_registry=None, model_lock_sha='a'*64, runtime_lock_sha='b'*64,
        device_profile_sha='c'*64,dataset_sha='d'*64, backend='mps',
        presentation=PreviewPresentationPins(tmp_path,'e'*64,'f'*64))


@pytest.mark.parametrize('field,value', [('backend','unknown'),('presentation',None),
    ('model_lock_sha','bad'),('objects',None),('database',None)])
def test_installer_rejects_missing_authority_or_pins(tmp_path, field, value):
    args=options(tmp_path);args[field]=value
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        sql_removal_preview_factory(**args)


def test_exposure_sql_methods_cannot_install_removal(tmp_path):
    args=options(tmp_path)
    args['database']=SimpleNamespace(admit_semantic_exposure_preview=lambda *a:True,
        exposure_preview_protection_origin=lambda *a:True,
        exposure_preview_report_evidence=lambda *a:{})
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        sql_removal_preview_factory(**args)
