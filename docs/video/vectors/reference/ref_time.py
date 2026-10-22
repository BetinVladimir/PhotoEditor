import json, math
TPS=705_600_000
SUPPORTED={ "24/1":(24,1),"25/1":(25,1),"30/1":(30,1),"50/1":(50,1),"60/1":(60,1),
 "24000/1001":(24000,1001),"30000/1001":(30000,1001),"60000/1001":(60000,1001)}
def frame_ticks(num,den):
    v=TPS*den
    if v%num: raise ValueError('FPS_UNSUPPORTED')
    return v//num
def clip_duration(c):
    if c['source']['kind']=='photo': return c['duration']
    n,d=c.get('speed',{'num':1,'den':1}).values() if 'speed' in c else (1,1)
    return ((c['src_out']-c['src_in'])*d)//n
out={}
out['frame_ticks']={k:frame_ticks(*v) for k,v in SUPPORTED.items()}
out['sample_ticks']={'48000':TPS//48000,'44100':TPS//44100}
assert TPS%48000==0 and TPS%44100==0
out['max_ticks_3h']=3*3600*TPS
out['max_safe']=2**53-1
out['frames_to_ticks']=[{'fps':'30000/1001','frame':n,'ticks':n*frame_ticks(30000,1001)} for n in (0,1,30,1800)]
# ticks_to_frame floor
ft=frame_ticks(30000,1001)
out['ticks_to_frame']=[{'fps':'30000/1001','ticks':t,'floor':t//ft,'round':(t+ft//2)//ft} for t in (0,ft-1,ft,ft*10+ft//2-1,ft*10+ft//2,TPS)]
json.dump(out,open('time.json','w'),indent=1)
print(json.dumps(out,indent=1)[:1500])
