import json
TPS=705_600_000
FT={'25/1':28_224_000}
THR={1:0.8,2:0.65,3:0.5,4:0.4,5:0.3}
def runs(mask):
    out=[];s=None
    for i,m in enumerate(mask+[False]):
        if m and s is None:s=i
        if not m and s is not None:out.append((s,i-1));s=None
    return out
def postprocess(single,many,luma,sens=3,min_shot=8,mode='all',include_flash=False,fps='25/1'):
    n=len(single);t=THR[sens];c=[]
    flashes=[]
    for i in range(1,n-1):
        if luma[i]-luma[i-1]>60 and luma[i]-luma[i+1]>60 and abs(luma[i-1]-luma[i+1])<20: flashes.append(i)
    for a,b in runs([v>=t for v in single]):
        seg=single[a:b+1];k=a+seg.index(max(seg))
        c.append({'kind':'cut','frame_in':k+1,'frame_out':k+1,'confidence':single[k]})
    if mode=='all':
        for a,b in runs([v>=t for v in many]):
            if b-a+1>=3:
                c=[x for x in c if not (x['kind']=='cut' and a<=x['frame_in']<=b+1)]
                c.append({'kind':'dissolve','frame_in':a,'frame_out':b+1,'confidence':max(many[a:b+1])})
        # fades
        for a,b in runs([v<=24 for v in luma]):
            if b-a+1>=2:
                j=a
                while j>0 and luma[j-1]>luma[j]: j-=1
                if a-j>=5 and luma[j]>=64: c.append({'kind':'fade_out','frame_in':j,'frame_out':a,'confidence':0.9})
                k=b
                while k<n-1 and luma[k+1]>luma[k]: k+=1
                if k-b>=5 and luma[k]>=64: c.append({'kind':'fade_in','frame_in':b,'frame_out':k,'confidence':0.9})
    # flash suppression of cuts
    fl=set(flashes)
    c=[x for x in c if not (x['kind']=='cut' and (x['frame_in'] in fl or x['frame_in']-1 in fl))]
    if mode=='all' and include_flash:
        for i in flashes: c.append({'kind':'flash','frame_in':i,'frame_out':i+1,'confidence':0.8})
    c.sort(key=lambda x:(x['frame_in'],-x['confidence'],x['kind']))
    kept=[]
    for x in c:
        if kept and x['frame_in']-kept[-1]['frame_in']<min_shot:
            if x['confidence']>kept[-1]['confidence']: kept[-1]=x
        else: kept.append(x)
    ft=FT[fps]
    out=[]
    for i,x in enumerate(kept,1):
        out.append({'id':f'b{i}','kind':x['kind'],'frame_in':x['frame_in'],'frame_out':x['frame_out'],'start_ticks':x['frame_in']*ft,'end_ticks':x['frame_out']*ft,'confidence':round(x['confidence'],4)})
    return out
def z(n,v=0.0):return [v]*n
V=[]
def add(name,single,many,luma,**kw):
    V.append({'id':name,'input':{'single':single,'many':many,'luma':luma,'params':kw},'expect':postprocess(single,many,luma,**kw)})
N=40;L=lambda:[100.0]*N
# CD-01 one clean hard cut: prob at idx 19 -> boundary frame 20
s=z(N);s[19]=0.97;add('CD-01',s,z(N),L())
# CD-02 below threshold at sens3 (0.5): 0.45 -> none; same at sens5 (0.3) -> found
s=z(N);s[19]=0.45;add('CD-02',s,z(N),L());add('CD-03',s,z(N),L(),sens=5)
# CD-04 adjacent run picks argmax
s=z(N);s[19]=0.6;s[20]=0.9;s[21]=0.7;add('CD-04',s,z(N),L())
# CD-05 min shot merge: two cuts 4 frames apart keep higher
s=z(N);s[10]=0.7;s[14]=0.95;add('CD-05',s,z(N),L())
# CD-06 two cuts exactly min_shot apart both kept
s=z(N);s[10]=0.7;s[18]=0.95;add('CD-06',s,z(N),L())
# CD-07 dissolve: many run 10..15 with a cut peak inside removed
s=z(N);s[12]=0.8;m=z(N)
for i in range(10,16):m[i]=0.7+0.02*(i-10)
add('CD-07',s,m,L())
# CD-08 short many-run (len 2) ignored
m=z(N);m[10]=0.9;m[11]=0.9;add('CD-08',z(N),m,L())
# CD-09 fade out: luma ramps 160,140,120,100,80,60 then dark 10,10,10
lu=[100.0]*N
for k,v in enumerate([160,140,120,100,80,60]):lu[10+k]=float(v)
for k in range(16,22):lu[k]=10.0
for k in range(22,N):lu[k]=100.0
add('CD-09',z(N),z(N),lu)
# CD-10 fade in
lu=[100.0]*N
for k in range(10,16):lu[k]=10.0
for k,v in enumerate([40,60,80,100,120,140]):lu[16+k]=float(v)
add('CD-10',z(N),z(N),lu)
# CD-11 flash at 20: luma spike; model fires cut at 19/20 -> suppressed
lu=[100.0]*N;lu[20]=230.0;s=z(N);s[19]=0.9;add('CD-11',s,z(N),lu)
add('CD-12',s,z(N),lu,include_flash=True)
# CD-13 hard_only ignores dissolve and fades
lu9=[100.0]*N
for k,v in enumerate([160,140,120,100,80,60]):lu9[10+k]=float(v)
for k in range(16,22):lu9[k]=10.0
s13=z(N);s13[30]=0.9;m13=z(N)
for i in range(5,9):m13[i]=0.9
add('CD-13',s13,m13,lu9,mode='hard_only')
# CD-14 empty
add('CD-14',z(N),z(N),L())
lm=[float(x) for x in open('../fx/luma_fades.txt').read().split()]
add('CD-15',z(len(lm)),z(len(lm)),lm)
json.dump({'tps':TPS,'fps':'25/1','frame_ticks':FT['25/1'],'thresholds':THR,'cases':V},open('cuts.json','w'),indent=1)
for v in V:print(v['id'],[(b['kind'],b['frame_in'],b['frame_out'],b['confidence']) for b in v['expect']])
