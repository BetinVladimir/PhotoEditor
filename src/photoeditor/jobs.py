import json
import os
import shutil
import subprocess
import time
import uuid
from pathlib import Path
from contextlib import contextmanager

import cv2
import numpy as np
from PIL import Image, ImageDraw

from . import __version__
from .analysis import Analyzer, linear, encoded, luma
from .config import for_image
from .io import atomic_json, digest, hash_object, read_rgb, proxy_rgb, write_jpeg, write_tiff
from .models import ensure_models
from .render import render, cleanup_face_highlights, finish_face_color, post_white_balance


def emit(event, **data):
    print(json.dumps({'event':event,**data}, ensure_ascii=False),flush=True)


@contextmanager
def job_lock(root):
    root.mkdir(parents=True,exist_ok=True)
    # OS lock is released automatically on kill/crash; no stale lock recovery heuristic.
    import fcntl
    with open(root/'.lock','a') as f:
        try:
            fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('Another PhotoEditor job uses this output directory')
        try:
            yield
        finally:
            fcntl.flock(f,fcntl.LOCK_UN)


def scan(source, cfg):
    files=source.rglob('*') if cfg.input.recursive else source.glob('*')
    result=[]
    for path in sorted(files):
        if path.is_symlink() or any(p.is_symlink() for p in path.parents if p!=source): continue
        if not path.is_file() or path.suffix.lower().lstrip('.') not in cfg.input.formats: continue
        rel=path.relative_to(source).as_posix()
        sha=digest(path)
        result.append({'relative':rel,'sha256':sha,'source_id':hash_object([rel,sha])[:16]})
    if not result:
        raise ValueError('No supported input photographs found')
    missing=set(cfg.image_overrides)-{p['relative'] for p in result}
    if missing: raise ValueError(f'Overrides refer to missing inputs: {sorted(missing)}')
    missing=set(cfg.collection.expected_faces)-{p['relative'] for p in result}
    if missing: raise ValueError(f'Expected-face entries refer to missing inputs: {sorted(missing)}')
    return result


def make_plan(source,dest,cfg):
    models=ensure_models()
    items=scan(source,cfg)
    cv2.setNumThreads(cfg.runtime.threads)
    cache=dest/'.cache'
    cache.mkdir(parents=True,exist_ok=True)
    analyzer=None
    try:
        for item in items:
            key=hash_object({'sha':item['sha256'],'edge':cfg.runtime.analysis_long_edge,
                             'models':models,'analysis_revision':2,'input':cfg.input.model_dump(),
                             'max_mp':cfg.runtime.max_megapixels})
            base=cache/key
            meta=base.with_suffix('.json'); masks=base.with_suffix('.npz')
            item['cache_key']=key
            if meta.is_file() and masks.is_file():
                try:
                    cached=json.loads(meta.read_text())
                    if cached.get('masks_sha256')==digest(masks):
                        item.update(cached['analysis'])
                        emit('analysis_cached',file=item['relative'],faces=item['stats']['face_count'])
                        continue
                except (ValueError,KeyError): pass
            try:
                rgb,warnings=read_rgb(source/item['relative'],cfg)
                height,width=rgb.shape[:2]
                proxy=proxy_rgb(rgb,cfg.runtime.analysis_long_edge)
                del rgb
                if analyzer is None: analyzer=Analyzer()
                stats,maps=analyzer.analyze(proxy)
                tmp=masks.with_name(masks.name+'.tmp')
                with open(tmp,'wb') as f: np.savez_compressed(f,**maps)
                os.replace(tmp,masks)
                data={'stats':stats,'dimensions':[width,height],'warnings':warnings,'status':'ready'}
                atomic_json(meta,{'analysis':data,'masks_sha256':digest(masks)})
                item.update(data)
                emit('analysis_ready',file=item['relative'],faces=stats['face_count'])
            except Exception as exc:
                item.update(status='failed',error=str(exc))
                emit('image_failed',file=item['relative'],error=str(exc))
    finally:
        if analyzer: analyzer.close()
    values=[]
    for item in items:
        if item['status']!='ready': continue
        fs=item['stats']['faces']
        v=float(np.median([f['median_linear_luma'] for f in fs])) if fs else item['stats']['median_linear_luma']
        item['metered_luma']=v
        values.append(v)
    target=float(linear(np.array(cfg.collection.target_face_luma,np.float32))) if cfg.collection.mode=='target' else (float(np.median(values)) if values else .18)
    for item in items:
        if item['status']!='ready': continue
        ev=float(np.clip(np.log2(max(target,.01)/max(item['metered_luma'],.005))*cfg.collection.strength,
                         -cfg.collection.max_correction_ev,cfg.collection.max_correction_ev))
        item['exposure_ev']=ev if cfg.collection.enabled else 0
    plan={'version':__version__,'analysis_revision':2,'input_root':str(source),'config_hash':hash_object(cfg.model_dump()),
          'models':models,'target_linear_luma':target,'items':items}
    plan['plan_hash']=hash_object(plan)
    atomic_json(dest/'collection-plan.json',plan)
    return plan


