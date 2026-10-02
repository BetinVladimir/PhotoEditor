from contextlib import contextmanager
import hashlib
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
from photo_worker.algorithms.color import apply_global
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage
from photo_worker.contracts.pair import PairAlignment
from photo_worker.contracts.style import MetricBlock,Quality
from photo_worker.contracts.style_validation import ValidationPair,StyleValidateParams,StyleQualityReport,PackageParams
from photo_worker.operations.style.validate import execute as validate
from photo_worker.operations.style.package import execute as package
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for
from test_style_resolve import profile


def ref(data=b'x'):return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(data).hexdigest())


class ValidationArtifacts:
    def __init__(self,root):
        self.contracts={};self.paths={};self.staged=[];self.staged_refs={}
        style=profile();global_color=style.global_.model_copy(update={'gains_rgb':[1.35,.85,1.1]});style=style.model_copy(update={'global_':global_color});self.style_ref=self.add(style)
        before=np.random.default_rng(14).random((32,32,3),dtype=np.float32)*.65;after=apply_global(before,global_color)
        bp=self.array(root,'before',before);ap=self.array(root,'after',after);bc=Canvas(width=32,height=32,orientation=1,space_id='before',source_sha256=bp.sha256);ac=Canvas(width=32,height=32,orientation=1,space_id='after',source_sha256=ap.sha256);color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='a'*64,encoding='gamma24')
        bi=WorkingImage(pixels=bp,canvas=bc,color=color,dtype='float32',alpha=None,transform_chain=TransformChain.create([]));ai=WorkingImage(pixels=ap,canvas=ac,color=color,dtype='float32',alpha=None,transform_chain=TransformChain.create([]));br=self.add(bi);ar=self.add(ai)
        alignment=PairAlignment(before=br,after=ar,before_canvas=bc,after_canvas=ac,before_to_after=[1,0,0,0,1,0,0,0,1],overlap=1,median_reprojection=0,inlier_count=20,confidence='measured',crop_aware=False,warnings=[]);rr=self.add(alignment)
        case=ValidationPair(pair_id=uuid4(),session_id='heldout-session',before=br,after=ar,alignment=rr)
        self.value=StyleValidateParams(style=self.style_ref,holdout=[case],training_session_ids=['training-session'],samples_per_pair=1024)
    def array(self,root,name,value):
        path=root/(name+'.npy');np.save(path,value,allow_pickle=False);r=ref(path.read_bytes());self.paths[r.artifact_id]=path;return r
    def add(self,value):
        r=ref(canonical_bytes(value));self.contracts[r.artifact_id]=value;return r
    def params(self,model):return self.value
    def contract(self,r,model):return self.contracts[r.artifact_id]
    @contextmanager
    def input(self,r,*,suffix):yield SimpleNamespace(path=self.paths[r.artifact_id])
    def stage_contract(self,value,role):
        self.staged.append((role,value));result=self.add(value);self.staged_refs[role]=result;return result


def test_validation_measures_independent_pair_but_requires_acceptance_population(tmp_path):
    a=ValidationArtifacts(tmp_path);ctx=context_for(a.style_ref,'style.validate');ctx.lease.command.inputs=[a.style_ref,*[r for p in a.value.holdout for r in (p.before,p.after,p.alignment)]];ctx.lease.execution_profile.update(resource_estimate={'ram_bytes':1073741824},tile_size=256)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:a.scratch=scratch;result=validate(ctx,a)
    style=a.staged[0][1];report=a.staged[1][1]
    assert report.candidate.median_delta_e<report.baseline.median_delta_e*.1
    assert report.reasons==['insufficient_pairs','insufficient_sessions'] and not report.accepted_by_metrics
    assert report.style==a.staged_refs['validated_style']
    assert style.quality.status=='needs_review' and style.quality.validation==report.candidate and result.review_required


class PackageArtifacts:
    def __init__(self,root,style,report,style_ref=None,report_ref=None):
        self.style_ref=style_ref or ref(canonical_bytes(style));self.report_ref=report_ref or ref(canonical_bytes(report));self.value=PackageParams(style=self.style_ref,quality_report=self.report_ref);self.contracts={self.style_ref.artifact_id:style,self.report_ref.artifact_id:report};self.staged=[];self.data=None
    def params(self,model):return self.value
    def contract(self,r,model):return self.contracts[r.artifact_id]
    def stage(self,path,reservation,role,media_type):reservation.validate_file(path);self.data=path.read_bytes();return ref(self.data)
    def stage_contract(self,value,role):self.staged.append((role,value));return ref(canonical_bytes(value))


