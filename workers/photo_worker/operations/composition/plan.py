import math
import cv2
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.composition import CompositionParams,CompositionPlan,ImageStructure
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def estimate_rotation(lines,max_degrees):
    deviations=[]
    for line in lines:
        dx=(line.x1-line.x0);dy=(line.y1-line.y0)
        if dx*dx+dy*dy<1e-8:continue
        angle=math.degrees(math.atan2(dy,dx));deviation=((angle+45)%90)-45
        deviations.extend([deviation]*max(1,int(round(line.confidence*10))))
    if not deviations:return 0.
    value=float(np.median(np.asarray(deviations)))
    return float(np.clip(-value,-max_degrees,max_degrees))


def _rect_score(rect,subjects,w,h):
    x,y,rw,rh=rect;kept=total=0.
    for s in subjects:
        sx0,sy0,sx1,sy1=s.x0*w,s.y0*h,s.x1*w,s.y1*h;area=(sx1-sx0)*(sy1-sy0);total+=area
        kept+=max(0,min(x+rw,sx1)-max(x,sx0))*max(0,min(y+rh,sy1)-max(y,sy0))
    retained=1. if total==0 else kept/total
    offset=math.hypot((x+rw/2)/w-.5,(y+rh/2)/h-.5)/math.sqrt(.5)
    return retained,float(min(1,offset))


def choose_crop(w,h,aspect,rotation,protected,subjects,headroom):
    center=(w/2,h/2);rot=cv2.getRotationMatrix2D(center,rotation,1.);matrix=np.vstack([rot,[0.,0.,1.]])
    inverse=np.linalg.inv(matrix);best=None
    # Exact deterministic search: largest dimensions first, then subject retention,
    # center distance and top-left coordinates. Candidate corners are checked in source space.
    max_w=min(w,int(h*aspect));max_h=int(max_w/aspect)
    if max_h>h:max_h=h;max_w=int(h*aspect)
    for scale in np.linspace(1,.2,81):
        rw=max(1,int(max_w*scale));rh=max(1,int(rw/aspect))
        if rh>h:continue
        xs=sorted(set([0,max(0,w-rw),max(0,(w-rw)//2),*[max(0,min(w-rw,int(p[0]-rw/2))) for p in protected]]))
        ys=sorted(set([0,max(0,h-rh),max(0,(h-rh)//2),*[max(0,min(h-rh,int(p[1]-headroom*rh))) for p in protected]]))
        for y in ys:
            for x in xs:
                corners=np.array([[x,y,1],[x+rw-1,y,1],[x+rw-1,y+rh-1,1],[x,y+rh-1,1]],np.float64).T
                src=inverse@corners;src=src[:2]/src[2:]
                if np.any(src[0]<0) or np.any(src[0]>w-1) or np.any(src[1]<0) or np.any(src[1]>h-1):continue
                transformed=[matrix@np.array([px,py,1.]) for px,py in protected]
                if any(not(x<=q[0]<=x+rw-1 and y<=q[1]<=y+rh-1) for q in transformed):continue
                retained,offset=_rect_score((x,y,rw,rh),subjects,w,h);key=(-rw*rh,-retained,offset,y,x)
                if best is None or key<best[0]:best=(key,(x,y,rw,rh),src.T.reshape(-1).tolist(),retained,offset,matrix)
        if best is not None:break
    return best


def choose_outpaint(w,h,aspect,subjects,protected):
    current=w/h
    if aspect>current:
        pixels=max(8,math.ceil(h*aspect)-w)
        center=np.mean([(s.x0+s.x1)/2 for s in subjects]) if subjects else (np.mean([p[0]/max(1,w-1) for p in protected]) if protected else .5)
        side='right' if center<=.5 else 'left'
    else:
        pixels=max(8,math.ceil(w/aspect)-h)
        center=np.mean([(s.y0+s.y1)/2 for s in subjects]) if subjects else (np.mean([p[1]/max(1,h-1) for p in protected]) if protected else .5)
        side='bottom' if center<=.5 else 'top'
    if pixels>8192 or (w+pixels if side in ('left','right') else w)*(h+pixels if side in ('top','bottom') else h)>200000000:return None,None
    return side,pixels


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(CompositionParams);structure=artifacts.contract(p.structure,ImageStructure);image=artifacts.contract(structure.image,WorkingImage)
        if image.canvas.width*image.canvas.height>200000000:raise ValueError('Pixel limit')
        rotation=estimate_rotation(structure.lines,p.max_rotation_degrees) if p.allow_rotation else 0.
        protected=[(v.x*(image.canvas.width-1),v.y*(image.canvas.height-1)) for v in structure.protected_points]
        chosen=choose_crop(image.canvas.width,image.canvas.height,p.aspect_width/p.aspect_height,rotation,protected,structure.subjects,p.headroom_target)
    except (ValueError,np.linalg.LinAlgError) as exc:raise OperationFailure('INPUT_INVALID') from exc
    reasons=[]
    if not structure.lines:reasons.append('no_structure')
    if abs(rotation)>=3:reasons.append('large_rotation')
    if chosen is None:
        reasons.append('no_valid_crop');side,pixels=choose_outpaint(image.canvas.width,image.canvas.height,p.aspect_width/p.aspect_height,structure.subjects,protected)
        if side is None:raise OperationFailure('INPUT_INVALID')
        ow=image.canvas.width+(pixels if side in ('left','right') else 0);oh=image.canvas.height+(pixels if side in ('top','bottom') else 0);matrix=np.eye(3);quad=[0.,0.,image.canvas.width-1.,0.,image.canvas.width-1.,image.canvas.height-1.,0.,image.canvas.height-1.];retained=1.;offset=0.;preserved=True;disposition='outpaint'
    else:
        side=pixels=None;_,(x,y,ow,oh),quad,retained,offset,rot=chosen;translate=np.array([[1.,0.,-x],[0.,1.,-y],[0.,0.,1.]]);matrix=translate@rot;preserved=True
        if retained<.9:reasons.append('subject_loss')
        disposition='review' if reasons and reasons!=['no_structure'] or retained<.9 else 'apply'
    plan=CompositionPlan(image=structure.image,structure=p.structure,source_width=image.canvas.width,source_height=image.canvas.height,output_width=ow,output_height=oh,forward_matrix=matrix.reshape(-1).tolist(),source_quad=quad,rotation_degrees=rotation,perspective_area_change=0,retained_subject_fraction=retained,center_offset=offset,protected_points_preserved=preserved,disposition=disposition,reasons=reasons,outpaint_side=side,outpaint_pixels=pixels)
    ref=artifacts.stage_contract(plan,'composition_plan');return OperationResult(outputs=[ref],review_required=disposition!='apply',bytes_written=len(canonical_bytes(plan)))


def composition_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'composition_plan'})) as a:return execute(ctx,a)
    return [Operation('composition.plan','cpu',run)]
