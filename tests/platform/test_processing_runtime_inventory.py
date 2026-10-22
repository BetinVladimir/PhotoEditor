"""Processor implementation changes must affect model-runtime qualification."""
from types import SimpleNamespace
import pytest
from photo_worker.models import processing_inventory as inventory


def test_processor_inventory_includes_python_native_and_versioned_vendor_libraries(monkeypatch,tmp_path):
    packages={}
    for name in ('transformers','PIL'):
        root=tmp_path/name;root.mkdir();(root/'__init__.py').write_text('')
        (root/'processor.py').write_text('version = 1')
        (root/'_native.so').write_bytes(b'native')
        packages[name]=SimpleNamespace(origin=str(root/'__init__.py'))
    vendor=tmp_path/'pillow.libs';vendor.mkdir();(vendor/'liblcms2-hash.so.2.0').write_bytes(b'lcms')
    monkeypatch.setattr(inventory,'find_spec',lambda name:packages[name])
    paths=inventory.processing_component_paths()
    assert paths['processor/transformers/processor.py']==tmp_path/'transformers/processor.py'
    assert paths['processor/PIL/_native.so']==tmp_path/'PIL/_native.so'
    assert paths['processor/pillow.libs/liblcms2-hash.so.2.0']==vendor/'liblcms2-hash.so.2.0'
    assert 'processing_inventory_source' in paths


def test_processor_inventory_rejects_mutable_external_symlink(monkeypatch,tmp_path):
    for name in ('transformers','PIL'):
        root=tmp_path/name;root.mkdir();(root/'__init__.py').write_text('')
    (tmp_path/'transformers'/'processor.py').symlink_to(tmp_path/'PIL'/'__init__.py')
    monkeypatch.setattr(inventory,'find_spec',lambda name:SimpleNamespace(origin=str(tmp_path/name/'__init__.py')))
    with pytest.raises(ValueError,match='SYMLINK'):inventory.processing_component_paths()
