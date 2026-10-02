"""Full-resolution tiled float32 effects. Proxies carry masks/fields, never output pixels."""
import cv2
import numpy as np
from .analysis import luma, linear, encoded, polygon, blur


def sample_map(array, x0, y0, x1, y1, width, height):
    """Global pixel-center mapping, invariant to tile size."""
    ph,pw = array.shape[:2]
    xs = (np.arange(x0,x1,dtype=np.float32)+.5)*pw/width-.5
    ys = (np.arange(y0,y1,dtype=np.float32)+.5)*ph/height-.5
    xx,yy = np.meshgrid(xs,ys)
    return cv2.remap(array,xx,yy,cv2.INTER_LINEAR,borderMode=cv2.BORDER_REPLICATE)


def prepare_maps(maps, stats, cfg):
    result = {k:v.copy() for k,v in maps.items()}
    y = np.maximum(maps['luma'],1e-5)
    h,w = y.shape
    field = np.zeros((h,w),np.float32)
    weights = np.zeros_like(field)
    face_target = np.zeros_like(field)
    face_target_weight = np.zeros_like(field)
    recovery_mask = np.zeros_like(field)
    for face in stats['faces']:
        pts = np.array(face['points'],np.float32)*[w,h]
        x0,y0,x1,y1=face['bbox']
        fw=(x1-x0)*w
        # Per-face luminance reference retains each person's underlying tone.
        ellipse=np.zeros_like(field)
        center=(round((x0+x1)*w/2),round((y0+y1)*h/2))
        cv2.ellipse(ellipse,center,(max(1,round(fw*.62)),max(1,round((y1-y0)*h*.65))),0,0,360,1,-1)
        ellipse=blur(ellipse,fw*.12)
        if cfg.detail.blur_detection.enabled and cfg.detail.face_recovery.enabled:
            a=max(0,round((x0-(x1-x0)*.18)*w)); c=min(w,round((x1+(x1-x0)*.18)*w))
            b=max(0,round((y0-(x1-x0)*.18)*h)); d=min(h,round((y1+(x1-x0)*.18)*h))
            crop=maps['luma'][b:d,a:c]
            if crop.size:
                normalized=cv2.resize(crop,(512,512),interpolation=cv2.INTER_AREA)
                score=float(cv2.Laplacian(normalized,cv2.CV_32F,ksize=3).var())
                if score < cfg.detail.blur_detection.threshold:
                    # Tighter than the relighting ellipse so sharpening cannot
                    # create a halo along hair, sky or the face boundary.
                    core=np.zeros_like(field)
                    cv2.ellipse(core,center,(max(1,round(fw*.48)),max(1,round((y1-y0)*h*.56))),0,0,360,1,-1)
                    recovery_mask=np.maximum(recovery_mask,blur(core,max(2,fw*.045)))
        # Large-scale Retinex field: suppress broad sun/shadow patches while
        # retaining pores and facial features in the original pixel layer.
        low_near=blur(np.log2(y),max(2,fw*.045))
        low_far=blur(np.log2(y),max(3,fw*.16))
        low=low_near*.72+low_far*.28
        delta=np.log2(max(face['median_linear_luma'],.01))-low
        local=np.where(delta>=0,delta*1.12,delta*.78)
        local=np.clip(local,-cfg.beauty.even_light.max_local_ev,
                      cfg.beauty.even_light.max_local_ev)
        # A small common key-light gradient makes unrelated scenes read as one
        # series without painting synthetic catchlights or replacing the face.
        yy,xx=np.mgrid[:h,:w].astype(np.float32)
        cx=(x0+x1)*w/2
        key=np.clip((cx-xx)/max(fw,1),-.5,.5)*.18
        local+=key
        field+=local*ellipse
        weights+=ellipse
        target=(cfg.collection.target_face_luma if cfg.collection.mode=='target'
                else float(encoded(np.array(face['median_linear_luma'],np.float32))))
        face_target+=target*ellipse
        face_target_weight+=ellipse
    if cfg.beauty.enabled and cfg.beauty.even_light.enabled:
        field=field/np.maximum(1,weights)*cfg.beauty.even_light.strength
    else:
        field[:]=0
    # Contour lighting does not move pixels.
    if cfg.beauty.enabled and cfg.beauty.contour_light.enabled:
        yy,xx=np.mgrid[:h,:w].astype(np.float32)
        for face in stats['faces']:
            x0,y0,x1,y1=face['bbox']; fw=(x1-x0)*w; fh=(y1-y0)*h
            cx=(x0+x1)*w/2; cy=(y0+y1)*h/2
            side=np.exp(-((np.abs(xx-cx)/max(fw,1)-.4)/.12)**2-((yy-cy)/max(fh*.6,1))**4)
            field-=side*maps['face_skin']*cfg.beauty.contour_light.strength*.25
    result['ev_field']=field
    result['face_target']=face_target/np.maximum(face_target_weight,1e-6)
    widths=[(f['bbox'][2]-f['bbox'][0])*w for f in stats['faces']]
    feather=max(2.,(float(np.median(widths)) if widths else w*.1)*.035)
    result['relight_mask']=np.clip(blur(maps['face_oval'],feather),0,1)*maps['person']
    result['recovery_mask']=np.clip(recovery_mask,0,1)*maps['person']
    return result


