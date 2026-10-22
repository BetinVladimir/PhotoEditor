#!/usr/bin/env python3
"""Generate JSON Schema from executable Pydantic contracts and detect drift."""
from pathlib import Path
import argparse,json,sys,hashlib
import rfc8785
from endpoint_packaging import package_endpoints
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'workers'))
from photo_worker.contracts.conservative import DefectAutoRepairParams,DustAutoRepairParams,ConservativeRepairReport
from photo_worker.contracts.style import StyleV2
from photo_worker.contracts.assets import VerifyParams,InspectParams,DecodeParams,VerifiedAsset,AssetMetadata
from photo_worker.contracts.wire import TaskCommand,TaskResult
from photo_worker.models.lock import ModelLock
from photo_worker.contracts.pair import RegistrationParams,PairAlignment
from photo_worker.contracts.color_fit import ColorFitParams,ColorFit
from photo_worker.contracts.statistics import StatsParams
from photo_worker.contracts.resolved import ResolveParams,ResolvedStyle
from photo_worker.contracts.defects import DefectCandidates,DefectReview,DefectRepairParams,DefectRepairReport,DefectProposeParams,DefectProposeReport
from photo_worker.contracts.beauty import BeautyParams,BeautyReport
from photo_worker.contracts.mask_transform import MaskTransformParams,TransformedMask
from photo_worker.contracts.manual_mask import ManualMask,MaskRasterParams,RasterizedMask
from photo_worker.contracts.effective import EffectiveParams
from photo_worker.contracts.image_local import LocalParams,LocalReport
from photo_worker.contracts.image_color import ColorParams,ColorReport
from photo_worker.contracts.images import WorkingImage,MaskRef,RelativeDepth,Face68,Pose17,Stats20,TransformChain
from photo_worker.contracts.media import MediaParams,DerivativeManifest
from photo_worker.contracts.editor import EditorPreviewParams
from photo_worker.contracts.background import ComposeParams,ComposeReport,BlurParams,BlurReport,SkyParams,SkyReport
from photo_worker.contracts.hdr import WindowParams,WindowReport,HDRRegisterParams,BracketTransforms,HDRMergeParams,HDRReport
from photo_worker.contracts.dust import DustParams,DustCandidates,DustReview,DustRepairParams,DustRepairReport
from photo_worker.contracts.editor_effects import EditorEffectsSealParams,EditorExecutedBaseReceipt
from photo_worker.contracts.restoration import DenoiseParams,DenoiseReport,UpscaleParams,UpscaleReport,InpaintParams,InpaintReport,GenerationParams,GenerationReport,OutpaintParams
from photo_worker.contracts.style_aggregate import StyleAggregateParams,StyleAggregateReport
from photo_worker.contracts.person import PersonParams,PersonReport
from photo_worker.contracts.pose import PoseParams,PoseSet
from photo_worker.contracts.face import FaceDetectParams,FaceBoxes,LandmarkParams,FaceLandmarks
from photo_worker.contracts.body_fit import BodyFitParams,BodyFit
from photo_worker.contracts.body import BodyParams,BodyWarpReport
from photo_worker.contracts.style_validation import StyleValidateParams,StyleQualityReport,PackageParams,StylePackage
from photo_worker.contracts.derivative import ResizeParams,SharpenParams,WatermarkParams,DerivativeReport
from photo_worker.contracts.metadata import MetadataDocument,MetadataParams,MetadataReport
from photo_worker.contracts.report import ContactSheetParams,ContactSheet
from photo_worker.contracts.quality import QualityParams,QualityReport
from photo_worker.contracts.collection import CollectionFeatureParams,FeaturePackParams,SceneFeaturePack,SceneFeatures,GroupParams,SceneGroups,CullParams,CullRecommendations
from photo_worker.contracts.series import SeriesPackParams,SeriesStatsPack,SeriesMeasureParams,SeriesStats,SeriesParams,SeriesCorrections
from photo_worker.contracts.composition import StructureParams,ImageStructure,CompositionParams,CompositionPlan,CropParams,CropReport


from photo_worker.contracts.semantic_preview_projection import (
    PREVIEW_MODELS as SEMANTIC_PREVIEW_MODELS, standalone_schema, project_bundle,
)

