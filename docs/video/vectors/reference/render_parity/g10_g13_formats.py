from verify import *
import ref_compile as C, subprocess, time, json
F2997={'num':30000,'den':1001}
FT=K.ft({'video':{'fps':F2997}})
def mk(tracks,fps,master=None): return {'video':{'fps':fps},'tracks':tracks,'markers':[],'master':master or {'effects':[],'audio':{}}}
# 29.97 fps: 3 клипа по 90 кадров, кроссфейд 30 кадров
doc=mk([vt('V1',[clip('c1','video','red',0,0,90*FT),clip('c2','video','green',90*FT,5*FT*6,5*FT*6+90*FT,transition_in={'kind':'crossfade','duration':30*FT,'align':'center'}),
                 clip('c3','video','red',180*FT,0,90*FT)]),at_('A1',[])],F2997)
t0=time.time(); plan=C.compile_graph(doc,{'width':320,'height':180},{'sources':srcs_for(doc)})
cmd=["ffmpeg","-hide_banner","-loglevel","error","-y"]
for inp in plan['inputs']:
    c=next(c for t in doc['tracks'] for c in t['clips'] if c['id']==inp['clip']); cmd+=inp['options']+["-i",SRC[c['source']['media_id']]['path']]
cmd+=["-filter_complex",plan['filter_complex'],"-map","[vout]","-map","[abus]","-c:v","libx264","-preset","ultrafast","-pix_fmt","yuv420p","-c:a","pcm_s16le","g10.mov"]
r=subprocess.run(cmd,capture_output=True,text=True); print(r.returncode,r.stderr[-300:])
print('G10 29.97',px.probe('g10.mov'),'ожид кадров',270,'длит',270*FT/K.TPS)
print(plan['filter_complex'].split(';')[0][:200])
# 1280x720 из 320x180
doc=mkdoc([vt('V1',[clip('c1','video','red',0,0,S(2)),clip('c2','video','blue43',S(2),0,S(2))]),at_('A1',[])])
render(doc,'g11.mov',preset={'width':1280,'height':720}); print('G11',px.probe('g11.mov'))
print('  blue43 4:3 в 1280x720: контент x 160..1120 ->', px.yuv('g11.mov',3.0,640,360), px.yuv('g11.mov',3.0,100,360), px.yuv('g11.mov',3.0,200,360))
# много клипов: 100 клипов по 0.5 с на 2 дорожках
clips=[clip(f'c{i}','video','red' if i%2 else 'green',S(0.5)*i,0,S(0.5)) for i in range(100)]
doc=mkdoc([vt('V1',clips),at_('A1',[])]); t0=time.time(); render(doc,'g12.mov'); print('G12 100 клипов',px.probe('g12.mov'),'время %.1f с'%(time.time()-t0))
# только фото (нет аудио источников)
doc=mkdoc([vt('V1',[clip('p1','photo','orange',0,duration=S(2))]),at_('A1',[])]); render(doc,'g13.mov'); print('G13 фото без звука',px.probe('g13.mov'))
ffp=subprocess.run(["ffprobe","-v","error","-show_entries","stream=codec_type,channels,sample_rate","-of","csv=p=0","g13.mov"],capture_output=True,text=True).stdout; print(ffp)
def _fr(path): return dict(x.split('=') for x in px.probe(path) if x.startswith('nb_read'))['nb_read_frames']
badc = int(_fr('g10.mov') != '270') + int(_fr('g11.mov') != '100') + int(_fr('g12.mov') != '1250') + int(_fr('g13.mov') != '50')
badc += int(px.yuv('g11.mov',3.0,640,360)[2] < 110) + int(px.yuv('g11.mov',3.0,100,360)[0] != 16) + int('audio,48000,2' not in ffp)
print('BAD', badc)