def validate_plan(plan,source,cfg):
    if plan.get('analysis_revision')!=2: return False
    if plan.get('version')!=__version__ or plan.get('config_hash')!=hash_object(cfg.model_dump()) or plan.get('input_root')!=str(source):
        return False
    if plan.get('models')!=ensure_models(): return False
    return [(x['relative'],x['sha256']) for x in scan(source,cfg)]==[(x['relative'],x['sha256']) for x in plan['items']]


def ensure_plan(source,dest,cfg):
    path=dest/'collection-plan.json'
    if path.is_file():
        plan=json.loads(path.read_text())
        if validate_plan(plan,source,cfg):
            valid_cache=True
            for item in plan['items']:
                if item['status']!='ready': continue
                base=dest/'.cache'/item['cache_key']
                try:
                    metadata=json.loads(base.with_suffix('.json').read_text())
                    valid_cache=valid_cache and metadata['masks_sha256']==digest(base.with_suffix('.npz'))
                except (OSError,ValueError,KeyError): valid_cache=False
            if valid_cache: return plan
    return make_plan(source,dest,cfg)


def face_sharpness(image, item):
    height,width=image.shape[:2]
    values=[]
    for face in item['stats']['faces']:
        x0,y0,x1,y1=face['bbox']
        pad=(x1-x0)*.18
        a=max(0,round((x0-pad)*width)); c=min(width,round((x1+pad)*width))
        b=max(0,round((y0-pad)*height)); d=min(height,round((y1+pad)*height))
        crop=image[b:d,a:c]
        if crop.size==0: continue
        gray=luma(crop.astype(np.float32)/(65535 if crop.dtype==np.uint16 else 255))
        gray=cv2.resize(gray,(512,512),interpolation=cv2.INTER_AREA)
        score=float(cv2.Laplacian(gray,cv2.CV_32F,ksize=3).var())
        values.append({'face_id':face['id'],'score':score})
    return values


def metrics(rgb, result, maps, item):
    before=proxy_rgb(rgb,1600).astype(np.float32)/255
    after=proxy_rgb(result,1600).astype(np.float32)/255
    skin=cv2.resize(maps['face_skin'],(after.shape[1],after.shape[0]))>.5
    by=luma(before); ay=luma(after)
    def spread(values):
        if values.size < 20: return None
        return float(np.percentile(values,90)-np.percentile(values,10))
    sharpness=face_sharpness(result,item)
    return {'input_clipped_fraction':float(np.mean(np.max(before,axis=2)>=.995)),
            'output_clipped_fraction':float(np.mean(np.max(after,axis=2)>=.995)),
            'face_luma_before':float(np.median(by[skin])) if skin.any() else None,
            'face_luma_after':float(np.median(ay[skin])) if skin.any() else None,
            'face_luma_spread_before':spread(by[skin]),
            'face_luma_spread_after':spread(ay[skin]),
            'face_sharpness':sharpness,
            'min_face_sharpness':min((x['score'] for x in sharpness),default=None),
            'faces_detected':item['stats']['face_count']}


