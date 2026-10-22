import json, copy, sys
sys.path.insert(0,'.')
TPS=705_600_000; FT=28_224_000
def S(x): return int(round(x*TPS))
SUPPORTED={(24,1),(25,1),(30,1),(50,1),(60,1),(24000,1001),(30000,1001),(60000,1001)}
# ---------------- validate ----------------
def dur(c):
    if c['source']['kind']=='photo': return c['duration']
    sp=c.get('speed',{'num':1,'den':1}); return ((c['src_out']-c['src_in'])*sp['den'])//sp['num']
ID=__import__('re').compile(r'^[A-Za-z0-9_-]{1,64}$')
TOP={'schema_version','sequence_id','base_revision','timebase','video','audio','tracks','markers','master','rights'}
def is_int(x): return isinstance(x,int) and not isinstance(x,bool)
RANGES={'color.basic':{'exposure_ev':(-5,5),'temperature':(-100,100),'tint':(-100,100),'contrast':(-100,100),'highlights':(-100,100),'shadows':(-100,100),'whites':(-100,100),'blacks':(-100,100),'saturation':(-100,100),'vibrance':(-100,100)}}
def validate(doc,ctx):
    iss=[]
    def add(rule,ptr,code): iss.append({'rule':rule,'pointer':ptr,'code':code})
    # VT-01
    for k in doc:
        if k not in TOP: add('VT-01','/'+k,'DOC_SCHEMA')
    if doc.get('schema_version')!=1: add('VT-01','/schema_version','DOC_SCHEMA')
    f=doc['video']['fps']
    if (f['num'],f['den']) not in SUPPORTED: add('VT-01','/video/fps','DOC_SCHEMA')
    for ti,t in enumerate(doc['tracks']):
        for ci,c in enumerate(t['clips']):
            for key in ('at','src_in','src_out','duration'):
                if key in c and not is_int(c[key]): add('VT-01',f'/tracks/{ti}/clips/{ci}/{key}','DOC_SCHEMA')
    if iss: return iss
    # VT-02
    nv=sum(1 for t in doc['tracks'] if t['kind']=='video'); na=sum(1 for t in doc['tracks'] if t['kind']=='audio')
    nc=sum(len(t['clips']) for t in doc['tracks'])
    if nv>4 or na>4: add('VT-02','/tracks','DOC_LIMIT')
    if nc>2000: add('VT-02','/tracks','DOC_LIMIT')
    end=max([c['at']+dur(c) for t in doc['tracks'] for c in t['clips']] or [0])
    if end>7_620_480_000_000: add('VT-02','/tracks','DOC_LIMIT')
    # VT-03
    seen=set()
    def chk(i,ptr):
        if not isinstance(i,str) or not ID.match(i) or i in seen: add('VT-03',ptr,'DOC_ID_DUPLICATE')
        seen.add(i)
    for ti,t in enumerate(doc['tracks']):
        chk(t['id'],f'/tracks/{ti}/id')
        for ci,c in enumerate(t['clips']):
            chk(c['id'],f'/tracks/{ti}/clips/{ci}/id')
            for ei,e in enumerate(c.get('effects',[])): chk(e['id'],f'/tracks/{ti}/clips/{ci}/effects/{ei}/id')
    for mi,m in enumerate(doc['markers']): chk(m['id'],f'/markers/{mi}/id')
    # VT-04
    for ti,t in enumerate(doc['tracks']):
        for ci in range(1,len(t['clips'])):
            a,b=t['clips'][ci-1],t['clips'][ci]
            if b['at']<a['at'] or a['at']+dur(a)>b['at']: add('VT-04',f'/tracks/{ti}/clips/{ci}/at','CLIP_OVERLAP')
    # VT-05
    for ti,t in enumerate(doc['tracks']):
        for ci,c in enumerate(t['clips']):
            if c['source']['kind']=='photo': continue
            p=f'/tracks/{ti}/clips/{ci}'
            if c['src_in']<0: add('VT-05',p+'/src_in','CLIP_RANGE')
            elif c['src_out']<=c['src_in']: add('VT-05',p+'/src_out','CLIP_RANGE')
            else:
                d=ctx['sourceDurations'].get(c['source'].get('media_id'))
                if d is not None and c['src_out']>d: add('VT-05',p+'/src_out','CLIP_RANGE')
    # VT-06
    for ti,t in enumerate(doc['tracks']):
        for ci,c in enumerate(t['clips']):
            sid=c['source'].get('media_id') or c['source'].get('asset_version_id')
            p=f'/tracks/{ti}/clips/{ci}/source'
            if ctx['sourceStates'].get(sid)!='ready': add('VT-06',p,'SOURCE_NOT_READY')
            elif ctx['sourceHashes'].get(sid)!=c['source']['sha256']: add('VT-06',p,'SOURCE_HASH_MISMATCH')
    # VT-07
    for ti,t in enumerate(doc['tracks']):
        for ci,c in enumerate(t['clips']):
            for ei,e in enumerate(c.get('effects',[])):
                p=f'/tracks/{ti}/clips/{ci}/effects/{ei}/params'
                if e['type'] in RANGES:
                    for k,v in e['params'].items():
                        lo,hi=RANGES[e['type']].get(k,(None,None))
                        if lo is None or not isinstance(v,(int,float)) or v<lo or v>hi: add('VT-07',p+'/'+k,'EFFECT_PARAM')
                if e['type']=='color.curves':
                    for k,pts in e['params'].items():
                        xs=[q[0] for q in pts]
                        if not (2<=len(pts)<=16) or any(b<=a for a,b in zip(xs,xs[1:])) or any(not 0<=x<=1 for x in xs): add('VT-07',p+'/'+k,'EFFECT_PARAM')
    # VT-08
    for ti,t in enumerate(doc['tracks']):
        for ci,c in enumerate(t['clips']):
            tr=c.get('transition_in')
            if not tr: continue
            p=f'/tracks/{ti}/clips/{ci}/transition_in'
            if ci==0 and tr['kind']!='fade_in' and tr['kind']!='cut': add('VT-08',p+'/kind','TRANSITION_INVALID'); continue
            lim=dur(c)//2 if ci==0 else min(dur(c),dur(t['clips'][ci-1]))//2
            if tr.get('duration',0)>lim: add('VT-08',p+'/duration','TRANSITION_INVALID')
    # VT-09
    ids={t['id']:t for t in doc['tracks']}
    for ti,t in enumerate(doc['tracks']):
        for ci,c in enumerate(t['clips']):
            for ei,e in enumerate(c.get('effects',[])):
                if e['type']=='audio.duck':
                    for s in e['params'].get('sidechain_tracks',[]):
                        if s not in ids or ids[s]['kind']!='audio' or s==t['id']:
                            add('VT-09',f'/tracks/{ti}/clips/{ci}/effects/{ei}/params/sidechain_tracks','SIDECHAIN_INVALID'); break
    # VT-10
    if ctx.get('forRender') and ctx.get('musicClipIds') and doc['rights'].get('music_declared') is not True: add('VT-10','/rights/music_declared','RIGHTS_REQUIRED')
    return iss
