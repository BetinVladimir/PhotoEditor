import json, copy, sys, io, contextlib
sys.path.insert(0,'.')
with contextlib.redirect_stdout(io.StringIO()):
    import ref_cmds as R
TPS=R.TPS; FT=R.FT; S=R.S
# ---- markers_only
def apply_markers(doc,media_id,boundaries,accepted,mode,new_ids):
    doc=copy.deepcopy(doc);it=iter(new_ids);applied=[]
    bs=sorted([b for b in boundaries if b['id'] in accepted],key=lambda b:-b['start_ticks'])
    doc.setdefault('markers',[])
    for b in bs:
        ts=b['start_ticks'] if mode in ('split_at_start','markers_only') else (b['start_ticks']+b['end_ticks'])//2
        for t in doc['tracks']:
            if t['kind']!='video': continue
            for c in list(t['clips']):
                if c['source'].get('media_id')!=media_id: continue
                if not (c['src_in']<ts<c['src_out']): continue
                sp=c.get('speed',{'num':1,'den':1})
                tl=c['at']+(ts-c['src_in'])*sp['den']//sp['num']
                tl=((tl+FT//2)//FT)*FT
                if not (c['at']<tl<c['at']+R.dur(c)): continue
                if any(m['at']==tl and m['kind']=='cut' for m in doc['markers']): continue
                nid=next(it);doc['markers'].append({'id':nid,'at':tl,'kind':'cut','label':''});applied.append({'boundary':b['id'],'clip':c['id'],'new':nid,'at':tl})
    doc['markers'].sort(key=lambda m:m['at'])
    return doc,applied
b=R.base();b['markers']=[]
bounds=[{'id':'b1','kind':'cut','start_ticks':S(3),'end_ticks':S(3)},{'id':'b2','kind':'dissolve','start_ticks':S(6),'end_ticks':S(7)},{'id':'b3','kind':'cut','start_ticks':S(12),'end_ticks':S(12)}]
d1,a1=apply_markers(b,'m1',bounds,['b1','b2'],'markers_only',['n1','n2','n3'])
d2,a2=apply_markers(d1,'m1',bounds,['b1','b2'],'markers_only',['n4','n5'])
d3,a3=apply_markers(b,'m1',bounds,['b2'],'markers_only',['n1','n2'])
cases=[{'id':'TC-T-AC-M1','args':{'media_id':'m1','boundaries':bounds,'accepted':['b1','b2'],'mode':'markers_only','new_ids':['n1','n2','n3']},'expect':{'doc':d1,'applied':a1}},
 {'id':'TC-T-AC-M2','note':'repeat on result of M1: nothing applied, ids not consumed','input':'result of TC-T-AC-M1','args':{'media_id':'m1','boundaries':bounds,'accepted':['b1','b2'],'mode':'markers_only','new_ids':['n4','n5']},'expect':{'doc':d2,'applied':a2}},
 {'id':'TC-T-AC-M3','args':{'media_id':'m1','boundaries':bounds,'accepted':['b2'],'mode':'markers_only','new_ids':['n1','n2']},'expect':{'doc':d3,'applied':a3}}]
# ids exhausted
try: R.apply_cuts(b,'m1',bounds,['b1','b2'],'split_at_start',['n1']); exh='no error'
except StopIteration: exh='APPLY_IDS_EXHAUSTED'
cases.append({'id':'TC-T-AC-M4','note':'split mode with too few ids','args':{'media_id':'m1','boundaries':bounds,'accepted':['b1','b2'],'mode':'split_at_start','new_ids':['n1']},'expect':{'error':'APPLY_IDS_EXHAUSTED'}})
json.dump({'schema':'commands_extra/1','base':b,'cases':cases},open('commands_extra.json','w'),indent=1)
for c in cases: print(c['id'],c['expect'].get('applied',c['expect']))
# ---- snap
def snap(cand,anchors,thr,ft=FT):
    best=None
    for a in sorted(anchors):
        dd=abs(a-cand)
        if dd<=thr and (best is None or dd<best[0]): best=(dd,a)
    return best[1] if best else ((cand+ft//2)//ft)*ft
SN=[]
def sc(i,cand,anch,thr,desc):
    SN.append({'id':f'TC-T-SNAP-{i:02d}','desc':desc,'candidate':cand,'anchors':anch,'threshold':thr,'frame_ticks':FT,'expect':snap(cand,anch,thr)})
sc(1,1000000000,[1010000000],100000000,'anchor within threshold')
sc(2,1000000000,[900000000,1100000000],150000000,'tie -> smaller anchor')
sc(3,1000000000,[2000000000],100000000,'no anchor -> round to frame grid')
sc(4,987840000,[987840000],100000000,'anchor equals candidate')
sc(5,1000000000,[1050000000,1020000000],100000000,'closer of two anchors')
sc(6,1000000000,[1000000001],0,'threshold 0, anchor 1 tick away -> grid')
json.dump({'schema':'snap_cases/1','cases':SN},open('snap_cases.json','w'),indent=1)
for s in SN: print(s['id'],s['expect'])
# ---- range
def parse_range(h,size):
    if h=='': return (200,0,size-1)
    if not h.startswith('bytes='): return (416,None,None)
    spec=h[6:]
    if ',' in spec or spec.count('-')!=1: return (416,None,None)
    a,b=spec.split('-')
    if size==0: return (416,None,None)
    if a=='':
        if not b.isdigit(): return (416,None,None)
        n=int(b)
        if n==0: return (416,None,None)
        return (206,max(0,size-n),size-1)
    if not a.isdigit() or (b!='' and not b.isdigit()): return (416,None,None)
    a=int(a)
    if a>=size: return (416,None,None)
    if b=='': return (206,a,size-1)
    b=int(b)
    if a>b: return (416,None,None)
    return (206,a,min(b,size-1))
RG=[]
for i,(h,sz) in enumerate([('',1000),('bytes=0-99',1000),('bytes=900-',1000),('bytes=-100',1000),('bytes=0-5000',1000),('bytes=1000-',1000),('bytes=-0',1000),('bytes=5-2',1000),('bytes=0-1,5-6',1000),('items=0-1',1000),('bytes=a-b',1000),('bytes=-5000',1000),('bytes=0-0',0)],1):
    st,s,e=parse_range(h,sz);RG.append({'id':f'TC-G-RNG-{i:02d}','header':h,'size':sz,'expect':{'status':st,'start':s,'end':e}})
json.dump({'schema':'range_cases/1','note':'status 200 = whole object, 206 = partial, 416 = error (Content-Range: bytes */size)','cases':RG},open('range_cases.json','w'),indent=1)
for r in RG: print(r['id'],r['header'],r['size'],r['expect'])
# ---- cube
CB=[{'id':'TC-L-CUBE-00','kind':'valid','gen':{'kind':'identity','size':17},'expect':{'ok':True,'size':17}}]
neg=[('TC-L-CUBE-01',{'kind':'identity','size':16},'size 16 below minimum 17'),
 ('TC-L-CUBE-02',{'kind':'header_only','size':66},'size 66 above maximum 65'),
 ('TC-L-CUBE-03',{'kind':'identity','size':17,'drop_rows':1},'one row missing'),
 ('TC-L-CUBE-04',{'kind':'identity','size':17,'replace_row':{'index':5,'text':'nan 0.5 0.5'}},'NaN value'),
 ('TC-L-CUBE-05',{'kind':'identity','size':17,'replace_row':{'index':5,'text':'1.5 0.5 0.5'}},'value 1.5 outside default domain [0;1]'),
 ('TC-L-CUBE-06',{'kind':'text','text':'TITLE "x"\nLUT_1D_SIZE 1024\n0 0 0\n'},'1D LUT not supported'),
 ('TC-L-CUBE-07',{'kind':'pad_comment','bytes':20971521},'file larger than 20 MiB'),
 ('TC-L-CUBE-08',{'kind':'text','text':'# '+'x'*300+'\nLUT_3D_SIZE 17\n'},'line longer than 256 characters'),
 ('TC-L-CUBE-09',{'kind':'text','text':''},'empty file'),
 ('TC-L-CUBE-10',{'kind':'text','text':'abc'},'not a cube file')]
for i,g,d in neg: CB.append({'id':i,'desc':d,'gen':g,'expect':{'ok':False,'error':'LUT_INVALID'}})
json.dump({'schema':'cube_cases/1','generators':{'identity':'LUT_3D_SIZE N then N^3 rows; row index k=r+N*g+N*N*b has value r/(N-1) g/(N-1) b/(N-1) formatted %.6f; drop_rows removes that many rows from the end; replace_row replaces the row at index','header_only':'only the line LUT_3D_SIZE N','pad_comment':'a comment line # followed by x characters so that the total file size equals bytes, then LUT_3D_SIZE 17','text':'literal'},'cases':CB},open('cube_cases.json','w'),indent=1)