def publish_delivery(dest,run,entries):
    """Atomically publish successful edited JPEGs directly under output."""
    published=[]
    for item in entries:
        if item.get('status')=='failed': continue
        delivery=next((p for p in item.get('outputs',{}) if p.startswith('finished/') and p.endswith('.jpg')),None)
        if not delivery:
            delivery=next((p for p in item.get('outputs',{}) if p.startswith('restored/') and p.endswith('.jpg')),None)
        if not delivery:
            delivery=next((p for p in item.get('outputs',{}) if p.startswith('delivery/') and p.endswith('.jpg')),None)
        if not delivery: continue
        target=dest/Path(item['relative']).with_suffix('.jpg')
        target.parent.mkdir(parents=True,exist_ok=True)
        tmp=target.with_name('.'+target.name+'.tmp')
        shutil.copyfile(run/delivery,tmp)
        os.replace(tmp,target)
        published.append(str(target.relative_to(dest)))
    return published


def cleanup_all_face_highlights(source, dest, run, plan, entries, cfg):
    """Final full-resolution pass, after optional face restoration."""
    by_id={x['source_id']:x for x in plan['items']}
    for record in entries:
        if record.get('status')=='failed': continue
        local_cfg=for_image(cfg,record['relative'])
        glare=local_cfg.beauty.highlight_cleanup
        color=local_cfg.beauty.face_color_finish
        white=local_cfg.beauty.post_white_balance
        if not (local_cfg.beauty.enabled and ((glare.enabled and glare.strength) or
                                              (color.enabled and color.strength) or
                                              (white.enabled and white.strength))):
            continue
        if any(p.startswith('finished/') and (run/p).is_file() for p in record.get('outputs',{})):
            continue
        candidate=next((p for p in record.get('outputs',{}) if p.startswith('restored/') and p.endswith('.jpg')),None)
        if not candidate:
            candidate=next((p for p in record.get('outputs',{}) if p.startswith('delivery/') and p.endswith('.jpg')),None)
        if not candidate: continue
        item=by_id[record['source_id']]
        try:
            with np.load(dest/'.cache'/f'{item["cache_key"]}.npz') as archive:
                maps={k:archive[k] for k in archive.files}
            bgr=cv2.imread(str(run/candidate),cv2.IMREAD_COLOR)
            if bgr is None: raise ValueError(f'Cannot decode {candidate}')
            rgb=cv2.cvtColor(bgr,cv2.COLOR_BGR2RGB)
            result,pixels=cleanup_face_highlights(rgb,maps,item['stats'],local_cfg)
            result,wb=post_white_balance(result,local_cfg)
            result,color_pixels=finish_face_color(result,maps,item['stats'],local_cfg)
            stem=f'{Path(record["relative"]).stem}__{record["source_id"]}'
            finished=run/'finished'/f'{stem}.jpg'
            write_jpeg(finished,result,local_cfg.export.jpeg_quality)
            record['outputs'][str(finished.relative_to(run))]=digest(finished)
            record['highlight_cleanup']={'changed_pixels':pixels,'stage':'after_face_restoration'}
            record['face_color_finish']={'changed_pixels':color_pixels,'stage':'after_highlight_cleanup'}
            record['post_white_balance']={**wb,'stage':'before_face_color_finish'}
            preview=run/'previews'/f'{stem}.jpg'
            write_jpeg(preview,proxy_rgb(result,local_cfg.export.preview_long_edge),92)
            record['outputs'][str(preview.relative_to(run))]=digest(preview)
            original,_=read_rgb(source/record['relative'],local_cfg)
            save_review(run,source,item,original,result)
            atomic_json(run/'sidecars'/f'{record["source_id"]}.json',record)
        except (OSError,ValueError,KeyError,cv2.error) as exc:
            record.setdefault('issues',[]).append(f'face_highlight_cleanup_failed: {exc}')


