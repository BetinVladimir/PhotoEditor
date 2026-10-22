from pathlib import Path
import json
import cv2
import numpy as np
import pytest
from PIL import Image
import tifffile
from photoeditor.config import Config, for_image, load, env_paths
from photoeditor.analysis import linear, encoded
from photoeditor.render import render, geometry_field
from photoeditor.io import write_tiff, read_rgb, atomic_json, hash_object
from photoeditor.jobs import scan, job_lock, face_sharpness


def no_op():
    return Config.model_validate({'color':{'enabled':False},'collection':{'enabled':False},
        'beauty':{'enabled':False},'detail':{'sharpening':{'enabled':False}}})


def maps_for(h,w):
    z=np.zeros((h,w),np.float32)
    return {k:z.copy() for k in ['face_skin','body_skin','person','protected','face_oval','luma']}


def test_config_rejects_unknown_conflict_duplicate_and_alias(tmp_path):
    for text in ['geometry: {enabled: true}', 'beauty: {skin: {unknown: 2}}',
                 'profile: a\nprofile: b', 'profile: &a hello\nimage_overrides: {x: *a}',
                 'color: {wb_gains: [1, 1]}','color: {exposure_ev: .nan}']:
        p=tmp_path/'bad.yaml';p.write_text(text)
        with pytest.raises(ValueError): load(p)


def test_overrides_checked_before_render(tmp_path):
    p=tmp_path/'bad.yaml';p.write_text('image_overrides:\n  a.jpg:\n    beauty: {skin: {strength_typo: 1}}')
    with pytest.raises(ValueError): load(p)
    cfg=Config(image_overrides={'a.jpg':{'color':{'exposure_ev':1}}})
    assert for_image(cfg,'a.jpg').color.exposure_ev==1
    assert cfg.color.exposure_ev==0


def test_env_rejects_overlap_and_symlink(tmp_path,monkeypatch):
    inp=tmp_path/'in';inp.mkdir();(tmp_path/'alias').symlink_to(inp,target_is_directory=True)
    cfg=tmp_path/'config.yaml';cfg.write_text('{}')
    monkeypatch.setenv('PHOTO_INPUT_DIR',str(inp));monkeypatch.setenv('PHOTO_CONFIG_FILE',str(cfg))
    for out in [inp,inp/'output',tmp_path,tmp_path/'alias']:
        monkeypatch.setenv('PHOTO_OUTPUT_DIR',str(out))
        with pytest.raises(ValueError): env_paths()


def test_linear_transfer_roundtrip():
    x=np.linspace(0,1,10000,dtype=np.float32)
    assert np.max(np.abs(encoded(linear(x))-x))<2e-7


def test_noop_preserves_16bit_and_tiff_roundtrip(tmp_path):
    rng=np.random.default_rng(12)
    arr=rng.integers(0,65536,(129,257,3),dtype=np.uint16)
    cfg=no_op(); out,_=render(arr,maps_for(43,86),{'faces':[]},cfg,0)
    assert np.max(np.abs(out.astype(int)-arr.astype(int)))<=1
    path=tmp_path/'test.tif';write_tiff(path,out)
    decoded,warnings=read_rgb(path,cfg)
    assert np.array_equal(decoded,out)
    with tifffile.TiffFile(path) as tf:
        assert tf.pages[0].bitspersample==16
        assert 34675 in tf.pages[0].tags


def test_tiles_match_full_reference():
    rng=np.random.default_rng(13)
    # Smooth portrait-like gradients with texture across tile borders.
    h,w=351,423
    xx,yy=np.meshgrid(np.linspace(0,1,w),np.linspace(0,1,h))
    arr=np.stack([.3+.4*xx,.2+.4*yy,.15+.25*xx],axis=2)
    arr=np.uint8(np.clip(arr+rng.normal(0,.012,arr.shape),0,1)*255)
    maps=maps_for(117,141); maps['face_skin'][:]=.8;maps['luma'][:]=.2
    cfg=Config();cfg.runtime.tile_size=128
    tiled,_=render(arr,maps,{'faces':[]},cfg,.4)
    cfg.runtime.tile_size=1024
    full,_=render(arr,maps,{'faces':[]},cfg,.4)
    assert np.max(np.abs(tiled.astype(int)-full.astype(int)))<=7