def test_package_is_exact_canonical_style_json(tmp_path):
    style=profile();quality=Quality(status='needs_review',reconstruction=None,validation=MetricBlock(median_delta_e=1,p95_delta_e=2,ssim=.9,sample_count=100),transfer_confidence='unvalidated',warnings=['insufficient_pairs']);style=style.model_copy(update={'quality':quality})
    provisional=ref();report=StyleQualityReport(style=provisional,config_hash=style.config_hash,threshold_profile_id='style_validation_v1',pair_count=1,session_count=1,baseline=MetricBlock(median_delta_e=2,p95_delta_e=3,ssim=.8,sample_count=100),candidate=quality.validation,median_improvement_fraction=.5,accepted_by_metrics=False,resulting_quality=quality,reasons=['insufficient_pairs'])
    a=PackageArtifacts(tmp_path,style,report.model_copy(update={'style':None}) if False else report)
    # Bind report to the exact style ref used by PackageParams.
    report=report.model_copy(update={'style':a.style_ref});a.contracts[a.report_ref.artifact_id]=report
    # Re-key the report ref because its immutable content changed.
    a.report_ref=ref(canonical_bytes(report));a.contracts[a.report_ref.artifact_id]=report;a.value=PackageParams(style=a.style_ref,quality_report=a.report_ref)
    ctx=context_for(a.style_ref,'style.package');ctx.lease.execution_profile['resource_estimate']={'ram_bytes':268435456};root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:a.scratch=scratch;result=package(ctx,a)
    assert a.data==canonical_bytes(style) and hashlib.sha256(a.data).hexdigest()==style.config_hash
    assert a.staged[0][1].config_hash==style.config_hash and result.review_required


def test_validation_outputs_feed_package_without_reference_rewrite(tmp_path):
    validation=ValidationArtifacts(tmp_path);vctx=context_for(validation.style_ref,'style.validate');vctx.lease.command.inputs=[validation.style_ref,*[r for p in validation.value.holdout for r in (p.before,p.after,p.alignment)]];vctx.lease.execution_profile.update(resource_estimate={'ram_bytes':1073741824},tile_size=256)
    validate_root=tmp_path/'validate-scratch';validate_root.mkdir()
    with ScratchSpace(validate_root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:validation.scratch=scratch;validate(vctx,validation)
    style=validation.staged[0][1];report=validation.staged[1][1]
    packaged=PackageArtifacts(tmp_path,style,report,validation.staged_refs['validated_style'],validation.staged_refs['quality_report']);pctx=context_for(packaged.style_ref,'style.package');pctx.lease.command.inputs=[packaged.style_ref,packaged.report_ref];pctx.lease.execution_profile['resource_estimate']={'ram_bytes':268435456};package_root=tmp_path/'package-scratch';package_root.mkdir()
    with ScratchSpace(package_root,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:packaged.scratch=scratch;result=package(pctx,packaged)
    assert packaged.data==canonical_bytes(style)
    assert packaged.staged[0][1].style.sha256==hashlib.sha256(packaged.data).hexdigest()
    assert result.review_required


def test_package_promotes_only_metric_accepted_style_to_validated(tmp_path):
    metric=MetricBlock(median_delta_e=.5,p95_delta_e=1.,ssim=.98,sample_count=12000);quality=Quality(status='measured',reconstruction=None,validation=metric,transfer_confidence='experimental',warnings=[]);style=profile().model_copy(update={'quality':quality});style_ref=ref(canonical_bytes(style))
    report=StyleQualityReport(style=style_ref,config_hash=style.config_hash,threshold_profile_id='style_validation_v1',pair_count=12,session_count=3,baseline=MetricBlock(median_delta_e=1.,p95_delta_e=1.4,ssim=.9,sample_count=12000),candidate=metric,median_improvement_fraction=.5,accepted_by_metrics=True,resulting_quality=quality,reasons=[]);report_ref=ref(canonical_bytes(report))
    a=PackageArtifacts(tmp_path,style,report,style_ref,report_ref);ctx=context_for(style_ref,'style.package');ctx.lease.command.inputs=[style_ref,report_ref];ctx.lease.execution_profile['resource_estimate']={'ram_bytes':268435456};root=tmp_path/'accepted-package';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:a.scratch=scratch;result=package(ctx,a)
    from photo_worker.contracts.style import StyleV2
    packaged=StyleV2.model_validate_json(a.data)
    assert packaged.quality.transfer_confidence=='validated' and packaged.quality.status=='measured'
    assert a.staged[0][1].config_hash==packaged.config_hash==hashlib.sha256(a.data).hexdigest()
    assert not result.review_required
