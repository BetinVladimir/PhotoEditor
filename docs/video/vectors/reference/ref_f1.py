import json
HARD={'cut'};GRAD={'dissolve','fade_in','fade_out'}
def iou(a,b):
    lo=max(a[0],b[0]);hi=min(a[1],b[1]);inter=max(0,hi-lo);un=max(a[1],b[1])-min(a[0],b[0]);return inter/un if un>0 else 0
def score(pred,gt):
    res={}
    tp=fp=fn=0
    for group,kinds in (('hard',HARD),('gradual',GRAD)):
        P=[p for p in pred if p['kind'] in kinds];G=[g for g in gt if g['kind'] in kinds]
        pairs=[]
        for i,p in enumerate(P):
            for j,g in enumerate(G):
                if group=='hard':
                    d=abs(p['frame_in']-g['frame_in'])
                    if d<=2: pairs.append((d,i,j))
                else:
                    v=iou((p['frame_in'],p['frame_out']),(g['frame_in'],g['frame_out']))
                    if v>=0.3: pairs.append((1-v,i,j))
        pairs.sort();up=set();ug=set();m=0
        for d,i,j in pairs:
            if i in up or j in ug: continue
            up.add(i);ug.add(j);m+=1
        tp+=m;fp+=len(P)-m;fn+=len(G)-m
    return tp,fp,fn
def prf(tp,fp,fn):
    if tp+fp+fn==0:return 1.0,1.0,1.0
    p=tp/(tp+fp) if tp+fp else 0.0;r=tp/(tp+fn) if tp+fn else 0.0
    f=2*p*r/(p+r) if p+r else 0.0;return round(p,4),round(r,4),round(f,4)
cases=[]
def c(name,pred,gt):
    tp,fp,fn=score(pred,gt);p,r,f=prf(tp,fp,fn);cases.append({'id':name,'pred':pred,'gt':gt,'expect':{'tp':tp,'fp':fp,'fn':fn,'precision':p,'recall':r,'f1':f}})
k=lambda kind,a,b=None:{'kind':kind,'frame_in':a,'frame_out':a if b is None else b}
c('F1-01',[k('cut',50),k('cut',100)],[k('cut',50),k('cut',100)])
c('F1-02',[k('cut',52)],[k('cut',50)])
c('F1-03',[k('cut',53)],[k('cut',50)])
c('F1-04',[],[])
c('F1-05',[k('cut',50)],[])
c('F1-06',[],[k('cut',50)])
c('F1-07',[k('cut',50),k('cut',51)],[k('cut',50)])
c('F1-08',[k('dissolve',100,110)],[k('dissolve',104,114)])
c('F1-09',[k('dissolve',100,110)],[k('dissolve',108,118)])
c('F1-10',[k('cut',50),k('dissolve',100,110),k('fade_out',200,210),k('flash',30)],[k('cut',50),k('dissolve',101,111),k('cut',150)])
json.dump(cases,open('f1.json','w'),indent=1)
for x in cases:print(x['id'],x['expect'])