def restore_soft_faces(source, dest, run, plan, entries, cfg):
    gate=cfg.detail.blur_detection
    if not (gate.enabled and gate.auto_restore):
        return
    selected=[]
    for record in entries:
        score=record.get('metrics',{}).get('min_face_sharpness')
        if record.get('status')=='failed' or score is None or score>=gate.threshold:
            continue
        if any(p.startswith('restored/') and (run/p).is_file() for p in record.get('outputs',{})):
            continue
        previous=record.get('face_restoration',{})
        if previous and Path(previous.get('output','')).is_file():
            # A rejected or sharp-skip result is also a completed gate decision.
            continue
        delivery=next((p for p in record.get('outputs',{}) if p.startswith('delivery/')),None)
        if delivery:
            selected.append({'source_id':record['source_id'],'relative':record['relative'],
                             'input':str(run/delivery),'output':str(run/'restored'/f'{Path(delivery).stem}.png')})
    if not selected:
        return
    root=Path(__file__).resolve().parents[2]
    python=Path(os.environ.get('PHOTO_CODEFORMER_PYTHON',root/'.venv-codeformer/bin/python'))
    model_root=Path(os.environ.get('PHOTO_CODEFORMER_DIR',root/'.models/CodeFormer'))
    arcface=Path(os.environ.get('PHOTO_ARCFACE_MODEL',
        root/'.venv-codeformer/lib/python3.10/site-packages/weights/facelib/recognition_arcface_ir_se50.pth'))
    request=run/'restoration-request.json'; response=run/'restoration-response.json'
    payload={'images':selected,'threshold':gate.threshold,'fidelity_candidates':gate.fidelity_candidates,
             'blend':gate.blend,'min_identity_similarity':gate.min_identity_similarity,
             'codeformer_model':str(model_root/'weights/CodeFormer/codeformer.pth'),
             'arcface_model':str(arcface)}
    atomic_json(request,payload)
    if not (python.is_file() and model_root.is_dir() and arcface.is_file()):
        for record in entries:
            if any(x['source_id']==record.get('source_id') for x in selected):
                record.setdefault('issues',[]).append('face_restoration_runtime_unavailable')
        return
    try:
        subprocess.run([str(python),str(root/'scripts/codeformer_arcface_gate.py'),
                        '--request',str(request),'--response',str(response)],
                       cwd=model_root,check=True,timeout=1800)
        results={x['source_id']:x for x in json.loads(response.read_text())['images']}
        by_id={x['source_id']:x for x in plan['items']}
        for record in entries:
            result=results.get(record.get('source_id'))
            if not result: continue
            record['face_restoration']=result
            if result.get('accepted_faces',0):
                png=Path(result['output']); rgb=cv2.cvtColor(cv2.imread(str(png)),cv2.COLOR_BGR2RGB)
                stem=Path(png).stem; restored=run/'restored'/f'{stem}.jpg'
                write_jpeg(restored,rgb,cfg.export.jpeg_quality)
                record['outputs'][str(restored.relative_to(run))]=digest(restored)
                preview=run/'previews'/f'{Path(record["relative"]).stem}__{record["source_id"]}.jpg'
                write_jpeg(preview,proxy_rgb(rgb,cfg.export.preview_long_edge),92)
                record['outputs'][str(preview.relative_to(run))]=digest(preview)
                original,_=read_rgb(source/record['relative'],for_image(cfg,record['relative']))
                save_review(run,source,by_id[record['source_id']],original,rgb)
            atomic_json(run/'sidecars'/f'{record["source_id"]}.json',record)
    except (OSError,subprocess.SubprocessError,ValueError,KeyError) as exc:
        for record in entries:
            if any(x['source_id']==record.get('source_id') for x in selected):
                record.setdefault('issues',[]).append(f'face_restoration_failed: {exc}')


def save_review(run,source,item,before,after):
    sid=item['source_id']
    width,height=before.shape[1],before.shape[0]
    for face in item['stats']['faces']:
        x0,y0,x1,y1=face['bbox']
        pad=(x1-x0)*.18
        x0=max(0,round((x0-pad)*width)); x1=min(width,round((x1+pad)*width))
        y0=max(0,round((y0-pad)*height)); y1=min(height,round((y1+pad)*height))
        for name,rgb in [('before',before),('after',after)]:
            crop=rgb[y0:y1,x0:x1]
            if crop.dtype==np.uint16: crop=(crop/257).round().astype(np.uint8)
            write_jpeg(run/'review'/'faces'/f'{sid}__{face["id"]}__{name}.jpg',crop,95)