def test_geometry_off_and_field_bounds():
    maps=maps_for(200,300)
    cfg=Config()
    field,_=geometry_field({'faces':[]},maps,cfg)
    assert not field.any()
    cfg.beauty.preserve_geometry=False;cfg.geometry.enabled=True
    cfg.geometry.body_regions=[(.3,.2,.7,.9)]
    field,notes=geometry_field({'faces':[]},maps,cfg)
    assert np.max(np.abs(field))<=.4*300*cfg.geometry.max_body_displacement*cfg.geometry.body_strength
    assert np.min(1+np.gradient(field,axis=1))>.7
    assert notes


def test_orientation_applied_once(tmp_path):
    im=Image.new('RGB',(80,40),'red');exif=Image.Exif();exif[274]=6
    p=tmp_path/'rot.jpg';im.save(p,exif=exif)
    a,_=read_rgb(p,Config());assert a.shape==(80,40,3)


def test_scan_no_symlink_and_stable_ids(tmp_path):
    p=tmp_path/'a.jpg';Image.new('RGB',(20,20),'blue').save(p)
    (tmp_path/'link.jpg').symlink_to(p)
    first=scan(tmp_path,Config());assert len(first)==1
    assert scan(tmp_path,Config())==first
    Image.new('RGB',(20,20),'red').save(p)
    assert scan(tmp_path,Config())[0]['source_id']!=first[0]['source_id']


def test_lock_released_on_exception(tmp_path):
    with pytest.raises(RuntimeError):
        with job_lock(tmp_path):
            with pytest.raises(ValueError):
                with job_lock(tmp_path): pass
            raise RuntimeError()
    with job_lock(tmp_path): pass


def test_batch_resume_and_corrupt_file_isolation(tmp_path, monkeypatch):
    from photoeditor import jobs
    source=tmp_path/'input';source.mkdir()
    dest=tmp_path/'output'
    Image.new('RGB',(192,128),(120,90,80)).save(source/'portrait.jpg')
    (source/'broken.jpg').write_bytes(b'not a jpeg')
    class FakeAnalyzer:
        def analyze(self,rgb):
            h,w=rgb.shape[:2]
            return {'faces':[],'face_count':0,'median_linear_luma':.2,
                    'clipped_fraction':0,'proxy_size':[w,h]},maps_for(h,w)
        def close(self):pass
    monkeypatch.setattr(jobs,'Analyzer',FakeAnalyzer)
    monkeypatch.setattr(jobs,'ensure_models',lambda:{'test':'fixed'})
    cfg=no_op()
    with jobs.job_lock(dest):
        run,code=jobs.run_batch(source,dest,cfg)
    report=json.loads((run/'report.json').read_text())
    assert report['failed']==1 and code==3
    good=next(i for i in report['images'] if i['relative']=='portrait.jpg')
    assert len(good['outputs'])==3
    assert (dest/'portrait.jpg').is_file()
    assert json.loads((dest/'latest.json').read_text())['published']==['portrait.jpg']
    monkeypatch.setattr(jobs,'render',lambda *a,**k: (_ for _ in ()).throw(AssertionError('Must use cached render')))
    with jobs.job_lock(dest):
        resumed,_=jobs.run_batch(source,dest,cfg,resume=True)
    assert resumed==run
    assert json.loads((run/'report.json').read_text())['failed']==1


def test_corrupt_analysis_cache_is_rebuilt(tmp_path,monkeypatch):
    from photoeditor import jobs
    source=tmp_path/'input';source.mkdir()
    Image.new('RGB',(80,60),'gray').save(source/'a.jpg')
    dest=tmp_path/'output';calls=[]
    class FakeAnalyzer:
        def analyze(self,rgb):
            calls.append(1);h,w=rgb.shape[:2]
            return {'faces':[],'face_count':0,'median_linear_luma':.2}, maps_for(h,w)
        def close(self):pass
    monkeypatch.setattr(jobs,'Analyzer',FakeAnalyzer)
    monkeypatch.setattr(jobs,'ensure_models',lambda:{'test':'fixed'})
    cfg=no_op();plan=jobs.make_plan(source,dest,cfg)
    (dest/'.cache'/f'{plan["items"][0]["cache_key"]}.npz').write_bytes(b'broken')
    jobs.ensure_plan(source,dest,cfg)
    assert len(calls)==2


