import json, copy
from math import gcd
TPS=705_600_000
FT=28_224_000  # 25 fps
def S(x): return int(round(x*TPS))
class E(Exception): pass
def dur(c):
    if c['source']['kind']=='photo': return c['duration']
    sp=c.get('speed',{'num':1,'den':1})
    return ((c['src_out']-c['src_in'])*sp['den'])//sp['num']
def find(doc,cid):
    for t in doc['tracks']:
        for i,c in enumerate(t['clips']):
            if c['id']==cid: return t,i,c
    raise E('CLIP_NOT_FOUND')
def split_clip(doc,cid,at,new_id):
    doc=copy.deepcopy(doc);t,i,c=find(doc,cid)
    if c['source']['kind']=='photo': raise E('SPLIT_PHOTO_UNSUPPORTED')
    d=dur(c)
    if not (c['at']<at<c['at']+d): raise E('SPLIT_OUT_OF_RANGE')
    if at%FT: raise E('SPLIT_NOT_ALIGNED')
    sp=c.get('speed',{'num':1,'den':1});off=at-c['at']
    if (off*sp['num'])%sp['den']: raise E('SPLIT_NOT_ALIGNED')
    so=off*sp['num']//sp['den']
    b=copy.deepcopy(c);b['id']=new_id;b['at']=at;b['src_in']=c['src_in']+so
    c['src_out']=c['src_in']+so
    b.pop('transition_in',None)
    if 'audio' in c: c['audio']['fade_out']=0; b['audio']['fade_in']=0
    for k,e in enumerate(b.get('effects',[]),1): e['id']=f"{new_id}-e{k}"
    t['clips'].insert(i+1,b);return doc
def trim_edge(doc,cid,edge,pos,src_dur):
    doc=copy.deepcopy(doc);t,i,c=find(doc,cid)
    if pos%FT: raise E('TRIM_NOT_ALIGNED')
    sp=c.get('speed',{'num':1,'den':1});end=c['at']+dur(c)
    if edge=='in':
        prev_end=0 if i==0 else t['clips'][i-1]['at']+dur(t['clips'][i-1])
        if pos<prev_end: raise E('TRIM_OVERLAP')
        if pos>=end: raise E('TRIM_EMPTY')
        delta=pos-c['at']
        if (delta*sp['num'])%sp['den']: raise E('TRIM_NOT_ALIGNED')
        ns=c['src_in']+delta*sp['num']//sp['den']
        if ns<0: raise E('TRIM_BEFORE_SOURCE')
        c['src_in']=ns;c['at']=pos
    else:
        if pos<=c['at']: raise E('TRIM_EMPTY')
        if i+1<len(t['clips']) and pos>t['clips'][i+1]['at']: raise E('TRIM_OVERLAP')
        if (( pos-c['at'])*sp['num'])%sp['den']: raise E('TRIM_NOT_ALIGNED')
        no=c['src_in']+(pos-c['at'])*sp['num']//sp['den']
        if no>src_dur: raise E('TRIM_BEYOND_SOURCE')
        c['src_out']=no
    return doc
def move_clip(doc,cid,new_at,track_id=None):
    doc=copy.deepcopy(doc);t,i,c=find(doc,cid)
    if new_at<0 or new_at%FT: raise E('MOVE_NOT_ALIGNED')
    tgt=t if track_id is None else next((x for x in doc['tracks'] if x['id']==track_id),None)
    if tgt is None: raise E('TRACK_NOT_FOUND')
    kind='audio' if c['source']['kind']=='audio' else 'video'
    if tgt['kind']!=kind: raise E('MOVE_TRACK_KIND')
    t['clips'].pop(i);d=dur(c)
    for o in tgt['clips']:
        if new_at<o['at']+dur(o) and o['at']<new_at+d: raise E('MOVE_OVERLAP')
    c['at']=new_at;tgt['clips'].append(c);tgt['clips'].sort(key=lambda x:x['at']);return doc
def delete_clip(doc,cid,ripple):
    doc=copy.deepcopy(doc);t,i,c=find(doc,cid);d=dur(c);t['clips'].pop(i)
    if ripple:
        for o in t['clips']:
            if o['at']>c['at']: o['at']-=d
    return doc