def outputs():
    result={ROOT/'packages/contracts/generated/editor-preview-params.schema.json':json.dumps(EditorPreviewParams.model_json_schema(by_alias=True)|{'$schema':'https://json-schema.org/draft/2020-12/schema','$id':'https://photoeditor.local/contracts/editor-preview-params.schema.json'},ensure_ascii=False,indent=2,sort_keys=True)+'\n'}
    for name,model in [('editor-effects-seal-params',EditorEffectsSealParams),('editor-executed-base-receipt',EditorExecutedBaseReceipt),('defect-auto-repair-params',DefectAutoRepairParams),('dust-auto-repair-params',DustAutoRepairParams),('conservative-repair-report',ConservativeRepairReport),('structure-params',StructureParams),('image-structure',ImageStructure),('composition-params',CompositionParams),('composition-plan',CompositionPlan),('crop-params',CropParams),('crop-report',CropReport),('series-measure-params',SeriesMeasureParams),('series-pack-params',SeriesPackParams),('series-stats-pack',SeriesStatsPack),('series-stats',SeriesStats),('series-params',SeriesParams),('series-corrections',SeriesCorrections),('collection-feature-params',CollectionFeatureParams),('feature-pack-params',FeaturePackParams),('scene-feature-pack',SceneFeaturePack),('scene-features',SceneFeatures),('group-params',GroupParams),('scene-groups',SceneGroups),('cull-params',CullParams),('cull-recommendations',CullRecommendations),('quality-params',QualityParams),('quality-report',QualityReport),('contact-sheet-params',ContactSheetParams),('contact-sheet',ContactSheet),('metadata-document',MetadataDocument),('metadata-params',MetadataParams),('metadata-report',MetadataReport),('resize-params',ResizeParams),('sharpen-params',SharpenParams),('watermark-params',WatermarkParams),('derivative-report',DerivativeReport),('style-validate-params',StyleValidateParams),('style-quality-report',StyleQualityReport),('package-params',PackageParams),('style-package',StylePackage),('body-params',BodyParams),('body-warp-report',BodyWarpReport),('body-fit-params',BodyFitParams),('body-fit',BodyFit),('landmark-params',LandmarkParams),('face-landmarks',FaceLandmarks),('face-detect-params',FaceDetectParams),('face-boxes',FaceBoxes),('pose-params',PoseParams),('pose-set',PoseSet),('person-params',PersonParams),('person-report',PersonReport),('style-aggregate-params',StyleAggregateParams),('style-aggregate-report',StyleAggregateReport),('outpaint-params',OutpaintParams),('generation-params',GenerationParams),('generation-report',GenerationReport),('inpaint-params',InpaintParams),('inpaint-report',InpaintReport),('upscale-params',UpscaleParams),('upscale-report',UpscaleReport),('denoise-params',DenoiseParams),('denoise-report',DenoiseReport),('hdr-merge-params',HDRMergeParams),('hdr-report',HDRReport),('dust-params',DustParams),('dust-candidates',DustCandidates),('dust-review',DustReview),('dust-repair-params',DustRepairParams),('dust-repair-report',DustRepairReport),('hdr-register-params',HDRRegisterParams),('bracket-transforms',BracketTransforms),('window-params',WindowParams),('window-report',WindowReport),('sky-params',SkyParams),('sky-report',SkyReport),('blur-params',BlurParams),('blur-report',BlurReport),('compose-params',ComposeParams),('compose-report',ComposeReport),('media-params',MediaParams),('derivative-manifest',DerivativeManifest),('defect-propose-params',DefectProposeParams),('defect-propose-report',DefectProposeReport),('defect-repair-params',DefectRepairParams),('defect-repair-report',DefectRepairReport),('defect-candidates',DefectCandidates),('defect-review',DefectReview),('beauty-params',BeautyParams),('beauty-report',BeautyReport),('mask-transform-params',MaskTransformParams),('transformed-mask',TransformedMask),('mask-raster-params',MaskRasterParams),('rasterized-mask',RasterizedMask),('manual-mask',ManualMask),('effective-params',EffectiveParams),('local-params',LocalParams),('local-report',LocalReport),('color-params',ColorParams),('color-report',ColorReport),('resolve-params',ResolveParams),('resolved-style',ResolvedStyle),('stats-params',StatsParams),('color-fit-params',ColorFitParams),('color-fit',ColorFit),('registration-params',RegistrationParams),('pair-alignment',PairAlignment),('decode-params',DecodeParams),('verify-params',VerifyParams),('inspect-params',InspectParams),('verified-asset',VerifiedAsset),('asset-metadata',AssetMetadata),('photo-style',StyleV2),('task-command',TaskCommand),('task-result',TaskResult),('model-lock',ModelLock),('working-image',WorkingImage),('mask-ref',MaskRef),('relative-depth',RelativeDepth),('face68',Face68),('pose17',Pose17),('stats20',Stats20),('transform-chain',TransformChain)]:
        schema=model.model_json_schema(by_alias=True)
        schema['$schema']='https://json-schema.org/draft/2020-12/schema'
        schema['$id']=f'https://photoeditor.local/contracts/{name}.schema.json'
        result[ROOT/f'packages/contracts/generated/{name}.schema.json']=json.dumps(schema,ensure_ascii=False,indent=2,sort_keys=True)+'\n'
    for name, model in SEMANTIC_PREVIEW_MODELS:
        schema = standalone_schema(name, model)
        result[ROOT/f'packages/contracts/generated/{name}.schema.json'] = json.dumps(
            schema, ensure_ascii=False, indent=2, sort_keys=True) + '\n'
    from photo_worker.contracts.retouch_preview_catalog_projection import project_retouch_preview_catalog
    catalog_path=ROOT/'docs/implementation-v2/contracts/operations.json'
    result[catalog_path]=json.dumps(project_retouch_preview_catalog(json.loads(catalog_path.read_text())),ensure_ascii=False,indent=2)+'\n'
    bundle_path = ROOT/'docs/implementation-v2/contracts/schemas.json'
    result[bundle_path] = json.dumps(project_bundle(json.loads(bundle_path.read_text())),
                                    ensure_ascii=False, indent=2) + '\n'
    source=ROOT/'docs/implementation/contracts/operations.json'
    result[ROOT/'packages/contracts/operations.json']=source.read_text()
    # The baseline route registry is typed source documentation, while the V2
    # registry supplies executable schemas. A blind baseline copy destroys the
    # current packaged additive routes and changes their consumer format.
    baseline_path=ROOT/'docs/implementation/contracts/endpoints.json'
    executable_path=ROOT/'docs/implementation-v2/contracts/endpoints.json'
    result[ROOT/'packages/contracts/baseline-endpoints.json']=baseline_path.read_text()
    result[ROOT/'packages/contracts/endpoints.json']=json.dumps(package_endpoints(
        json.loads(baseline_path.read_text()),json.loads(executable_path.read_text()),
        json.loads(result[bundle_path])),ensure_ascii=False,indent=2)+'\n'
    result[ROOT/'packages/contracts/v2-endpoints.json']=(ROOT/'docs/implementation-v2/contracts/endpoints.json').read_text()
    result[ROOT/'backend/internal/workflow/operations.json']=(ROOT/'docs/implementation/contracts/operations.json').read_text()
    inputs=['{"z":-0.0,"a":[333333333.33333329,1E30,4.50,2e-3,1e-27]}',
            '{"😀":1,"\\ufffd":2,"a":"<>&\\u2028"}',
            '{"a":9007199254740991,"n":-9007199254740991,"b":[true,null,false]}']
    vectors=[]
    for raw in inputs:
        canonical=rfc8785.dumps(json.loads(raw))
        vectors.append({'input':raw,'canonical':canonical.decode(),'sha256':hashlib.sha256(canonical).hexdigest()})
    data=json.dumps(vectors,ensure_ascii=False,indent=2)+'\n'
    result[ROOT/'packages/contracts/generated/canonical-vectors.json']=data
    result[ROOT/'backend/internal/canonical/vectors.json']=data
    return result

def main():
    p=argparse.ArgumentParser();p.add_argument('--check',action='store_true');args=p.parse_args()
    for path,data in outputs().items():
        if args.check:
            if not path.exists() or path.read_text()!=data:raise SystemExit(f'Contract drift: {path.relative_to(ROOT)}')
        else:path.parent.mkdir(parents=True,exist_ok=True);path.write_text(data)
if __name__=='__main__':main()
