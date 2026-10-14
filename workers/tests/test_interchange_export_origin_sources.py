from types import SimpleNamespace
from uuid import uuid5
import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.interchange.export_bundle_factory import qualify_declared_person_origin_sources
from photo_worker.v2.interchange_export_prepare import ExportSource
from photo_worker.v2.interchange_manifest import BundlePersonOrigin
from test_interchange_person_origin import fixture


def test_export_origin_sources_require_declared_physical_json_and_deterministic_receipt():
    raw,scope,_=fixture()
    identity=uuid5(scope.entry_id,f'v30:person-origin:{scope.mask_ref.artifact_id}')
    path=f'origins/{scope.entry_id}/{scope.mask_ref.artifact_id}.json'
    from hashlib import sha256
    receipt=ExportSource(ArtifactRef(artifact_id=identity,sha256=sha256(raw).hexdigest()),
        path,'PersonOriginReceipt',len(raw),True)
    mask=ExportSource(scope.mask_ref,f'dependencies/{scope.mask_ref.artifact_id}.json',
        'MaskRef',100,True)
    mapping=BundlePersonOrigin(entry_id=scope.entry_id,recipe=scope.recipe,
        override=scope.override,mask_ref=scope.mask_ref,mask_pixels=scope.mask_pixels,
        path=path,sha256=receipt.reference.sha256)
    sources=(receipt,mask)
    entries={(item.reference.artifact_id,item.reference.sha256):SimpleNamespace(contract=item.contract)
        for item in sources}
    reader=SimpleNamespace(entries=entries,evidence=lambda ref:SimpleNamespace(
        bytes=next(item.bytes for item in sources if item.reference==ref),media_type='application/json'))
    qualify_declared_person_origin_sources(reader,sources,(mapping,))
    del entries[(receipt.reference.artifact_id,receipt.reference.sha256)]
    with pytest.raises(ValueError,match='UNDECLARED'):
        qualify_declared_person_origin_sources(reader,sources,(mapping,))
    entries[(receipt.reference.artifact_id,receipt.reference.sha256)]=SimpleNamespace(contract=receipt.contract)
    changed=mapping.model_copy(deep=True)
    changed.path='origins/foreign.json'
    with pytest.raises(ValueError,match='MAPPING'):
        qualify_declared_person_origin_sources(reader,sources,(changed,))
