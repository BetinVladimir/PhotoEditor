from types import SimpleNamespace
import pytest
from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
from photo_worker.operations.semantic.preview_background_cleanup_installation import sql_background_cleanup_preview_factory


def options(tmp_path):
    return dict(database=SimpleNamespace(
        admit_semantic_background_cleanup_preview=lambda *a:True,
        background_cleanup_preview_protection_origin=lambda *a:True,
        background_cleanup_preview_report_evidence=lambda *a:{}),
        objects=SimpleNamespace(get_object=lambda *a:None), bucket='photos',
        icc_registry=None, model_lock_sha='a'*64, runtime_lock_sha='b'*64,
        device_profile_sha='c'*64,dataset_sha='d'*64, backend='mps',
        presentation=PreviewPresentationPins(tmp_path,'e'*64,'f'*64))


@pytest.mark.parametrize('field,value', [('backend','unknown'),('presentation',None),
    ('model_lock_sha','bad'),('objects',None),('database',None)])
def test_installer_rejects_missing_authority_or_pins(tmp_path, field, value):
    args=options(tmp_path);args[field]=value
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        sql_background_cleanup_preview_factory(**args)


def test_exposure_sql_methods_cannot_install_background_cleanup(tmp_path):
    args=options(tmp_path)
    args['database']=SimpleNamespace(admit_semantic_exposure_preview=lambda *a:True,
        exposure_preview_protection_origin=lambda *a:True,
        exposure_preview_report_evidence=lambda *a:{})
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        sql_background_cleanup_preview_factory(**args)


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
def test_background_cleanup_factory_constructs_own_installer_without_admission_io(tmp_path,monkeypatch,backend):
    from photo_worker.operations.semantic import preview_background_cleanup_installation as module
    calls=[]
    monkeypatch.setattr(module,'local_exposure_icc_verifier',lambda registry: calls.append(registry) or (lambda color:True))
    args=options(tmp_path);args['backend']=backend
    def unexpected(*a):raise AssertionError('startup must not admit or read task evidence')
    args['database']=SimpleNamespace(admit_semantic_background_cleanup_preview=unexpected,
        background_cleanup_preview_protection_origin=unexpected,background_cleanup_preview_report_evidence=unexpected)
    args['objects']=SimpleNamespace(get_object=unexpected)
    factory=sql_background_cleanup_preview_factory(**args)
    assert callable(factory) and calls==[args['icc_registry']]
