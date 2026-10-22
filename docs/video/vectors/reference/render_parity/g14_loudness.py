from verify import *
import json, re, subprocess
import ref_compile as C
duck={'id':'dk','type':'audio.duck','enabled':True,'params':{'sidechain_tracks':['A2'],'threshold_db':-30,'depth_db':12,'attack_ms':20,'release_ms':250}}
doc=mkdoc([vt('V1',[clip('v1','video','red',0,0,S(8))]),
           at_('A1',[clip('mus','audio','green',0,0,S(8),effects=[duck])],gain_db=-6.0),
           at_('A2',[clip('sp','audio','blue43',S(2),0,S(3))])])
plan=C.compile_graph(doc,{'width':320,'height':180},{'sources':srcs_for(doc)})
planA=C.compile_graph(doc,{'width':320,'height':180},{'sources':srcs_for(doc)},mode='audio')
def inputs(plan=plan):
    cmd=[]
    for inp in plan['inputs']:
        c=next(c for t in doc['tracks'] for c in t['clips'] if c['id']==inp['clip']); cmd+=inp['options']+["-i",SRC[c['source']['media_id']]['path']]
    return cmd
T,TP=-14.0,-1.0
p1=f"[abus]loudnorm=I={T}:TP={TP}:LRA=11:print_format=json[aout]"
r=subprocess.run(["ffmpeg","-hide_banner","-y"]+inputs(planA)+["-filter_complex",planA['filter_complex']+";"+p1,"-map","[aout]","-f","null","-"],capture_output=True,text=True)
print(r.returncode, r.stderr[-600:]) if r.returncode else None
j=json.loads(r.stderr[r.stderr.rindex('{'):r.stderr.rindex('}')+1]); print({k:j[k] for k in ('input_i','input_tp','input_lra','input_thresh','target_offset')})
lim=10**(TP/20)
p2=(f"[abus]loudnorm=I={T}:TP={TP}:LRA=11:measured_I={j['input_i']}:measured_TP={j['input_tp']}:measured_LRA={j['input_lra']}:"
    f"measured_thresh={j['input_thresh']}:offset={j['target_offset']}:linear=true:print_format=summary,alimiter=limit={lim:.6f}:level=disabled[aout]")
r=subprocess.run(["ffmpeg","-hide_banner","-y"]+inputs()+["-filter_complex",plan['filter_complex']+";"+p2,"-map","[vout]","-map","[aout]","-c:v","libx264","-preset","ultrafast","-pix_fmt","yuv420p","-c:a","aac","-b:a","192k","g14.mp4"],capture_output=True,text=True)
print(r.returncode, r.stderr[-200:])
# измерить итог
m=subprocess.run(["ffmpeg","-hide_banner","-i","g14.mp4","-af","loudnorm=I=-14:TP=-1:LRA=11:print_format=json","-f","null","-"],capture_output=True,text=True).stderr
jj=json.loads(m[m.rindex('{'):m.rindex('}')+1]); print('итог: input_i',jj['input_i'],'input_tp',jj['input_tp'])
print('BAD', int(abs(float(jj['input_i']) - T) > 0.5))