def process_tile(rgb, maps, cfg, exposure, sigma):
    out=linear(rgb)
    if cfg.color.enabled:
        out*=np.array(cfg.color.wb_gains,np.float32)
        out*=np.float32(2**(exposure+cfg.color.exposure_ev))
    out*=np.exp2(maps['ev_field'][...,None])
    # Continuous shoulder in linear light. Identity below 0.70.
    if cfg.color.enabled:
        peak=np.max(out,axis=2)
        shoulder=.70+.24*(1-np.exp(-np.maximum(peak-.70,0)/.24))
        scale=np.where(peak>.70,shoulder/np.maximum(peak,1e-6),1)
        out*=scale[...,None]
    out=encoded(np.maximum(out,0))
    face=maps['face_skin']; body=maps['body_skin']
    skin=np.maximum(face,body)
    recovery=maps.get('recovery_mask')
    if cfg.detail.face_recovery.enabled and recovery is not None and np.any(recovery>.01):
        # Conditional pre-beauty recovery for genuinely soft faces. Combine a
        # fine and a mid-scale residual; threshold both to avoid lifting JPEG
        # noise. Highlight guard prevents ringing on clipped backlight edges.
        fine=out-cv2.GaussianBlur(out,(0,0),max(.55,sigma*.11))
        mid=out-cv2.GaussianBlur(out,(0,0),max(1.0,sigma*.32))
        detail=fine*1.10+mid*.28
        detail=np.sign(detail)*np.maximum(np.abs(detail)-.0025,0)
        detail=np.clip(detail,-cfg.detail.face_recovery.max_detail_gain,
                       cfg.detail.face_recovery.max_detail_gain)
        guard=(1-np.clip((luma(out)-.72)/.22,0,1)*.8)[...,None]
        amount=recovery[...,None]*cfg.detail.face_recovery.strength*guard
        out=np.clip(out+detail*amount,0,1)
    if cfg.beauty.enabled:
        if cfg.beauty.even_light.enabled and cfg.beauty.even_light.strength:
            yy=luma(out)
            near=cv2.GaussianBlur(yy,(0,0),max(2.,sigma*4))
            far=cv2.GaussianBlur(yy,(0,0),max(3.,sigma*12))
            ratio=np.clip((far+.025)/(near+.025),.68,1.48)
            # Protect the central features while still filling their broad cast
            # shadow; masks have soft boundaries to avoid portrait halos.
            oval=maps['relight_mask']*(1-maps['protected']*.45)
            mix=oval*cfg.beauty.even_light.strength*.62
            out*=np.exp2(np.log2(ratio)[...,None]*mix[...,None])
            # Compress only the broad facial luminance range around the common
            # series target. High-frequency texture stays in the RGB layer.
            target=maps['face_target']
            # Keep the person's original midtone and use the series target only
            # as a weak anchor. A stronger pull creates pale, flat faces when a
            # source already contains clipped sun highlights.
            mapped=target+(yy-target)*.78
            tone_ratio=np.clip((mapped+.025)/(yy+.025),.88,1.16)
            highlight_guard=1-np.clip((yy-.58)/.30,0,1)*.85
            tone_mix=oval*cfg.beauty.even_light.strength*.20*highlight_guard
            out*=np.exp2(np.log2(tone_ratio)[...,None]*tone_mix[...,None])
        # Specular reduction affects highlights on skin only, retaining shape.
        if cfg.beauty.specular.enabled and cfg.beauty.specular.strength:
            yy=luma(out)
            local=cv2.GaussianBlur(yy,(0,0),sigma*3)
            spec=np.clip((yy-local-.015)/.16,0,1)*np.clip((yy-.52)/.3,0,1)*skin
            out*=1-(spec*cfg.beauty.specular.strength*.20)[...,None]
        if cfg.beauty.blemishes.enabled and cfg.beauty.blemishes.auto_redness:
            chroma=out[...,0]-(out[...,1]+out[...,2])*.5
            chroma_base=cv2.GaussianBlur(chroma,(0,0),max(2.,sigma*1.4))
            # Slightly more sensitive to small inflamed/red defects while the
            # local chroma baseline keeps stable freckles and moles intact.
            spot=np.clip((chroma-chroma_base-.010)/.065,0,1)*face
            amount=spot*cfg.beauty.blemishes.auto_strength
            out[...,0]-=(chroma-chroma_base)*amount*.78
            out[...,1]+=(chroma-chroma_base)*amount*.20
        if cfg.beauty.redness.enabled and cfg.beauty.redness.strength:
            excess=np.maximum(out[...,0]-(out[...,1]*1.22+.025),0)
            delta=excess*skin*cfg.beauty.redness.strength*.65
            out[...,0]-=delta
            out[...,1]+=delta*.35
        if cfg.beauty.skin.enabled:
            strength=(face*cfg.beauty.skin.face_strength+body*cfg.beauty.skin.body_strength)
            strength=np.clip(strength,0,1)
            guide=luma(out).astype(np.float32)
            radius=max(2,round(sigma*3))
            base=cv2.ximgproc.guidedFilter(guide,out,radius,.002)
            fine=out-cv2.GaussianBlur(out,(0,0),max(.6,sigma*.22))
            target=base+fine*cfg.beauty.skin.texture_retention
            out=out*(1-strength[...,None])+target*strength[...,None]
    if cfg.color.enabled:
        yy=luma(out)
        out=yy[...,None]+(out-yy[...,None])*(1+cfg.color.saturation)
        # Endpoint-preserving S curve; gamma=1 and contrast=0 are exact identity.
        out=np.clip(out,0,1)
        out+=cfg.color.contrast*(out-.5)*4*out*(1-out)
        out=np.clip(out,0,1)**(1/cfg.color.gamma)
    if cfg.detail.sharpening.enabled and cfg.detail.sharpening.strength:
        small=cv2.GaussianBlur(out,(0,0),max(.6,sigma*.16))
        detail=out-small
        # Threshold smooth noise and limit overshoot. Preserve enough facial
        # micro-contrast to avoid the soft, waxy result of the beauty pass.
        detail=np.sign(detail)*np.maximum(np.abs(detail)-.003,0)
        skin_factor=1-.38*skin[...,None]
        highlight_guard=1-np.clip((luma(out)-.78)/.18,0,1)[...,None]*.65
        out+=np.clip(detail,-.030,.030)*cfg.detail.sharpening.strength*skin_factor*highlight_guard
    return np.clip(out,0,1)