def test_brightening_does_not_create_white_clipping():
    from photoeditor.render import process_tile
    rgb=np.full((80,80,3),.95,np.float32)
    maps={k:np.zeros((80,80),np.float32)
          for k in ['face_skin','body_skin','face_oval','protected','relight_mask','face_target','ev_field']}
    cfg=Config();cfg.color.gamma=1.09
    result=process_tile(rgb,maps,cfg,2.5,2.)
    assert result.max()<.995


def test_body_warp_does_not_move_inside_face():
    maps=maps_for(200,300);maps['face_oval'][20:80,100:160]=1
    cfg=Config.model_validate({'beauty':{'preserve_geometry':False},'geometry':{'enabled':True,'body_regions':[[.2,.1,.8,.9]]}})
    field,notes=geometry_field({'faces':[]},maps,cfg)
    assert np.max(np.abs(field[20:80,100:160]))==0
    assert np.min(1+np.gradient(field,axis=1))>=.749


def test_even_light_reduces_large_face_shadow_without_flattening_detail():
    from photoeditor.render import prepare_maps
    h,w=160,200
    maps=maps_for(h,w)
    maps['luma'][:]=.28
    maps['luma'][:, :100]=.07
    maps['face_skin'][20:145,30:170]=1
    face={'points':[[.2,.1]]*478,'bbox':[.15,.1,.85,.95],
          'median_linear_luma':.18,'id':'person-1'}
    cfg=Config()
    field=prepare_maps(maps,{'faces':[face]},cfg)['ev_field']
    assert np.median(field[45:120,45:85]) > np.median(field[45:120,115:155])+.35
    assert np.max(np.abs(field)) <= cfg.beauty.even_light.max_local_ev+.1


def test_auto_red_blemish_reduces_local_red_chroma():
    from photoeditor.render import process_tile
    rgb=np.full((100,100,3),.48,np.float32)
    rgb[45:55,45:55,0]=.72
    maps={k:np.zeros((100,100),np.float32) for k in ['face_skin','body_skin','face_oval','protected','relight_mask','face_target','ev_field']}
    maps['face_skin'][:]=1
    cfg=Config.model_validate({'beauty':{'blemishes':{'enabled':True,'auto_redness':True,'auto_strength':1}}})
    before=float(rgb[50,50,0]-(rgb[50,50,1]+rgb[50,50,2])*.5)
    result=process_tile(rgb,maps,cfg,0,4)
    after=float(result[50,50,0]-(result[50,50,1]+result[50,50,2])*.5)
    assert after < before*.7
    assert np.max(np.abs(result[20,20]-rgb[20,20])) < .03