H1='a'*64;H2='b'*64
def base_doc():
    return {'schema_version':1,'sequence_id':'2f6c0000-0000-4000-8000-000000000001','base_revision':1,'timebase':{'ticks_per_second':TPS},
     'video':{'fps':{'num':25,'den':1},'width':1280,'height':720,'color':{'space':'rec709','range':'limited'},'background':'#000000'},
     'audio':{'sample_rate':48000,'channels':2},
     'tracks':[{'id':'V1','kind':'video','clips':[
        {'id':'c1','source':{'kind':'video','media_id':'m1','sha256':H1},'at':0,'src_in':0,'src_out':S(4),'speed':{'num':1,'den':1},'effects':[]},
        {'id':'c2','source':{'kind':'video','media_id':'m1','sha256':H1},'at':S(4),'src_in':S(4),'src_out':S(8),'speed':{'num':1,'den':1},'effects':[]}]},
       {'id':'A1','kind':'audio','clips':[
        {'id':'a1','source':{'kind':'audio','media_id':'m2','sha256':H2},'at':0,'src_in':0,'src_out':S(8),'audio':{'gain_db':0.0,'fade_in':0,'fade_out':0,'mute':False},'effects':[]}]}],
     'markers':[],'master':{'effects':[],'audio':{'loudness':{'target_lufs':-14.0,'true_peak_db':-1.0}}},'rights':{'music_declared':True}}
