from types import SimpleNamespace as NS
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.export import EncodeParams,ExportSize
from photo_worker.runtime import restore_export_geometry as m


@pytest.mark.parametrize('mode',['valid','resize','upscale_blocked','version','source','width','alpha'])
def test_export_geometry_uses_production_dimensions_and_adopted_source(mode):
    version,root=uuid4(),uuid4();ref=ArtifactRef(artifact_id=root,sha256='a'*64)
    params=EncodeParams(image=ref,format='jpeg',icc_id='srgb_v1',icc_sha256='b'*64,quality=90,
        alpha='matte',matte_rgb=[1.,1.,1.],metadata_policy='strip',
        size=ExportSize(long_edge=1000 if mode=='resize' else 8000) if mode in ('resize','upscale_blocked') else None)
    source=m.RestoreLegacyPhysicalRoot(NS(asset_version_id=version,artifact_id=uuid4() if mode=='source' else root,
        object=NS(sha256=ref.sha256)),'c'*64,ArtifactRef(artifact_id=uuid4(),sha256='d'*64),None,4000,2000,'float32',(),None)
    width,height=(1000,500) if mode=='resize' else (4000,2000)
    if mode=='width':width-=1
    def invoke():return m.verify_export_geometry(params,source,uuid4() if mode=='version' else version,width,height,mode=='alpha')
    if mode in ('valid','resize','upscale_blocked'):assert invoke()==(width,height)
    else:
        with pytest.raises(ValueError):invoke()