def test_face_blur_score_separates_soft_and_detailed_crop():
    h=w=256
    yy,xx=np.mgrid[:h,:w]
    detailed=np.uint8((((xx//4+yy//4)%2)*120+60))[...,None].repeat(3,axis=2)
    soft=cv2.GaussianBlur(detailed,(0,0),12)
    item={'stats':{'faces':[{'id':'person-1','bbox':[.2,.2,.8,.8]}]}}
    sharp_score=face_sharpness(detailed,item)[0]['score']
    soft_score=face_sharpness(soft,item)[0]['score']
    assert sharp_score > soft_score*20
    assert soft_score < Config().detail.blur_detection.threshold


def test_face_recovery_activates_only_for_soft_face():
    from photoeditor.render import prepare_maps, process_tile
    h=w=160
    maps=maps_for(h,w);maps['person'][:]=1;maps['face_oval'][20:145,25:135]=1
    maps['face_skin'][25:140,30:130]=1;maps['luma'][:]=.35
    face={'points':[[.2,.1]]*478,'bbox':[.16,.1,.84,.94],
          'median_linear_luma':.18,'id':'person-1'}
    cfg=Config();cfg.detail.blur_detection.threshold=.002
    prepared=prepare_maps(maps,{'faces':[face]},cfg)
    assert prepared['recovery_mask'].max()>.9
    rgb=np.full((h,w,3),.4,np.float32)
    rgb[:,::8]=.47
    before=float(cv2.Laplacian(rgb[...,0],cv2.CV_32F).var())
    local={k:prepared[k] for k in ['face_skin','body_skin','face_oval','protected','relight_mask','recovery_mask','face_target','ev_field']}
    result=process_tile(rgb,local,cfg,0,3)
    after=float(cv2.Laplacian(result[...,0],cv2.CV_32F).var())
    assert after>before


def test_restoration_candidate_config_is_bounded():
    cfg=Config.model_validate({'detail':{'blur_detection':{
        'auto_restore':True,'fidelity_candidates':[.85,.92,.97],'blend':.65}}})
    assert cfg.detail.blur_detection.auto_restore
    assert cfg.detail.blur_detection.fidelity_candidates==[.85,.92,.97]
    for values in [[],[.5]*6]:
        with pytest.raises(ValueError):
            Config.model_validate({'detail':{'blur_detection':{'fidelity_candidates':values}}})


def test_post_highlight_cleanup_reduces_glare_and_preserves_features():
    from photoeditor.render import cleanup_face_highlights
    h=w=180
    rgb=np.full((h,w,3),np.uint16(.46*65535),np.uint16)
    rgb[55:105,75:120]=np.uint16(.86*65535)
    maps=maps_for(h,w);maps['face_skin'][20:160,25:155]=1
    maps['protected'][72:88,82:112]=1
    stats={'faces':[{'bbox':[.12,.08,.88,.94]}]}
    cfg=Config.model_validate({'beauty':{'highlight_cleanup':{
        'enabled':True,'strength':.8,'threshold':.55,'local_contrast':.02,'max_reduction':.18}}})
    out,changed=cleanup_face_highlights(rgb.copy(),maps,stats,cfg)
    assert changed>0
    assert out[62,92,0] < rgb[62,92,0]
    assert np.array_equal(out[78,95],rgb[78,95])
    assert np.array_equal(out[10,10],rgb[10,10])


def test_face_color_finish_warms_gray_skin_without_luma_shift():
    from photoeditor.render import finish_face_color
    from photoeditor.analysis import luma
    h=w=160
    rgb=np.full((h,w,3),np.uint16(.52*65535),np.uint16)
    rgb[20:140,20:140]=np.array([.56,.50,.47])*65535
    rgb[55:105,65:115]=np.uint16(.60*65535)  # neutral gray patch
    maps=maps_for(h,w);maps['face_skin'][18:142,18:142]=1
    maps['protected'][70:90,75:105]=1
    stats={'faces':[{'bbox':[.1,.1,.9,.9]}]}
    cfg=Config.model_validate({'beauty':{'face_color_finish':{
        'enabled':True,'strength':.8,'saturation':.15,'warmth':.018,'max_chroma_shift':.05}}})
    out,changed=finish_face_color(rgb.copy(),maps,stats,cfg)
    assert changed>0
    assert int(out[60,70,0])-int(out[60,70,2]) > int(rgb[60,70,0])-int(rgb[60,70,2])
    before=float(luma(rgb.astype(np.float32)/65535)[60,70])
    after=float(luma(out.astype(np.float32)/65535)[60,70])
    assert abs(after-before)<.01
    assert np.array_equal(out[80,85],rgb[80,85])


def test_post_white_balance_neutralizes_cast_and_protects_highlights():
    from photoeditor.render import post_white_balance
    rgb=np.zeros((120,180,3),np.uint16)
    rgb[:]=np.array([.62,.54,.45])*65535
    rgb[20:55,20:70]=np.array([1.,.98,.91])*65535
    cfg=Config.model_validate({'beauty':{'post_white_balance':{
        'enabled':True,'strength':.8,'max_gain':.2,'neutral_saturation':.3,
        'highlight_start':.75,'highlight_headroom':.96}}})
    out,metrics=post_white_balance(rgb,cfg)
    before=rgb[90,120].astype(float)/65535;after=out[90,120].astype(float)/65535
    assert np.ptp(after)<np.ptp(before)
    assert metrics['gains'][0]<1 and metrics['gains'][2]>1
    assert out[30,40].max()<rgb[30,40].max()
    assert metrics['clipped_after']<=metrics['clipped_before']


# Provider doubles below test the consumer boundary, not model quality.
def restoration_case(tmp_path, monkeypatch):
    from photoeditor import jobs
    source=tmp_path/'source';source.mkdir()
    run=tmp_path/'run';(run/'delivery').mkdir(parents=True)
    # A legitimate prior geometry edit may differ from the raw source canvas.
    cv2.imwrite(str(source/'portrait.png'),np.full((60,90,3),100,np.uint8))
    delivery=run/'delivery'/'portrait.jpg'
    cv2.imwrite(str(delivery),np.full((40,70,3),100,np.uint8))
    python=tmp_path/'model-python';python.touch()
    model=tmp_path/'model';model.mkdir()
    arcface=tmp_path/'arcface.pth';arcface.touch()
    monkeypatch.setenv('PHOTO_CODEFORMER_PYTHON',str(python))
    monkeypatch.setenv('PHOTO_CODEFORMER_DIR',str(model))
    monkeypatch.setenv('PHOTO_ARCFACE_MODEL',str(arcface))
    cfg=Config.model_validate({'detail':{'blur_detection':{'enabled':True,'auto_restore':True}}})
    record=dict(source_id='face1',relative='portrait.png',status='ready',
                metrics=dict(min_face_sharpness=0),outputs={'delivery/portrait.jpg':'original-delivery-hash'})
    plan=dict(items=[dict(source_id='face1',relative='portrait.png',stats=dict(faces=[]))])
    reviews=[]
    monkeypatch.setattr(jobs,'save_review',lambda *args: reviews.append(args))
    return jobs,source,run,cfg,record,plan,reviews


def restoration_provider(monkeypatch,jobs,shapes,accepted):
    calls=[]
    def invoke(argv,**kwargs):
        request=json.loads(Path(argv[argv.index('--request')+1]).read_text())
        response=Path(argv[argv.index('--response')+1])
        spec=request['images'][0];calls.append(spec)
        shape=shapes[min(len(calls)-1,len(shapes)-1)]
        output=Path(spec['output']);output.parent.mkdir(parents=True,exist_ok=True)
        if shape is None:
            output.write_bytes(b'not an image')
        else:
            cv2.imwrite(str(output),np.full(shape,120,np.uint8))
        response.write_text(json.dumps(dict(images=[dict(source_id=spec['source_id'],
            relative=spec['relative'],output=str(output),accepted_faces=accepted,faces=[])])))
    monkeypatch.setattr(jobs.subprocess,'run',invoke)
    return calls


@pytest.mark.parametrize('shape', [(80,140,3),(20,35,3),(40,70),(40,70,4),None])
@pytest.mark.parametrize('accepted', [0,1])
def test_restoration_refuses_invalid_provider_geometry_before_acceptance(tmp_path,monkeypatch,shape,accepted):
    jobs,source,run,cfg,record,plan,reviews=restoration_case(tmp_path,monkeypatch)
    calls=restoration_provider(monkeypatch,jobs,[shape],accepted)
    before=dict(record['outputs'])
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert len(calls)==1
    assert 'face_restoration' not in record
    assert record['outputs']==before
    assert reviews==[]
    assert not list((run/'restored').glob('*.jpg'))
    assert not (run/'previews').exists()
    assert not (run/'sidecars').exists()
    assert any('output geometry differs from selected delivery input' in issue for issue in record['issues'])


@pytest.mark.parametrize('accepted', [0,1])
def test_restoration_accepts_same_delivery_geometry_and_completes_once(tmp_path,monkeypatch,accepted):
    jobs,source,run,cfg,record,plan,reviews=restoration_case(tmp_path,monkeypatch)
    calls=restoration_provider(monkeypatch,jobs,[(40,70,3)],accepted)
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert len(calls)==1
    assert record['face_restoration']['accepted_faces']==accepted
    persisted=json.loads((run/'sidecars'/'face1.json').read_text())
    assert persisted['face_restoration']==record['face_restoration']
    if accepted:
        assert cv2.imread(str(run/'restored'/'portrait.jpg')).shape==(40,70,3)
        assert 'restored/portrait.jpg' in record['outputs']
        assert len(reviews)==1
    else:
        assert record['outputs']=={'delivery/portrait.jpg':'original-delivery-hash'}
        assert reviews==[]
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert len(calls)==1


def test_restoration_invalid_result_remains_retryable(tmp_path,monkeypatch):
    jobs,source,run,cfg,record,plan,reviews=restoration_case(tmp_path,monkeypatch)
    calls=restoration_provider(monkeypatch,jobs,[(80,140,3),(40,70,3)],1)
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert 'face_restoration' not in record
    assert reviews==[]
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert len(calls)==2
    assert record['face_restoration']['accepted_faces']==1
    assert cv2.imread(str(run/'restored'/'portrait.jpg')).shape==(40,70,3)
    assert len(reviews)==1


@pytest.mark.parametrize('accepted', [0,1])
def test_restoration_repairs_historical_bad_completion_without_losing_evidence(tmp_path,monkeypatch,accepted):
    jobs,source,run,cfg,record,plan,reviews=restoration_case(tmp_path,monkeypatch)
    output=run/'restored'/'portrait.png';output.parent.mkdir()
    cv2.imwrite(str(output),np.full((80,140,3),210,np.uint8))
    previous=dict(source_id='face1',relative='portrait.png',output=str(output),accepted_faces=1,faces=[])
    record['face_restoration']=previous
    restored=output.with_suffix('.jpg');cv2.imwrite(str(restored),np.full((80,140,3),210,np.uint8))
    record['outputs']['restored/portrait.jpg']='historical-bad-jpeg'
    preview=run/'previews'/'portrait__face1.jpg';preview.parent.mkdir()
    cv2.imwrite(str(preview),np.full((80,140,3),210,np.uint8))
    record['outputs']['previews/portrait__face1.jpg']='historical-bad-preview'
    old={p.relative_to(run).as_posix():p.read_bytes() for p in (output,restored,preview)}
    calls=restoration_provider(monkeypatch,jobs,[(40,70,3)],accepted)
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert len(calls)==1
    assert record['face_restoration']['accepted_faces']==accepted
    assert len(record['face_restoration_history'])==1
    history=run/record['face_restoration_history'][0]
    archived=json.loads((history/'record.json').read_text())
    assert archived['record']['face_restoration']==previous
    for relative,raw in old.items():
        assert (history/'files'/relative).read_bytes()==raw
    assert cv2.imread(str(output)).shape==(40,70,3)
    if accepted:
        assert cv2.imread(str(restored)).shape==(40,70,3)
        assert 'restored/portrait.jpg' in record['outputs']
    else:
        assert restored.read_bytes()==old['restored/portrait.jpg']
        assert 'restored/portrait.jpg' not in record['outputs']
        assert 'previews/portrait__face1.jpg' not in record['outputs']
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert len(calls)==1
    assert len(record['face_restoration_history'])==1


def test_restoration_failed_historical_repair_keeps_previous_evidence_and_retries(tmp_path,monkeypatch):
    jobs,source,run,cfg,record,plan,reviews=restoration_case(tmp_path,monkeypatch)
    output=run/'restored'/'portrait.png';output.parent.mkdir()
    cv2.imwrite(str(output),np.full((80,140,3),210,np.uint8))
    original=output.read_bytes()
    previous=dict(source_id='face1',relative='portrait.png',output=str(output),accepted_faces=0,faces=[])
    record['face_restoration']=previous
    calls=restoration_provider(monkeypatch,jobs,[(80,140,3),(40,70,3)],0)
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert record['face_restoration']==previous
    assert len(record['face_restoration_history'])==1
    history=run/record['face_restoration_history'][0]
    assert (history/'files'/'restored'/'portrait.png').read_bytes()==original
    assert reviews==[]
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert len(calls)==2
    assert cv2.imread(str(output)).shape==(40,70,3)
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert len(calls)==2


def test_restoration_history_failure_refuses_overwrite(tmp_path,monkeypatch):
    jobs,source,run,cfg,record,plan,reviews=restoration_case(tmp_path,monkeypatch)
    output=run/'restored'/'portrait.png';output.parent.mkdir()
    cv2.imwrite(str(output),np.full((80,140,3),210,np.uint8))
    old=output.read_bytes()
    record['face_restoration']=dict(output=str(output),accepted_faces=0)
    calls=restoration_provider(monkeypatch,jobs,[(40,70,3)],1)
    def fail_history(*args):
        raise OSError('archive unavailable')
    monkeypatch.setattr(jobs,'preserve_restoration_history',fail_history)
    jobs.restore_soft_faces(source,tmp_path,run,plan,[record],cfg)
    assert calls==[] and output.read_bytes()==old
    assert any('history_failed' in issue for issue in record['issues'])