def base_ctx():
    return {'sourceDurations':{'m1':S(20),'m2':S(20)},'sourceStates':{'m1':'ready','m2':'ready'},'sourceHashes':{'m1':H1,'m2':H2},'musicClipIds':['a1'],'forRender':False}
def setp(doc,path,val):
    cur=doc;parts=[p for p in path.split('/') if p!='']
    for p in parts[:-1]: cur=cur[int(p)] if isinstance(cur,list) else cur[p]
    last=parts[-1]
    if isinstance(cur,list): cur[int(last)]=val
    else: cur[last]=val
def appendp(doc,path,val):
    cur=doc
    for p in [p for p in path.split('/') if p!='']: cur=cur[int(p)] if isinstance(cur,list) else cur[p]
    cur.append(val)
C=[]
def case(i,desc,patches=(),ctxp=None):
    d=base_doc();x=base_ctx()
    for op in patches:
        (setp if op['op']=='set' else appendp)(d,op['path'],op['value'])
    for k,v in (ctxp or {}).items(): x[k]=v
    C.append({'id':f'TC-T-VAL-{i:02d}','desc':desc,'patches':list(patches),'ctx_patch':ctxp or {},'expect':validate(d,x)})
c1=lambda k,v:{'op':'set','path':'/tracks/0/clips/0/'+k,'value':v}
case(0,'base valid')
case(1,'unknown root key',[{'op':'set','path':'/extra','value':1}])
case(2,'non-integer at',[c1('at',1.5)])
case(3,'schema_version 2',[{'op':'set','path':'/schema_version','value':2}])
case(4,'unsupported fps 7/1',[{'op':'set','path':'/video/fps','value':{'num':7,'den':1}}])
case(5,'five video tracks',[{'op':'append','path':'/tracks','value':{'id':f'V{n}','kind':'video','clips':[]}} for n in (2,3,4,5)])
case(6,'duplicate clip id',[{'op':'set','path':'/tracks/0/clips/1/id','value':'c1'}])
case(7,'bad id pattern',[c1('id','bad id!')])
case(8,'overlap',[{'op':'set','path':'/tracks/0/clips/1/at','value':S(3)}])
case(9,'unsorted clips',[{'op':'set','path':'/tracks/0/clips','value':[base_doc()['tracks'][0]['clips'][1],base_doc()['tracks'][0]['clips'][0]]}])
case(10,'src_out equals src_in',[c1('src_out',0)])
case(11,'src_out beyond source',[{'op':'set','path':'/tracks/0/clips/1/src_out','value':S(25)}])
case(12,'source m1 not ready',[],{'sourceStates':{'m1':'other','m2':'ready'}})
case(13,'hash mismatch m2',[],{'sourceHashes':{'m1':H1,'m2':'c'*64}})
case(14,'exposure out of range',[c1('effects',[{'id':'e1','type':'color.basic','enabled':True,'params':{'exposure_ev':6}}])])
case(15,'curve x not increasing',[c1('effects',[{'id':'e1','type':'color.curves','enabled':True,'params':{'master':[[0,0],[0.5,0.5],[0.4,0.6],[1,1]]}}])])
case(16,'transition longer than half',[{'op':'set','path':'/tracks/0/clips/1/transition_in','value':{'kind':'crossfade','duration':S(3),'align':'center'}}])
case(17,'first clip crossfade',[c1('transition_in',{'kind':'crossfade','duration':S(1),'align':'center'})])
case(18,'sidechain missing track',[{'op':'set','path':'/tracks/1/clips/0/effects','value':[{'id':'e9','type':'audio.duck','enabled':True,'params':{'sidechain_tracks':['A9'],'threshold_db':-30,'depth_db':12,'attack_ms':30,'release_ms':400}}]}])
case(19,'music without rights on render',[{'op':'set','path':'/rights/music_declared','value':False}],{'forRender':True})
case(20,'rights declared on render ok',[],{'forRender':True})
json.dump({'schema':'validate_cases/1','tps':TPS,'base_doc':base_doc(),'base_ctx':base_ctx(),'issue_order':'by rule VT-01..VT-10, within rule by document order; if any VT-01 issue exists only VT-01 issues are returned','cases':C},open('validate_cases.json','w'),indent=1,ensure_ascii=False)
for c in C: print(c['id'],c['desc'],[(x['rule'],x['pointer']) for x in c['expect']])
