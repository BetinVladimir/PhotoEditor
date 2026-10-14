"""Export dimensions derived from physically verified adopted WorkingImage."""
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.export import EncodeParams
from photo_worker.operations.export.encode import _dimensions
from .restore_legacy_physical import RestoreLegacyPhysicalRoot


def verify_export_geometry(params,source,asset_version_id,width,height,alpha):
    if (not isinstance(params,EncodeParams) or not isinstance(source,RestoreLegacyPhysicalRoot)
            or source.result.asset_version_id!=asset_version_id
            or params.image!=ArtifactRef(artifact_id=source.result.artifact_id,sha256=source.result.object.sha256)
            or source.dtype!='float32' or type(alpha) is not bool
            or any(type(v) is not int or v<1 for v in (source.width,source.height,width,height))
            or source.width*source.height>200000000 or width*height>200000000):
        raise ValueError('RESTORE_EXPORT_GEOMETRY_SOURCE')
    expected=_dimensions(source.width,source.height,params)
    expected_alpha=source.alpha is not None and params.alpha=='preserve'
    if (width,height)!=expected or alpha!=expected_alpha:
        raise ValueError('RESTORE_EXPORT_GEOMETRY_BINDING')
    return expected
