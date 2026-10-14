import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,Image,Pressable,SafeAreaView,ScrollView,Text,View} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {freezePreparedReviewDecision,freezeSavedPreparedRevoke,projectDustCandidateToCrop,type DustCrop,type PreparedReviewDecision,type PreparedReviewItem,type SavedPreparedReview} from './interchange-prepared-review';
import type {ImportPersonMaskEntry} from './interchange-person-mask-candidates';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

type Props={client:ApiClient;tenant:string;project:string;workflow:string};
type Choice='accepted'|'rejected';
const identity=(item:PreparedReviewItem)=>`${item.entry_id}:${item.target_version_id}:${item.kind}:${item.proposal.artifact_id}:${item.proposal.sha256}`;
const safeURL=(value:string)=>{const url=new URL(value);if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))throw new Error('PHOTO_PREVIEW_UNAVAILABLE');return url.toString();};

export function InterchangePreparedReviewScreen(props:Props){
 const scope=JSON.stringify([props.client.origin,props.tenant,props.project,props.workflow]);
 return <ScopedScreen key={scope} {...props}/>;
}

function ScopedScreen({client,tenant,project,workflow}:Props){
 const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [items,setItems]=useState<PreparedReviewItem[]|null>(null),[entries,setEntries]=useState<ImportPersonMaskEntry[]>([]);
 const [saved,setSaved]=useState<SavedPreparedReview[]|null>(null);
 const [after,setAfter]=useState(''),[history,setHistory]=useState<string[]>([]),[nextAfter,setNextAfter]=useState('');
 const [savedAfter,setSavedAfter]=useState(''),[savedHistory,setSavedHistory]=useState<string[]>([]),[savedNextAfter,setSavedNextAfter]=useState(''),[savedLoading,setSavedLoading]=useState(true);
 const [selected,setSelected]=useState(''),[choices,setChoices]=useState<Record<string,Choice>>({});
 const [photo,setPhoto]=useState(''),[photoReady,setPhotoReady]=useState(false),[busy,setBusy]=useState(false),[loading,setLoading]=useState(true);
 const [error,setError]=useState(false),[revision,setRevision]=useState(0);
 const mutation=useRef<AbortController|null>(null),attempt=useRef<{fingerprint:string;key:string;body:PreparedReviewDecision}|null>(null);
 const item=items?.find(row=>identity(row)===selected),entry=entries.find(row=>row.entry_id===item?.entry_id&&row.target_version_id===item.target_version_id);

 useEffect(()=>{
  const controller=new AbortController();setLoading(true);setError(false);setItems(null);setSelected('');setChoices({});
  void Promise.all([api.preparedImportReviewPage(project,workflow,after,controller.signal),api.importPersonMaskEntries(project,workflow,controller.signal)]).then(([page,adopted])=>{
   if(controller.signal.aborted)return;
   if(page.items.some(row=>!adopted.entries.some(entry=>entry.entry_id===row.entry_id&&entry.target_version_id===row.target_version_id)))throw new Error('INTERCHANGE_PREPARED_REVIEW_SCOPE');
   setItems(page.items);setNextAfter(page.next_after??'');setEntries(adopted.entries);setSelected('');setChoices({});attempt.current=null;
  }).catch(()=>{if(!controller.signal.aborted){setItems(null);setNextAfter('');setError(true);}}).finally(()=>{if(!controller.signal.aborted)setLoading(false);});
  return()=>controller.abort();
 },[api,project,workflow,revision,after]);
 useEffect(()=>{
  const controller=new AbortController();setSaved(null);setSavedLoading(true);
  void api.savedPreparedImportReviewPage(project,workflow,savedAfter,controller.signal).then(page=>{if(!controller.signal.aborted){setSaved(page.items);setSavedNextAfter(page.next_after??'');}}).catch(()=>{if(!controller.signal.aborted){setSavedNextAfter('');setError(true);}}).finally(()=>{if(!controller.signal.aborted)setSavedLoading(false);});
  return()=>controller.abort();
 },[api,project,workflow,revision,savedAfter]);

 useEffect(()=>{
  const controller=new AbortController();setPhoto('');setPhotoReady(false);
  if(!item||!entry)return()=>controller.abort();
  void api.mediaLinks(entry.asset_id,item.target_version_id,['preview1600','thumb640'],controller.signal).then(links=>{
   if(controller.signal.aborted)return;
   const canvas=item.kind==='defects'?item.document.canvas as {width:number;height:number}:null;
   const link=links.items.find(link=>link.asset_version_id===item.target_version_id&&['preview1600','thumb640'].includes(link.variant)&&['image/jpeg','image/png','image/webp'].includes(link.media_type)&&link.width>0&&link.height>0&&Date.parse(link.expires_at)>Date.now()+5000&&(!canvas||Math.abs(link.width/link.height-canvas.width/canvas.height)<0.01));
   if(!link)throw new Error('PHOTO_PREVIEW_UNAVAILABLE');setPhoto(safeURL(link.url));
  }).catch(()=>{if(!controller.signal.aborted)setError(true);});
  return()=>controller.abort();
 },[api,entry?.asset_id,item?.proposal.artifact_id,item?.target_version_id]);
 useEffect(()=>()=>mutation.current?.abort(),[]);

 function refresh(){setAfter('');setHistory([]);setNextAfter('');setSavedAfter('');setSavedHistory([]);setSavedNextAfter('');setRevision(value=>value+1);}
 function nextProposalPage(){if(!nextAfter||loading||busy)return;setLoading(true);setHistory(current=>[...current,after]);setAfter(nextAfter);}
 function previousProposalPage(){if(!history.length||loading||busy)return;setLoading(true);setAfter(history[history.length-1]);setHistory(current=>current.slice(0,-1));}
 function nextSavedPage(){if(!savedNextAfter||savedLoading||busy)return;setSavedLoading(true);setSavedHistory(current=>[...current,savedAfter]);setSavedAfter(savedNextAfter);}
 function previousSavedPage(){if(!savedHistory.length||savedLoading||busy)return;setSavedLoading(true);setSavedAfter(savedHistory[savedHistory.length-1]);setSavedHistory(current=>current.slice(0,-1));}

 function choose(id:string,value:Choice){
  if(busy)return;
  setChoices(current=>({...current,[id]:current[id]===value?undefined:value} as Record<string,Choice>));attempt.current=null;
 }
 async function decide(action:'grant'|'revoke',decision:'accept'|'reject'|'skip'='accept'){
  if(!item||!photoReady||mutation.current)return;
  const accepted=Object.entries(choices).filter(([,v])=>v==='accepted').map(([id])=>id);
  const rejected=Object.entries(choices).filter(([,v])=>v==='rejected').map(([id])=>id);
  if(action==='grant'&&decision==='accept'&&accepted.length+rejected.length===0)return;
  let body:PreparedReviewDecision;
  try{body=freezePreparedReviewDecision(item,action,decision,accepted,rejected);}catch{setError(true);return;}
  const fingerprint=JSON.stringify(body);
  if(attempt.current?.fingerprint!==fingerprint)attempt.current={fingerprint,key:crypto.randomUUID(),body};
  const controller=new AbortController();mutation.current=controller;setBusy(true);setError(false);
  try{await api.decidePreparedImportReview(project,workflow,attempt.current.body,attempt.current.key,controller.signal);if(!controller.signal.aborted)refresh();}
  catch{if(!controller.signal.aborted)setError(true);}
  finally{if(mutation.current===controller)mutation.current=null;setBusy(false);}
 }
 async function revokeSaved(row:SavedPreparedReview){
  if(mutation.current)return;
  let body:PreparedReviewDecision;
  try{body=freezeSavedPreparedRevoke(row);}catch{setError(true);return;}
  const fingerprint=JSON.stringify(body);
  if(attempt.current?.fingerprint!==fingerprint)attempt.current={fingerprint,key:crypto.randomUUID(),body};
  const controller=new AbortController();mutation.current=controller;setBusy(true);setError(false);
  try{await api.decidePreparedImportReview(project,workflow,attempt.current.body,attempt.current.key,controller.signal);if(!controller.signal.aborted)refresh();}
  catch{if(!controller.signal.aborted)setError(true);}
  finally{if(mutation.current===controller)mutation.current=null;setBusy(false);}
 }

 const canvas=item?.kind==='defects'?item.document.canvas as {width:number;height:number}:null;
 const dustCrop=item?.kind==='dust'?item.document.raw_crop as DustCrop:null;
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={{padding:20,gap:12}}>
  <Text style={s.title}>{t('interchange_prepared_title')}</Text>
  {loading?<ActivityIndicator accessibilityLabel={t('interchange_prepared_loading')}/>:null}
  {error?<Text style={s.error} accessibilityRole="alert">{t('interchange_prepared_error')}</Text>:null}
  {saved?.filter(row=>row.status==='granted').map(row=><View key={`saved:${row.entry_id}:${row.proposal.artifact_id}`} style={{gap:6}}><Text style={s.subtitle}>{row.kind==='defects'?t('interchange_prepared_defects'):t('interchange_prepared_dust')} · {row.entry_id.slice(0,8)} · {t('interchange_prepared_saved')}</Text><Pressable style={s.button} accessibilityRole="button" disabled={busy} onPress={()=>void revokeSaved(row)}><Text style={s.buttonText}>{t('interchange_prepared_revoke')}</Text></Pressable></View>)}
  <View style={{flexDirection:'row',gap:8}}><Pressable style={s.button} accessibilityRole="button" disabled={busy||savedLoading||!savedHistory.length} onPress={previousSavedPage}><Text style={s.buttonText}>{t('interchange_prepared_saved_previous')}</Text></Pressable><Pressable style={s.button} accessibilityRole="button" disabled={busy||savedLoading||!savedNextAfter} onPress={nextSavedPage}><Text style={s.buttonText}>{t('interchange_prepared_saved_next')}</Text></Pressable></View>
  {!loading&&items?.length===0?<Text style={s.subtitle}>{t('interchange_prepared_empty')}</Text>:null}
  {items?.map(row=><Pressable key={identity(row)} style={s.button} accessibilityRole="button" onPress={()=>{mutation.current?.abort();setSelected(identity(row));setChoices({});attempt.current=null;setError(false);}}><Text style={s.buttonText}>{row.kind==='defects'?t('interchange_prepared_defects'):t('interchange_prepared_dust')} · {row.entry_id.slice(0,8)}{selected===identity(row)?' ✓':''} {row.decision_status?`· ${row.decision_status}`:''}</Text></Pressable>)}
  <View style={{flexDirection:'row',gap:8}}><Pressable style={s.button} accessibilityRole="button" disabled={busy||loading||!history.length} onPress={previousProposalPage}><Text style={s.buttonText}>{t('interchange_prepared_previous')}</Text></Pressable><Pressable style={s.button} accessibilityRole="button" disabled={busy||loading||!nextAfter} onPress={nextProposalPage}><Text style={s.buttonText}>{t('interchange_prepared_next')}</Text></Pressable></View>
  {item&&<View style={{gap:10}}>
   <Text style={s.subtitle}>{item.kind==='defects'?t('interchange_prepared_defects'):t('interchange_prepared_dust')} · {item.document.candidates.length} {t('interchange_prepared_candidates')}</Text>
   {photo&&canvas?<View style={{width:'100%',aspectRatio:canvas.width/canvas.height,backgroundColor:'#202124'}}><Image accessibilityLabel={t('interchange_prepared_photo')} source={{uri:photo}} resizeMode="contain" style={{width:'100%',height:'100%'}} onLoad={()=>setPhotoReady(true)} onError={()=>{setPhotoReady(false);setError(true);}}/>
    {photoReady&&item.document.candidates.map(candidate=>candidate.bbox?<View key={candidate.id} pointerEvents="none" style={{position:'absolute',left:`${candidate.bbox.x/canvas.width*100}%` as `${number}%`,top:`${candidate.bbox.y/canvas.height*100}%` as `${number}%`,width:`${candidate.bbox.width/canvas.width*100}%` as `${number}%`,height:`${candidate.bbox.height/canvas.height*100}%` as `${number}%`,borderWidth:2,borderColor:choices[candidate.id]==='accepted'?'#3ddc84':choices[candidate.id]==='rejected'?'#ff5b5b':'#ffcc33'}}/>:null)}
   </View>:null}
   {photo&&!canvas?<Image accessibilityLabel={t('interchange_prepared_photo')} source={{uri:photo}} resizeMode="contain" style={{width:'100%',height:320}} onLoad={()=>setPhotoReady(true)} onError={()=>{setPhotoReady(false);setError(true);}}/>:null}
   {dustCrop?<View style={{gap:6}}><Text style={s.subtitle}>{t('interchange_prepared_sensor_map')}</Text>
    <View accessibilityLabel={t('interchange_prepared_sensor_map')} style={{width:'100%',aspectRatio:dustCrop.width/dustCrop.height,backgroundColor:'#172330',borderWidth:1,borderColor:'#8293a6',overflow:'hidden'}}>
     {item.document.candidates.map(candidate=>{const marker=projectDustCandidateToCrop(dustCrop,candidate);return <View key={candidate.id} pointerEvents="none" style={{position:'absolute',left:`${marker.left}%` as `${number}%`,top:`${marker.top}%` as `${number}%`,width:`${marker.width}%` as `${number}%`,height:`${marker.height}%` as `${number}%`,borderWidth:2,borderRadius:999,borderColor:choices[candidate.id]==='accepted'?'#3ddc84':choices[candidate.id]==='rejected'?'#ff5b5b':'#ffcc33'}}/>;})}
    </View><Text>{t('interchange_prepared_sensor_map_note')}</Text>
   </View>:null}
   {!photo?<ActivityIndicator accessibilityLabel={t('interchange_prepared_loading')}/>:null}
   {item.document.candidates.map(candidate=><View key={candidate.id} style={{gap:6}}>
    <Text>{candidate.id.slice(0,12)} · {item.kind==='defects'?`${candidate.bbox?.x}, ${candidate.bbox?.y} · ${candidate.bbox?.width}×${candidate.bbox?.height}`:`${candidate.sensor_x}, ${candidate.sensor_y} · r=${candidate.radius_sensor_px}`}</Text>
    <View style={{flexDirection:'row',gap:8}}><Pressable style={s.button} accessibilityRole="checkbox" accessibilityState={{checked:choices[candidate.id]==='accepted',disabled:busy||!photoReady}} disabled={busy||!photoReady} onPress={()=>choose(candidate.id,'accepted')}><Text style={s.buttonText}>{t('interchange_prepared_accept')}</Text></Pressable><Pressable style={s.button} accessibilityRole="checkbox" accessibilityState={{checked:choices[candidate.id]==='rejected',disabled:busy||!photoReady}} disabled={busy||!photoReady} onPress={()=>choose(candidate.id,'rejected')}><Text style={s.buttonText}>{t('interchange_prepared_reject')}</Text></Pressable></View>
   </View>)}
   <Text style={s.subtitle}>{t('interchange_prepared_explain')}</Text>
   <Pressable style={s.button} accessibilityRole="button" disabled={busy||!photoReady||!Object.values(choices).some(Boolean)} onPress={()=>void decide('grant','accept')}><Text style={s.buttonText}>{t('interchange_prepared_save')}</Text></Pressable>
   <Pressable style={s.button} accessibilityRole="button" disabled={busy||!photoReady} onPress={()=>void decide('grant','skip')}><Text style={s.buttonText}>{t('interchange_prepared_skip')}</Text></Pressable>
  </View>}
  <Pressable style={s.button} accessibilityRole="button" disabled={busy||loading||savedLoading} onPress={refresh}><Text style={s.buttonText}>{t('interchange_refresh')}</Text></Pressable>
 </ScrollView></SafeAreaView>;
}
