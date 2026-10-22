import os, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
import json, subprocess, sys, math, os, struct, wave
import ref_compile as C, ref_editor_core as K, px
S=lambda x: round(x*K.TPS)
FPS25={'num':25,'den':1}
def rgb2yuv(r,g,b):
    Kr,Kb=0.2126,0.0722; Kg=1-Kr-Kb
    y=(Kr*r+Kg*g+Kb*b)/255; cb=(b/255-y)/(2*(1-Kb)); cr=(r/255-y)/(2*(1-Kr))
    return (16+219*y,128+224*cb,128+224*cr)
BLACK=(16.0,128.0,128.0)
COL={'red':(255,0,0),'green':(0,255,0),'blue43':(0,0,255),'orange':(255,128,0)}
SRC={'red':{'path':'red.mp4','has_audio':True,'channels':1,'duration':S(12),'freq':440},
     'green':{'path':'green.mp4','has_audio':True,'channels':1,'duration':S(12),'freq':880},
     'blue43':{'path':'blue43.mp4','has_audio':True,'channels':1,'duration':S(12),'freq':220},
     'orange':{'path':'photo_orange.png','has_audio':False},
     'grad':{'path':'photo_grad.png','has_audio':False}}
def clip(cid,kind,sid,at,src_in=0,src_out=0,**kw):
    if kind=='photo':
        c={'id':cid,'source':{'kind':'photo','asset_version_id':sid,'sha256':'h'+cid},'at':at,'duration':kw.pop('duration'),'fit':'fit','effects':[]}
    else:
        c={'id':cid,'source':{'kind':kind,'media_id':sid,'sha256':'h'+cid},'at':at,'src_in':src_in,'src_out':src_out,'speed':{'num':1,'den':1},
           'fit':'fit','audio':{'gain_db':0.0,'fade_in':0,'fade_out':0,'mute':False},'effects':[]}
    c.update(kw); return c
def mkdoc(tracks,master=None):
    return {'video':{'fps':FPS25},'tracks':tracks,'markers':[],'master':master or {'effects':[],'audio':{}}}
def vt(tid,clips,**kw): d={'id':tid,'kind':'video','clips':clips}; d.update(kw); return d
def at_(tid,clips,**kw): d={'id':tid,'kind':'audio','clips':clips}; d.update(kw); return d
def srcs_for(doc): return {k:{kk:vv for kk,vv in v.items() if kk!='path'} for k,v in SRC.items()}
def render(doc,out,preset=None,two_pass=False):
    preset=preset or {'width':320,'height':180}
    plan=C.compile_graph(doc,preset,{'sources':srcs_for(doc)})
    cmd=["ffmpeg","-hide_banner","-loglevel","error","-y"]
    for it,inp in zip(range(len(plan['inputs'])),plan['inputs']):
        cid=inp['clip']; c=next(c for t in doc['tracks'] for c in t['clips'] if c['id']==cid)
        sid=c['source'].get('media_id') or c['source'].get('asset_version_id')
        cmd+=inp['options']+["-i",SRC[sid]['path']]
    cmd+=["-filter_complex",plan['filter_complex'],"-map","[vout]","-map","[abus]","-c:v","libx264","-preset","ultrafast","-crf","12","-pix_fmt","yuv420p",
          "-colorspace","bt709","-color_primaries","bt709","-color_trc","bt709","-color_range","tv","-c:a","pcm_s16le",out]
    r=subprocess.run(cmd,capture_output=True,text=True)
    if r.returncode: print('FFMPEG FAIL\n',plan['filter_complex'],'\n',r.stderr[-1500:]); raise SystemExit(1)
    return plan
def expected_center(doc,t,srcmap=None):
    sd={k:v['duration'] for k,v in SRC.items() if 'duration' in v}
    comp=K.compose_at(doc,S(t),sd)
    cur=BLACK
    for L in comp['layers']:
        c=next(c for tr in doc['tracks'] for c in tr['clips'] if c['id']==L['clip'])
        sid=c['source'].get('media_id') or c['source'].get('asset_version_id')
        col=rgb2yuv(*COL[sid]); b=L['brightness']; b=b['num']/b['den']
        a=L['opacity']; a=a['num']/a['den']
        col=tuple(BLACK[i]+(col[i]-BLACK[i])*b for i in range(3))
        cur=tuple(cur[i]*(1-a)+col[i]*a for i in range(3))
    return cur
def check_video(doc,out,times,tol=9):
    bad=0
    for t in times:
        got=px.yuv(out,t-0.001)  # кадр с pts >= t-1мс, то есть кадр момента t
        exp=expected_center(doc,t)
        ok=got and all(abs(got[i]-exp[i])<=tol for i in range(3))
        if not ok: bad+=1
        print(('  ok ' if ok else '  BAD'),f't={t:.3f}',got,tuple(round(x) for x in exp))
    return bad
def audio_rms(path,t0,t1):
    r=subprocess.run(["ffmpeg","-hide_banner","-loglevel","error","-i",path,"-vn","-f","s16le","-ac","2","-ar","48000","-"],capture_output=True)
    d=r.stdout; n=len(d)//4; a=int(t0*48000)*2; b=int(t1*48000)*2
    s=struct.unpack('<%dh'%(n*2),d)
    seg=s[a:b]; return math.sqrt(sum(x*x for x in seg)/max(1,len(seg)))/32768
def expected_rms(doc,t,dt=0.1):
    sd={k:v['duration'] for k,v in SRC.items() if 'duration' in v}
    acc=0.0; n=40
    for k in range(n):
        tt=int(S(t)+S(dt)*(k+0.5)/n)
        comp=K.compose_at(doc,tt,sd)
        sq=0.0
        for v in comp['voices']:
            if v['muted']: continue
            c=next(c for tr in doc['tracks'] for c in tr['clips'] if c['id']==v['clip'])
            g=10**(v['gain_db']/20)*v['env']; amp=0.125*g   # sine amplitude 1/8, mono -> stereo copy
            sq+=amp*amp/2
        acc+=sq
    return math.sqrt(acc/n)
def check_audio(doc,out,times,tol=0.006):
    bad=0
    for t in times:
        got=audio_rms(out,t,t+0.1); exp=expected_rms(doc,t)
        ok=abs(got-exp)<=tol+0.05*exp
        if not ok: bad+=1
        print(('  ok ' if ok else '  BAD'),f'a t={t:.2f} rms got={got:.4f} exp={exp:.4f}')
    return bad
def dur_frames(out):
    p=px.probe(out); return p