def set_speed(doc,cid,num,den):
    doc=copy.deepcopy(doc);t,i,c=find(doc,cid)
    if c['source']['kind']=='photo': raise E('SPEED_PHOTO')
    if num<=0 or den<=0 or num*1<den*0.25-1e-12 or Fraction_gt(num,den): raise E('SPEED_RANGE')
    g=gcd(num,den);num//=g;den//=g
    raw=(c['src_out']-c['src_in'])*den//num;d=(raw//FT)*FT
    if d<=0: raise E('SPEED_TOO_SHORT')
    if i+1<len(t['clips']) and c['at']+d>t['clips'][i+1]['at']: raise E('SPEED_OVERLAP')
    c['src_out']=c['src_in']+d*num//den;c['speed']={'num':num,'den':den};return doc
def Fraction_gt(n,d): return n>4*d
def apply_cuts(doc,media_id,boundaries,accepted,mode,new_ids):
    doc=copy.deepcopy(doc);it=iter(new_ids);applied=[]
    bs=sorted([b for b in boundaries if b['id'] in accepted],key=lambda b:-b['start_ticks'])
    for b in bs:
        ts=b['start_ticks'] if mode=='split_at_start' else (b['start_ticks']+b['end_ticks'])//2
        for t in doc['tracks']:
            if t['kind']!='video': continue
            for c in list(t['clips']):
                if c['source'].get('media_id')!=media_id: continue
                if not (c['src_in']<ts<c['src_out']): continue
                sp=c.get('speed',{'num':1,'den':1})
                tl=c['at']+(ts-c['src_in'])*sp['den']//sp['num']
                tl=((tl+FT//2)//FT)*FT
                if not (c['at']<tl<c['at']+dur(c)): continue
                nid=next(it);doc=split_clip(doc,c['id'],tl,nid);applied.append({'boundary':b['id'],'clip':c['id'],'new':nid,'at':tl})
    return doc,applied
def base():
    return {'video':{'fps':{'num':25,'den':1}},'tracks':[
      {'id':'V1','kind':'video','clips':[
        {'id':'c1','source':{'kind':'video','media_id':'m1'},'at':0,'src_in':0,'src_out':S(10),'speed':{'num':1,'den':1},'effects':[{'id':'e1','type':'color.basic','enabled':True,'params':{}}],'transition_in':{'kind':'crossfade','duration':S(1),'align':'center'}},
        {'id':'c2','source':{'kind':'video','media_id':'m1'},'at':S(10),'src_in':S(5),'src_out':S(9),'speed':{'num':1,'den':1},'effects':[]}]},
      {'id':'A1','kind':'audio','clips':[]}]}
SRC=S(20)
V={}
def rec(name,fn,*a,**k):
    try: r=fn(*a,**k);return {'case':name,'result':r}
    except E as e: return {'case':name,'error':str(e)}
b=base();V['base']=b
cases=[]
def case(name,op,args,fn):
    try: out=fn(); cases.append({'id':name,'op':op,'args':args,'expect':{'doc':out}})
    except E as e: cases.append({'id':name,'op':op,'args':args,'expect':{'error':str(e)}})
case('SP-01','split_clip',{'clip':'c1','at':S(4),'new_id':'c1b'},lambda:split_clip(b,'c1',S(4),'c1b'))
case('SP-02','split_clip',{'clip':'c1','at':0,'new_id':'x'},lambda:split_clip(b,'c1',0,'x'))
case('SP-03','split_clip',{'clip':'c1','at':S(10),'new_id':'x'},lambda:split_clip(b,'c1',S(10),'x'))
case('SP-04','split_clip',{'clip':'c1','at':S(4)+1,'new_id':'x'},lambda:split_clip(b,'c1',S(4)+1,'x'))
case('SP-05','split_clip',{'clip':'nope','at':S(4),'new_id':'x'},lambda:split_clip(b,'nope',S(4),'x'))
b2=set_speed(base(),'c2',2,1)  # c2: 4s src -> 2s
case('SP-06','split_clip(after set_speed c2 2/1)',{'clip':'c2','at':S(11),'new_id':'c2b'},lambda:split_clip(b2,'c2',S(11),'c2b'))
case('TR-01','trim_edge',{'clip':'c1','edge':'out','pos':S(8),'source_duration':SRC},lambda:trim_edge(b,'c1','out',S(8),SRC))
case('TR-02','trim_edge',{'clip':'c1','edge':'in','pos':S(2),'source_duration':SRC},lambda:trim_edge(b,'c1','in',S(2),SRC))
case('TR-03','trim_edge',{'clip':'c1','edge':'out','pos':S(11),'source_duration':SRC},lambda:trim_edge(b,'c1','out',S(11),SRC))
case('TR-04','trim_edge',{'clip':'c2','edge':'out','pos':S(21),'source_duration':S(9)},lambda:trim_edge(b,'c2','out',S(21),S(9)))
case('TR-05','trim_edge',{'clip':'c2','edge':'in','pos':S(9),'source_duration':SRC},lambda:trim_edge(b,'c2','in',S(9),SRC))
case('TR-06','trim_edge',{'clip':'c1','edge':'out','pos':0,'source_duration':SRC},lambda:trim_edge(b,'c1','out',0,SRC))
case('TR-07','trim_edge',{'clip':'c1','edge':'out','pos':S(8)+1,'source_duration':SRC},lambda:trim_edge(b,'c1','out',S(8)+1,SRC))
case('MV-01','move_clip',{'clip':'c2','new_at':S(12)},lambda:move_clip(b,'c2',S(12)))
case('MV-02','move_clip',{'clip':'c2','new_at':S(9)},lambda:move_clip(b,'c2',S(9)))
case('MV-03','move_clip',{'clip':'c2','new_at':S(12),'track':'A1'},lambda:move_clip(b,'c2',S(12),'A1'))
case('MV-04','move_clip',{'clip':'c2','new_at':-FT},lambda:move_clip(b,'c2',-FT))
case('DL-01','delete_clip',{'clip':'c1','ripple':False},lambda:delete_clip(b,'c1',False))
case('DL-02','delete_clip',{'clip':'c1','ripple':True},lambda:delete_clip(b,'c1',True))
case('SS-01','set_speed',{'clip':'c2','num':2,'den':1},lambda:set_speed(b,'c2',2,1))
case('SS-02','set_speed',{'clip':'c1','num':1,'den':3},lambda:set_speed(b,'c1',1,3))
case('SS-03','set_speed',{'clip':'c1','num':5,'den':1},lambda:set_speed(b,'c1',5,1))
case('SS-04','set_speed',{'clip':'c2','num':4,'den':2},lambda:set_speed(b,'c2',4,2))
case('SS-05','set_speed',{'clip':'c2','num':1,'den':5},lambda:set_speed(b,'c2',1,5))
bounds=[{'id':'b1','kind':'cut','start_ticks':S(3),'end_ticks':S(3)},
        {'id':'b2','kind':'dissolve','start_ticks':S(6),'end_ticks':S(7)},
        {'id':'b3','kind':'cut','start_ticks':S(12),'end_ticks':S(12)}]  # 12 not in c1 src(0..10); in c2 src(5..9)? no
for mode in ('split_at_start','split_at_middle'):
    try:
        d,ap=apply_cuts(b,'m1',bounds,['b1','b2'],mode,['n1','n2','n3'])
        cases.append({'id':'AC-'+('01' if mode=='split_at_start' else '02'),'op':'apply_cuts','args':{'media_id':'m1','boundaries':bounds,'accepted':['b1','b2'],'mode':mode,'new_ids':['n1','n2','n3']},'expect':{'doc':d,'applied':ap}})
    except E as e: print(e)
d1,_=apply_cuts(b,'m1',bounds,['b1','b2'],'split_at_start',['n1','n2','n3'])
d2,ap2=apply_cuts(d1,'m1',bounds,['b1','b2'],'split_at_start',['n4','n5'])
cases.append({'id':'AC-03','op':'apply_cuts (repeat on result of AC-01)','args':{'media_id':'m1','boundaries':bounds,'accepted':['b1','b2'],'mode':'split_at_start','new_ids':['n4','n5']},'expect':{'doc':d2,'applied':ap2}})
d3,ap3=apply_cuts(b,'m1',bounds,['b3'],'split_at_start',['n1'])
cases.append({'id':'AC-04','op':'apply_cuts','args':{'media_id':'m1','boundaries':bounds,'accepted':['b3'],'mode':'split_at_start','new_ids':['n1']},'expect':{'doc':d3,'applied':ap3}})
V['cases']=cases;V['source_duration_ticks']={'m1':SRC}
json.dump(V,open('commands.json','w'),indent=1)
for c in cases: print(c['id'],('ERR '+c['expect']['error']) if 'error' in c['expect'] else 'ok', c['expect'].get('applied',''))