def geometry_field(stats,maps,cfg):
    h,w=maps['luma'].shape
    dx=np.zeros((h,w),np.float32)
    notes=[]
    if not cfg.geometry.enabled:
        return dx,notes
    yy,xx=np.mgrid[:h,:w].astype(np.float32)
    protection=maps['protected']
    # Zero deformation over the central face, radial falloff outside cheeks.
    for f in stats['faces']:
        x0,y0,x1,y1=f['bbox']; fw=(x1-x0)*w; fh=(y1-y0)*h
        if fw<50:
            notes.append(f"{f['id']}: geometry skipped (face too small)")
            continue
        cx=(x0+x1)*w/2; cy=(y0+y1)*h/2
        u=(xx-cx)/max(fw,1); v=(yy-cy)/max(fh,1)
        side=np.exp(-((np.abs(u)-.43)/.13)**2-(v/.48)**4)
        central=np.clip((np.abs(u)-.25)/.1,0,1)
        dx+=np.sign(u)*side*central*(1-protection)*fw*cfg.geometry.max_face_displacement*cfg.geometry.face_strength
    # Keep faces exactly fixed against BODY warp, fade smoothly outside their ovals.
    distance=cv2.distanceTransform((maps['face_oval']<.1).astype(np.uint8),cv2.DIST_L2,5)
    body_protect=np.clip(distance/max(3,w*.02),0,1)
    body_protect=body_protect*body_protect*(3-2*body_protect)
    for x0,y0,x1,y1 in cfg.geometry.body_regions:
        bw=(x1-x0)*w; bh=(y1-y0)*h; cx=(x0+x1)*w/2; cy=(y0+y1)*h/2
        u=(xx-cx)/bw; v=(yy-cy)/bh
        window=np.exp(-((np.abs(u)-.42)/.16)**2-(v/.38)**4)
        dx+=np.sign(u)*window*bw*cfg.geometry.max_body_displacement*cfg.geometry.body_strength*body_protect
    # Sampling field: source_x = output_x + dx. Check no foldover before use.
    gradient=np.gradient(dx,axis=1)
    magnitude=float(np.max(np.abs(gradient)))
    if magnitude>.25:
        multiplier=.25/magnitude
        dx*=multiplier
        notes.append(f'geometry strength reduced to {multiplier:.3f} of request to protect local scale')
    if np.max(np.abs(dx))>.001:
        notes.append('geometry applied: background/identity requires visual review')
    return dx,notes


