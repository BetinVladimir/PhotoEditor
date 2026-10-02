"""Apply body and face geometry once through a validated full-resolution inverse field."""
from contextlib import ExitStack
import numpy as np
from photo_worker.algorithms.body_warp import anchors,face_anchors,solve_inverse_field,validate_field,bilinear
from photo_worker.algorithms.color import to_encoded,to_linear
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.body import BodyParams,BodyWarpMetrics,BodyWarpReport
from photo_worker.contracts.body_fit import BodyFit
from photo_worker.contracts.face import FaceLandmarks
from photo_worker.contracts.images import Canvas,DenseWarpTransform,Rect,TransformChain,WorkingImage
from photo_worker.contracts.pose import PoseSet
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(BodyParams);image=artifacts.contract(params.image,WorkingImage);poses=artifacts.contract(params.poses,PoseSet)
        h,w=image.canvas.height,image.canvas.width
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or h*w>40000000:raise ValueError('Invalid body image')
        if poses.image!=params.image or any(p.canvas!=image.canvas or p.transform_chain!=image.transform_chain for p in poses.poses):raise ValueError('Pose geometry mismatch')
        if params.person.canvas!=image.canvas or params.person.transform_chain!=image.transform_chain:raise ValueError('Person mask geometry mismatch')
        if image.alpha is not None and image.alpha not in ctx.lease.command.inputs:raise ValueError('Undeclared alpha')
        face_set=None if params.faces is None else artifacts.contract(params.faces,FaceLandmarks)
        if face_set is not None and (face_set.image!=params.image or any(f.canvas!=image.canvas or f.transform_chain!=image.transform_chain for f in face_set.landmarks)):raise ValueError('Face geometry mismatch')
        if params.fit is not None:
            fitted=artifacts.contract(params.fit,BodyFit)
            if (fitted.params.before!=params.image or fitted.params.before_pose!=params.poses
                or fitted.params.before_person!=params.person or fitted.params.before_faces!=params.faces):raise ValueError('Body fit binding mismatch')
            requested={o.before_object_id:o.applied for o in fitted.objects if o.accepted}
        else:
            requested={pose.object_id:params.coefficients for pose in poses.poses}
        selected=[p for p in poses.poses if p.object_id in requested]
        if len(selected)!=len(requested):raise ValueError('Unknown body object')
        required=[params.image,params.poses,params.person.artifact]
        if params.fit is not None:required.append(params.fit)
        if params.faces is not None:required.append(params.faces)
        if any(ref not in ctx.lease.command.inputs for ref in required):raise ValueError('Undeclared body input')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,6);outputs=[];written=0
    with ExitStack() as inputs:
        def mapped(ref,shape,dtype='float32',unit=True):
            local=inputs.enter_context(artifacts.input(ref,suffix='.npy'));return inputs.enter_context(read_array(local.path,shape,dtype,ctx.check_active,unit_interval=unit))
        rgb=mapped(image.pixels,(h,w,3));person=mapped(params.person.artifact,(h,w));alpha=None if image.alpha is None else mapped(image.alpha,(h,w),'uint16',False)
        reason='zero_strength' if params.strength==0 else 'no_people' if not selected else 'unsafe';effective=0.;attempts=0;validation=None;field=None
        if reason=='unsafe':
            strength=params.strength
            for attempt in range(4):
                attempts=attempt+1
                try:
                    sets=[]
                    for pose in selected:
                        points,values,roi=anchors(pose,requested[pose.object_id],strength,w,h)
                        # Associate each face to its nearest visible nose and merge its anchors into that local RBF.
                        if face_set is not None and requested[pose.object_id].face_slim>0:
                            nose=np.array([pose.points[0].x*(w-1),pose.points[0].y*(h-1)])
                            candidates=sorted(face_set.landmarks,key=lambda f:np.linalg.norm(np.mean([[q.x*(w-1),q.y*(h-1)] for q in f.points],axis=0)-nose))
                            if candidates:
                                fp,fv=face_anchors(candidates[0],requested[pose.object_id].face_slim,strength,w,h);points=np.r_[points,fp];values=np.r_[values,fv]
                        sets.append((points,values,roi))
                    candidate=solve_inverse_field(w,h,sets,person);check=validate_field(candidate,params.max_displacement_image_fraction*max(h,w),person)
                except ValueError as exc:
                    if str(exc)=='AMBIGUOUS_OVERLAP':reason='ambiguous_overlap';break
                    raise OperationFailure('INPUT_INVALID') from exc
                validation=check
                if check.safe:field=candidate;effective=strength;reason='applied';break
                strength/=2
        if field is None:
            yy,xx=np.mgrid[0:h,0:w];field=np.stack((xx,yy),axis=-1).astype(np.float32)
            validation=validate_field(field,params.max_displacement_image_fraction*max(h,w),person)
        ctx.progress(1,6)
        reserve_bytes=h*w*(8+12+(2 if alpha is not None else 0))+65536
        with artifacts.scratch.reserve(reserve_bytes) as reservation:
            field_path=reservation.path('body-inverse-field','.npy');field_mm=np.lib.format.open_memmap(field_path,mode='w+',dtype='<f4',shape=(h,w,2));field_mm[:]=field;field_mm.flush();field_mm._mmap.close()
            rgb_path=reservation.path('body-rgb','.npy');rgb_mm=np.lib.format.open_memmap(rgb_path,mode='w+',dtype='<f4',shape=(h,w,3))
            if reason=='applied':
                warped=bilinear(to_linear(np.asarray(rgb)),field);rgb_mm[:]=to_encoded(np.asarray(warped,np.float32))
            else:rgb_mm[:]=rgb # strength=0 and every safety skip are byte-value identity.
            rgb_mm.flush();rgb_mm._mmap.close()
            alpha_path=None
            if alpha is not None:
                alpha_path=reservation.path('body-alpha','.npy');alpha_mm=np.lib.format.open_memmap(alpha_path,mode='w+',dtype='<u2',shape=(h,w))
                alpha_mm[:]=np.rint(np.clip(bilinear(np.asarray(alpha,np.float32),field),0,65535)).astype(np.uint16) if reason=='applied' else alpha
                alpha_mm.flush();alpha_mm._mmap.close()
            reservation.validate();field_ref=artifacts.stage(field_path,reservation,'inverse_field','application/octet-stream');pixel_ref=artifacts.stage(rgb_path,reservation,'working_rgb','application/octet-stream');outputs.extend([field_ref,pixel_ref]);written+=field_path.stat().st_size+rgb_path.stat().st_size
            alpha_ref=None
            if alpha_path is not None:alpha_ref=artifacts.stage(alpha_path,reservation,'working_alpha','application/octet-stream');outputs.append(alpha_ref);written+=alpha_path.stat().st_size
    ctx.progress(3,6)
    space='body:'+canonical_hash({'image':params.image.model_dump(mode='json'),'field':field_ref.model_dump(mode='json')})[:32]
    canvas=Canvas(width=w,height=h,orientation=1,space_id=space,source_sha256=image.canvas.source_sha256)
    step=DenseWarpTransform(kind='dense_warp',from_canvas=image.canvas,to_canvas=canvas,invertibility='invertible',valid_region=Rect(x=0,y=0,width=w,height=h),inverse_field=field_ref)
    chain=TransformChain.create([*image.transform_chain.steps,step]);output=WorkingImage(pixels=pixel_ref,canvas=canvas,color=image.color,dtype='float32',alpha=alpha_ref,transform_chain=chain)
    image_ref=artifacts.stage_contract(output,'working_image');outputs.append(image_ref);ctx.progress(4,6)
    warnings=[]
    if reason=='no_people':warnings.append('no_people')
    if reason in ('unsafe','ambiguous_overlap'):warnings.append('unsafe_identity')
    if reason=='ambiguous_overlap':warnings.append('overlapping_people')
    report=BodyWarpReport(image=params.image,poses=params.poses,person=params.person.artifact,faces=params.faces,requested_strength=params.strength,effective_strength=effective,attempts=attempts,applied=reason=='applied',reason=reason,metrics=BodyWarpMetrics(max_displacement_pixels=validation.max_displacement,min_jacobian=validation.min_jacobian,protected_center_displacement_pixels=validation.protected_center_displacement),object_ids=[p.object_id for p in selected],warnings=warnings)
    report_ref=artifacts.stage_contract(report,'body_report');outputs.append(report_ref);ctx.progress(6,6)
    return OperationResult(outputs=outputs,review_required=reason in ('unsafe','ambiguous_overlap'),bytes_written=written+len(canonical_bytes(output))+len(canonical_bytes(report)))


def body_operations(config:ArtifactConfig)->list[Operation]:
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'inverse_field','working_rgb','working_alpha','working_image','body_report'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('geometry.body','cpu',run)]