def contact_sheet(run,source,plan,cfg):
    rows=[]
    for item in plan['items']:
        preview=run/'previews'/f'{Path(item["relative"]).stem}__{item["source_id"]}.jpg'
        if not preview.is_file(): continue
        rgb,_=read_rgb(source/item['relative'],cfg)
        before=Image.fromarray(proxy_rgb(rgb,640))
        with Image.open(preview) as im: after=im.copy()
        before.thumbnail((640,440)); after.thumbnail((640,440))
        row=Image.new('RGB',(1280,480),(25,25,25)); draw=ImageDraw.Draw(row)
        row.paste(before,(0,35)); row.paste(after,(640,35))
        draw.text((10,10),item['relative']+' | BEFORE',fill='white')
        draw.text((650,10),'AFTER — visual review required',fill='white')
        rows.append(row)
    if rows:
        sheet=Image.new('RGB',(1280,480*len(rows)))
        for i,row in enumerate(rows): sheet.paste(row,(0,480*i))
        write_jpeg(run/'review'/'contact-sheet-before-after.jpg',np.asarray(sheet),92)


def run_batch(source,dest,cfg,resume=False):
    cv2.setNumThreads(cfg.runtime.threads)
    plan=ensure_plan(source,dest,cfg)
    latest=dest/'latest.json'
    run=None
    if resume and latest.is_file():
        saved=json.loads(latest.read_text())
        candidate=dest/'runs'/saved['run_id']
        if candidate.parent.resolve()==(dest/'runs').resolve() and saved.get('plan_hash')==plan['plan_hash'] and candidate.is_dir():
            run=candidate
    if run is None:
        run=dest/'runs'/(time.strftime('%Y%m%d-%H%M%S')+'-'+uuid.uuid4().hex[:8])
        run.mkdir(parents=True)
    atomic_json(latest,{'run_id':run.name,'plan_hash':plan['plan_hash']})
    atomic_json(run/'resolved-config.json',cfg.model_dump())
    atomic_json(run/'collection-plan.json',plan)
    atomic_json(run/'manifest.json',{'input_root':str(source),'items':[{k:i[k] for k in ['relative','sha256','source_id']} for i in plan['items']]})
    entries=[]
    start=time.monotonic()
    try:
        for item in plan['items']:
            sidecar=run/'sidecars'/f'{item["source_id"]}.json'
            if resume and sidecar.is_file():
                saved=json.loads(sidecar.read_text())
                if saved.get('plan_hash')==plan['plan_hash'] and saved.get('outputs') and all((run/p).is_file() and digest(run/p)==h for p,h in saved['outputs'].items()):
                    entries.append(saved); emit('render_cached',file=item['relative']); continue
            record={'relative':item['relative'],'source_id':item['source_id'],'plan_hash':plan['plan_hash'],
                    'input_sha256':item['sha256'],'outputs':{}}
            t=time.monotonic()
            try:
                if item['status']!='ready': raise ValueError(item.get('error','Analysis failed'))
                path=source/item['relative']
                if digest(path)!=item['sha256']: raise ValueError('Input changed after analysis')
                local_cfg=for_image(cfg,item['relative'])
                rgb,warnings=read_rgb(path,local_cfg)
                with np.load(dest/'.cache'/f'{item["cache_key"]}.npz') as archive:
                    maps={k:archive[k] for k in archive.files}
                emit('render_started',file=item['relative'],exposure_ev=item['exposure_ev'])
                last_progress=[0]
                def progress(done,total):
                    if done==total or time.monotonic()-last_progress[0]>3:
                        emit('progress',file=item['relative'],done=done,total=total)
                        last_progress[0]=time.monotonic()
                result,notes=render(rgb,maps,item['stats'],local_cfg,item['exposure_ev'],progress)
                if digest(path)!=item['sha256']: raise ValueError('Input changed while rendering')
                stem=f'{Path(item["relative"]).stem}__{item["source_id"]}'
                paths=[]
                if cfg.export.master:
                    p=run/'images'/f'{stem}.tif'; write_tiff(p,result); paths.append(p)
                if cfg.export.delivery:
                    p=run/'delivery'/f'{stem}.jpg'; write_jpeg(p,np.round(result/257).astype(np.uint8),cfg.export.jpeg_quality); paths.append(p)
                p=run/'previews'/f'{stem}.jpg'; write_jpeg(p,proxy_rgb(result,cfg.export.preview_long_edge),92); paths.append(p)
                save_review(run,source,item,rgb,result)
                qa=metrics(rgb,result,maps,item)
                issues=[]
                expected=cfg.collection.expected_faces.get(item['relative'])
                if expected is not None and qa['faces_detected']!=expected:
                    issues.append(f'face_count_mismatch: expected {expected}, found {qa["faces_detected"]}')
                if qa['output_clipped_fraction']-qa['input_clipped_fraction']>.002: issues.append('increased_clipping')
                if not item['stats']['faces']: issues.append('no_faces_detected')
                if (local_cfg.detail.blur_detection.enabled and qa['min_face_sharpness'] is not None
                        and qa['min_face_sharpness'] < local_cfg.detail.blur_detection.threshold):
                    bd=local_cfg.detail.blur_detection
                    issues.append('face_blur_detected: '
                                  f'{qa["min_face_sharpness"]:.6f} < {bd.threshold:.6f}; '
                                  f'recommend {bd.restoration_model} fidelity={bd.fidelity:.2f} '
                                  f'with identity_similarity>={bd.min_identity_similarity:.2f}')
                if notes: issues.extend(notes)
                if qa['input_clipped_fraction']>.002: issues.append('source_highlights_clipped_not_recoverable')
                # Visual acceptance is separate: never claim the studio target automatically passed.
                record.update(status='needs_review' if issues else 'rendered',visual_status='not_reviewed',issues=issues,
                              warnings=warnings,metrics=qa,recipe=local_cfg.model_dump(),exposure_ev=item['exposure_ev'],
                              models=plan['models'],runtime='mediapipe-cpu/opencv-cpu',
                              outputs={str(p.relative_to(run)):digest(p) for p in paths})
                del rgb,result,maps
            except Exception as exc:
                record.update(status='failed',error=str(exc),visual_status='not_reviewed')
                emit('image_failed',file=item['relative'],error=str(exc))
            record['seconds']=round(time.monotonic()-t,3)
            atomic_json(sidecar,record); entries.append(record)
            atomic_json(run/'report.json',{'state':'running','images':entries})
            emit('image_completed',file=item['relative'],status=record['status'])
    except KeyboardInterrupt:
        atomic_json(run/'report.json',{'state':'cancelled','images':entries})
        raise
    restore_soft_faces(source,dest,run,plan,entries,cfg)
    cleanup_all_face_highlights(source,dest,run,plan,entries,cfg)
    contact_sheet(run,source,plan,cfg)
    errors=sum(i['status']=='failed' for i in entries)
    review=sum(i['status']=='needs_review' for i in entries)
    published=publish_delivery(dest,run,entries)
    summary={'state':'completed','run_dir':str(run),'seconds':round(time.monotonic()-start,3),
             'failed':errors,'needs_review':review,'images':entries}
    atomic_json(run/'report.json',summary)
    atomic_json(run/'acceptance.json',{'technical_status':'FAIL' if errors else 'PASS',
                 'visual_status':'NEEDS_REVIEW','reason':'Golden references and human visual acceptance are not yet recorded',
                 'run_id':run.name,'plan_hash':plan['plan_hash']})
    atomic_json(latest,{'run_id':run.name,'plan_hash':plan['plan_hash'],'published':published})
    emit('job_completed',run_dir=str(run),failed=errors,needs_review=review,published=published)
    return run, (3 if errors or review else 0)