def cleanup_face_highlights(rgb, maps, stats, cfg):
    """Reduce residual glare on every detected face after all normal effects.

    The operation is local to full-resolution face crops. It only compresses
    pixels that are both bright for the face and brighter than their smooth
    neighbourhood; protected features and non-skin pixels remain untouched.
    """
    settings=cfg.beauty.highlight_cleanup
    if not (cfg.beauty.enabled and settings.enabled and settings.strength):
        return rgb,0
    h,w=rgb.shape[:2]; scale=65535. if rgb.dtype==np.uint16 else 255.
    changed=0
    for face in stats.get('faces',[]):
        x0,y0,x1,y1=face['bbox']; fw=(x1-x0)*w; fh=(y1-y0)*h
        pad=.12
        a=max(0,round((x0-pad*(x1-x0))*w)); c=min(w,round((x1+pad*(x1-x0))*w))
        b=max(0,round((y0-pad*(y1-y0))*h)); d=min(h,round((y1+pad*(y1-y0))*h))
        if c-a<24 or d-b<24: continue
        skin=sample_map(maps['face_skin'],a,b,c,d,w,h)
        protect=sample_map(maps['protected'],a,b,c,d,w,h)
        valid=(skin>.18)&(protect<.55)
        if np.count_nonzero(valid)<64: continue
        crop=rgb[b:d,a:c].astype(np.float32)/scale
        yy=luma(crop)
        sigma=max(2.,min(fw,fh)*.045)
        local=cv2.GaussianBlur(yy,(0,0),sigma)
        face_reference=float(np.percentile(yy[valid],62))
        global_excess=np.maximum(yy-max(settings.threshold,face_reference+.045),0)
        local_excess=np.maximum(yy-local-settings.local_contrast,0)
        # The global term catches broad sun patches; the local term catches
        # compact oily glare. Smooth activation avoids visible patch borders.
        excess=np.maximum(local_excess,global_excess*.55)
        amount=np.clip(excess/max(.04,settings.max_reduction),0,1)
        amount=cv2.GaussianBlur(amount,(0,0),max(1.,sigma*.22))
        amount*=skin*(1-protect)*settings.strength
        reduction=np.minimum(excess*settings.strength,settings.max_reduction)
        target=np.maximum(yy-reduction,.02)
        ratio=np.clip(target/np.maximum(yy,1e-4),.72,1.)
        corrected=crop*ratio[...,None]
        alpha=np.clip(amount*1.35,0,1)[...,None]
        crop=crop*(1-alpha)+corrected*alpha
        rgb[b:d,a:c]=np.round(np.clip(crop,0,1)*scale).astype(rgb.dtype)
        changed+=int(np.count_nonzero(amount>.02))
    return rgb,changed


def finish_face_color(rgb, maps, stats, cfg):
    """Restore natural skin chroma after relighting, restoration and glare cleanup."""
    settings=cfg.beauty.face_color_finish
    if not (cfg.beauty.enabled and settings.enabled and settings.strength):
        return rgb,0
    h,w=rgb.shape[:2]; scale=65535. if rgb.dtype==np.uint16 else 255.
    changed=0
    for face in stats.get('faces',[]):
        x0,y0,x1,y1=face['bbox']; fw=(x1-x0)*w; fh=(y1-y0)*h; pad=.10
        a=max(0,round((x0-pad*(x1-x0))*w)); c=min(w,round((x1+pad*(x1-x0))*w))
        b=max(0,round((y0-pad*(y1-y0))*h)); d=min(h,round((y1+pad*(y1-y0))*h))
        if c-a<24 or d-b<24: continue
        skin=sample_map(maps['face_skin'],a,b,c,d,w,h)
        protect=sample_map(maps['protected'],a,b,c,d,w,h)
        crop=rgb[b:d,a:c].astype(np.float32)/scale; yy=luma(crop)
        valid=(skin>.22)&(protect<.45)
        reference=valid&(yy>.28)&(yy<.72)
        if np.count_nonzero(reference)<64: reference=valid
        if np.count_nonzero(reference)<64: continue
        cr=crop[...,0]-yy; cb=crop[...,2]-yy
        ref_cr=float(np.median(cr[reference])); ref_cb=float(np.median(cb[reference]))
        chroma=np.sqrt(cr*cr+cb*cb); ref_chroma=float(np.median(chroma[reference]))
        gray=np.clip((ref_chroma*1.10-chroma)/max(ref_chroma,.025),0,1)
        # Favor pale/highlighted skin where gray cast is most visible. The
        # shift is measured from the person's own midtone skin, not a fixed race tone.
        need=np.clip(gray*.72+np.clip((yy-.48)/.34,0,1)*.28,0,1)
        alpha=skin*(1-protect)*settings.strength*need
        target_cr=ref_cr*(1+settings.saturation)+settings.warmth
        target_cb=ref_cb*(1+settings.saturation)-settings.warmth*.45
        dcr=np.clip(target_cr-cr,-settings.max_chroma_shift,settings.max_chroma_shift)
        dcb=np.clip(target_cb-cb,-settings.max_chroma_shift,settings.max_chroma_shift)
        new_cr=cr+dcr; new_cb=cb+dcb
        red=yy+new_cr; blue=yy+new_cb
        green=(yy-.2126*red-.0722*blue)/.7152
        corrected=np.stack([red,green,blue],axis=2)
        edge=cv2.GaussianBlur(valid.astype(np.float32),(0,0),max(1.,min(fw,fh)*.008))
        mix=np.clip(alpha*edge,0,1)[...,None]
        crop=crop*(1-mix)+corrected*mix
        rgb[b:d,a:c]=np.round(np.clip(crop,0,1)*scale).astype(rgb.dtype)
        changed+=int(np.count_nonzero(alpha>.015))
    return rgb,changed


def post_white_balance(rgb, cfg):
    """Conservative gray-world WB plus a smooth highlight shoulder."""
    settings=cfg.beauty.post_white_balance
    if not (cfg.beauty.enabled and settings.enabled and settings.strength):
        return rgb,{'gains':[1.,1.,1.],'clipped_before':0.,'clipped_after':0.}
    scale=65535. if rgb.dtype==np.uint16 else 255.
    encoded_rgb=rgb.astype(np.float32)/scale
    # Estimate on a proxy to keep memory bounded and ignore colorful/clipped pixels.
    ph=max(1,min(encoded_rgb.shape[0],900)); pw=max(1,round(encoded_rgb.shape[1]*ph/encoded_rgb.shape[0]))
    proxy=cv2.resize(encoded_rgb,(pw,ph),interpolation=cv2.INTER_AREA)
    py=luma(proxy); chroma=np.max(proxy,axis=2)-np.min(proxy,axis=2)
    neutral=(py>.10)&(py<.78)&(chroma<settings.neutral_saturation)
    if np.count_nonzero(neutral)>=256:
        med=np.array([np.median(proxy[...,i][neutral]) for i in range(3)],np.float32)
        target=float(np.exp(np.mean(np.log(np.maximum(med,1e-4)))))
        raw=np.clip(target/np.maximum(med,1e-4),1-settings.max_gain,1+settings.max_gain)
        gains=1+(raw-1)*settings.strength
        # White balance is applied in linear light.
        balanced=encoded(np.maximum(linear(encoded_rgb)*gains,0))
    else:
        gains=np.ones(3,np.float32);balanced=encoded_rgb.copy()
    peak=np.max(balanced,axis=2)
    start=settings.highlight_start; room=max(settings.highlight_headroom-start,.02)
    shoulder=start+room*(1-np.exp(-np.maximum(peak-start,0)/room))
    shoulder=np.minimum(shoulder,settings.highlight_headroom)
    amount=settings.strength*np.clip((peak-start)/max(1-start,.01),0,1)
    target_peak=peak*(1-amount)+shoulder*amount
    balanced*=np.where(peak>start,target_peak/np.maximum(peak,1e-6),1)[...,None]
    clipped_before=float(np.mean(np.max(encoded_rgb,axis=2)>=.995))
    clipped_after=float(np.mean(np.max(balanced,axis=2)>=.995))
    result=np.round(np.clip(balanced,0,1)*scale).astype(rgb.dtype)
    return result,{'gains':[float(x) for x in gains],
                   'clipped_before':clipped_before,'clipped_after':clipped_after}


def render(rgb,maps,stats,cfg,exposure,progress=None):
    height,width=rgb.shape[:2]
    prepared=prepare_maps(maps,stats,cfg)
    out=np.empty((height,width,3),np.uint16)
    face_widths=[(f['bbox'][2]-f['bbox'][0])*width for f in stats['faces']]
    sigma=max(1.5,min(14.,(float(np.median(face_widths)) if face_widths else width*.12)*.008))
    # Guided filter radius + two box filters, specular blur and sharpening supports.
    halo=int(np.ceil(max(26*sigma,6*round(sigma*3))+8))
    tile=cfg.runtime.tile_size
    count=((height+tile-1)//tile)*((width+tile-1)//tile)
    step=0
    scale=65535 if rgb.dtype==np.uint16 else 255
    for y in range(0,height,tile):
        for x in range(0,width,tile):
            x1=min(width,x+tile); y1=min(height,y+tile)
            a=max(0,x-halo); b=max(0,y-halo); c=min(width,x1+halo); d=min(height,y1+halo)
            fragment=rgb[b:d,a:c].astype(np.float32)/scale
            local={k:sample_map(prepared[k],a,b,c,d,width,height)
                   for k in ('face_skin','body_skin','face_oval','protected','relight_mask','recovery_mask','face_target','ev_field')}
            processed=process_tile(fragment,local,cfg,exposure,sigma)
            out[y:y1,x:x1]=np.round(processed[y-b:y1-b,x-a:x1-a]*65535).astype(np.uint16)
            step+=1
            if progress: progress(step,count)
    # Explicit, bounded blemish regions. Work locally in uint8 only for this opt-in stage.
    if cfg.beauty.enabled and cfg.beauty.blemishes.enabled:
        for cx,cy,r in cfg.beauty.blemishes.circles:
            px=round(cx*width); py=round(cy*height); radius=max(1,round(r*width)); pad=radius*4+8
            a=max(0,px-pad); b=max(0,py-pad); c=min(width,px+pad+1); d=min(height,py+pad+1)
            skin=sample_map(np.maximum(maps['face_skin'],maps['body_skin']),a,b,c,d,width,height)
            protect=sample_map(maps['protected'],a,b,c,d,width,height)
            mask=np.zeros((d-b,c-a),np.uint8)
            cv2.circle(mask,(px-a,py-b),radius,255,-1)
            mask[(skin<.5)|(protect>.1)]=0
            if not np.any(mask): continue
            crop=out[b:d,a:c]
            healed=cv2.inpaint((crop/257).astype(np.uint8),mask,3,cv2.INPAINT_TELEA).astype(np.float32)*257
            alpha=blur(mask.astype(np.float32)/255,max(1,radius*.15))* (1-protect)
            crop[:]=np.round(crop*(1-alpha[...,None])+healed*alpha[...,None]).astype(np.uint16)
    dx,notes=geometry_field(stats,maps,cfg)
    if np.max(np.abs(dx))>.001:
        warped=np.empty_like(out)
        for y in range(0,height,tile):
            for x in range(0,width,tile):
                x1=min(width,x+tile); y1=min(height,y+tile)
                displacement=sample_map(dx,x,y,x1,y1,width,height)*width/dx.shape[1]
                xx,yy=np.meshgrid(np.arange(x,x1,dtype=np.float32),np.arange(y,y1,dtype=np.float32))
                warped[y:y1,x:x1]=cv2.remap(out,xx+displacement,yy,cv2.INTER_LINEAR,borderMode=cv2.BORDER_REFLECT_101)
        out=warped
    return out,notes
