import {ActivityIndicator,Button,Text,TouchableRipple} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useEffect,useMemo,useRef,useState} from 'react';
import {Image,SafeAreaView,ScrollView,View} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import {useReviewForeground} from './use-review-foreground';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {PreparedReviewAttemptStore} from './prepared-review-attempt-store';
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
 const generation=useMemo(()=>crypto.randomUUID(),[props.client]);
 const scope=JSON.stringify([props.client.origin,props.tenant,props.project,props.workflow]);
 return <ScopedScreen key={`${generation}:${scope}`} {...props}/>;
}

function ScopedScreen({client,tenant,project,workflow}:Props){
 const __uiT=useLocale().t;

 const foreground=useReviewForeground();
 const active=useRef(foreground.active);active.current=foreground.active;
 const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [items,setItems]=useState<PreparedReviewItem[]|null>(null),[entries,setEntries]=useState<ImportPersonMaskEntry[]>([]);
 const [saved,setSaved]=useState<SavedPreparedReview[]|null>(null);
 const [after,setAfter]=useState(''),[history,setHistory]=useState<string[]>([]),[nextAfter,setNextAfter]=useState('');
 const [savedAfter,setSavedAfter]=useState(''),[savedHistory,setSavedHistory]=useState<string[]>([]),[savedNextAfter,setSavedNextAfter]=useState(''),[savedLoading,setSavedLoading]=useState(true);
 const [selected,setSelected]=useState(''),[choices,setChoices]=useState<Record<string,Choice>>({});
 const [photo,setPhoto]=useState(''),[photoReady,setPhotoReady]=useState(false),[busy,setBusy]=useState(false),[loading,setLoading]=useState(true);
 const [error,setError]=useState(false),[revision,setRevision]=useState(0);
 const reads=useRef(new Set<AbortController>()),[readCancelled,setReadCancelled]=useState(false);
 const mutation=useRef<AbortController|null>(null),attempt=useRef<{fingerprint:string;key:string;body:PreparedReviewDecision}|null>(null);
 const [photoProof,setPhotoProof]=useState(''),[previewRevision,setPreviewRevision]=useState(0);
 const currentPhotoProof=useRef('');
 const journal=useRef<PreparedReviewAttemptStore|null>(null);
 const [journalReady,setJournalReady]=useState(false),[journalFailed,setJournalFailed]=useState(false),[journalRevision,setJournalRevision]=useState(0);
 const photoExpires=useRef(0),photoScope=useRef(''),currentSelection=useRef(selected);currentSelection.current=selected;
 const item=items?.find(row=>identity(row)===selected),entry=entries.find(row=>row.entry_id===item?.entry_id&&row.target_version_id===item.target_version_id);

 useEffect(()=>{
  const controller=new AbortController();reads.current.add(controller);journal.current=null;setJournalReady(false);setJournalFailed(false);
  if(!foreground.active)return()=>{reads.current.delete(controller);controller.abort();};
  void readDeadline(controller.signal,async signal=>{
   const user=await api.me(signal);signal.throwIfAborted();
   const store=new PreparedReviewAttemptStore(AsyncStorage,client.origin,user.id,tenant,project,workflow);
   const saved=await store.read();signal.throwIfAborted();
   if(saved)attempt.current={fingerprint:JSON.stringify(saved.body),key:saved.key,body:saved.body};else attempt.current=null;
   journal.current=store;setJournalReady(true);
  }).catch(()=>{if(!controller.signal.aborted){setError(true);setJournalFailed(true);}});
  return()=>{reads.current.delete(controller);controller.abort();journal.current=null;};
 },[api,client.origin,tenant,project,workflow,journalRevision,foreground.active,foreground.generation]);

 useEffect(()=>{
  const controller=new AbortController();reads.current.add(controller);setLoading(true);setError(false);setItems(null);setSelected('');setChoices({});
  if(!foreground.active)return()=>{reads.current.delete(controller);controller.abort();};
  void Promise.all([api.preparedImportReviewPage(project,workflow,after,controller.signal),api.importPersonMaskEntries(project,workflow,controller.signal)]).then(([page,adopted])=>{
   if(controller.signal.aborted)return;
   if(new Set(page.items.map(identity)).size!==page.items.length||page.next_after&&(typeof page.next_after!=='string'||page.next_after.length>4096||page.next_after===after||history.includes(page.next_after)))throw new Error('INTERCHANGE_PREPARED_PAGE_INVALID');
   if(page.items.some(row=>!adopted.entries.some(entry=>entry.entry_id===row.entry_id&&entry.target_version_id===row.target_version_id)))throw new Error('INTERCHANGE_PREPARED_REVIEW_SCOPE');
   setItems(page.items);setNextAfter(page.next_after??'');setEntries(adopted.entries);setSelected('');setChoices({});
  }).catch(()=>{if(!controller.signal.aborted){setItems(null);setNextAfter('');setError(true);}}).finally(()=>{if(!controller.signal.aborted)setLoading(false);});
  return()=>{reads.current.delete(controller);controller.abort();};
 },[api,project,workflow,revision,after,foreground.active,foreground.generation]);
 useEffect(()=>{
  const controller=new AbortController();reads.current.add(controller);setSaved(null);setSavedLoading(true);
  if(!foreground.active)return()=>{reads.current.delete(controller);controller.abort();};
  void api.savedPreparedImportReviewPage(project,workflow,savedAfter,controller.signal).then(page=>{if(!controller.signal.aborted){if(new Set(page.items.map(row=>`${row.entry_id}:${row.target_version_id}:${row.kind}:${row.proposal.artifact_id}`)).size!==page.items.length||page.next_after&&(typeof page.next_after!=='string'||page.next_after.length>4096||page.next_after===savedAfter||savedHistory.includes(page.next_after)))throw new Error('INTERCHANGE_PREPARED_PAGE_INVALID');setSaved(page.items);setSavedNextAfter(page.next_after??'');}}).catch(()=>{if(!controller.signal.aborted){setSavedNextAfter('');setError(true);}}).finally(()=>{if(!controller.signal.aborted)setSavedLoading(false);});
  return()=>{reads.current.delete(controller);controller.abort();};
 },[api,project,workflow,revision,savedAfter,foreground.active,foreground.generation]);

 useEffect(()=>{
  const controller=new AbortController();let expiry:ReturnType<typeof setTimeout>|undefined;currentPhotoProof.current='';setPhotoProof('');photoExpires.current=0;photoScope.current='';setPhoto('');setPhotoReady(false);
  if(!foreground.active||!item||!entry)return()=>controller.abort();
  void readDeadline(controller.signal,signal=>api.mediaLinks(entry.asset_id,item.target_version_id,['preview1600','thumb640'],signal)).then(links=>{
   if(controller.signal.aborted)return;
   const canvas=item.kind==='defects'?item.document.canvas as {width:number;height:number}:null;
   const link=links.items.find(link=>link.asset_version_id===item.target_version_id&&['preview1600','thumb640'].includes(link.variant)&&['image/jpeg','image/png','image/webp'].includes(link.media_type)&&link.width>0&&link.height>0&&Date.parse(link.expires_at)>Date.now()+5000&&(!canvas||Math.abs(link.width/link.height-canvas.width/canvas.height)<0.01));
   if(!link)throw new Error('PHOTO_PREVIEW_UNAVAILABLE');const proof=crypto.randomUUID();currentPhotoProof.current=proof;setPhotoProof(proof);setPhoto(safeURL(link.url));photoExpires.current=Date.parse(link.expires_at);photoScope.current=identity(item);
   expiry=setTimeout(()=>{currentPhotoProof.current='';photoExpires.current=0;setPhotoReady(false);setPhoto('');setError(true);},Math.min(2147483647,photoExpires.current-Date.now()));
  }).catch(()=>{if(!controller.signal.aborted)setError(true);});
  return()=>{controller.abort();currentPhotoProof.current='';if(expiry)clearTimeout(expiry);photoExpires.current=0;};
 },[api,entry?.asset_id,item?.proposal.artifact_id,item?.target_version_id,foreground.active,foreground.generation,previewRevision]);
 useEffect(()=>()=>mutation.current?.abort(),[]);
 useEffect(()=>{if(!foreground.active&&mutation.current)cancelMutation();},[foreground.active]);
 useEffect(()=>{if(!busy)return;const timer=setTimeout(cancelMutation,15000);return()=>clearTimeout(timer);},[busy]);
 function cancelMutation(){mutation.current?.abort();mutation.current=null;setBusy(false);setError(true);}

 function refresh(){if(mutation.current||attempt.current)return;setAfter('');setHistory([]);setNextAfter('');setSavedAfter('');setSavedHistory([]);setSavedNextAfter('');setRevision(value=>value+1);}
 function nextProposalPage(){if(!nextAfter||loading||busy||attempt.current)return;setLoading(true);setHistory(current=>[...current,after]);setAfter(nextAfter);}
 function previousProposalPage(){if(!history.length||loading||busy||attempt.current)return;setLoading(true);setAfter(history[history.length-1]);setHistory(current=>current.slice(0,-1));}
 function nextSavedPage(){if(!savedNextAfter||savedLoading||busy||attempt.current)return;setSavedLoading(true);setSavedHistory(current=>[...current,savedAfter]);setSavedAfter(savedNextAfter);}
 function previousSavedPage(){if(!savedHistory.length||savedLoading||busy||attempt.current)return;setSavedLoading(true);setSavedAfter(savedHistory[savedHistory.length-1]);setSavedHistory(current=>current.slice(0,-1));}

 function choose(id:string,value:Choice){
  if(busy||mutation.current||attempt.current)return;
  setChoices(current=>({...current,[id]:current[id]===value?undefined:value} as Record<string,Choice>));attempt.current=null;
 }
 async function decide(action:'grant'|'revoke',decision:'accept'|'reject'|'skip'='accept'){
  if(!journal.current||!active.current||!item||mutation.current||!attempt.current&&(!photoReady||photoExpires.current<=Date.now()||photoScope.current!==identity(item)))return;
  const accepted=Object.entries(choices).filter(([,v])=>v==='accepted').map(([id])=>id);
  const rejected=Object.entries(choices).filter(([,v])=>v==='rejected').map(([id])=>id);
  if(action==='grant'&&decision==='accept'&&accepted.length+rejected.length===0)return;
  let body:PreparedReviewDecision;
  try{body=freezePreparedReviewDecision(item,action,decision,accepted,rejected);}catch{setError(true);return;}
  const fingerprint=JSON.stringify(body);
  if(attempt.current&&attempt.current.fingerprint!==fingerprint){setError(true);return;}
  if(!attempt.current)attempt.current={fingerprint,key:crypto.randomUUID(),body};
  const controller=new AbortController();mutation.current=controller;setBusy(true);setError(false);
  try{
   const saved=attempt.current,store=journal.current!;
   await store.write({version:1,key:saved.key,body:saved.body});controller.signal.throwIfAborted();
   if(!active.current)throw new Error('REVIEW_BACKGROUND');
   const receipt=await api.decidePreparedImportReview(project,workflow,saved.body,saved.key,controller.signal);
   controller.signal.throwIfAborted();await store.clear(saved.key,receipt);
   if(!controller.signal.aborted){attempt.current=null;mutation.current=null;setBusy(false);refresh();}
  }
  catch{if(!controller.signal.aborted)setError(true);}
  finally{if(mutation.current===controller){mutation.current=null;setBusy(false);}}
 }
 async function revokeSaved(row:SavedPreparedReview){
  if(!journal.current||!active.current||mutation.current)return;
  let body:PreparedReviewDecision;
  try{body=freezeSavedPreparedRevoke(row);}catch{setError(true);return;}
  const fingerprint=JSON.stringify(body);
  if(attempt.current&&attempt.current.fingerprint!==fingerprint){setError(true);return;}
  if(!attempt.current)attempt.current={fingerprint,key:crypto.randomUUID(),body};
  const controller=new AbortController();mutation.current=controller;setBusy(true);setError(false);
  try{
   const saved=attempt.current,store=journal.current!;
   await store.write({version:1,key:saved.key,body:saved.body});controller.signal.throwIfAborted();
   if(!active.current)throw new Error('REVIEW_BACKGROUND');
   const receipt=await api.decidePreparedImportReview(project,workflow,saved.body,saved.key,controller.signal);
   controller.signal.throwIfAborted();await store.clear(saved.key,receipt);
   if(!controller.signal.aborted){attempt.current=null;mutation.current=null;setBusy(false);refresh();}
  }
  catch{if(!controller.signal.aborted)setError(true);}
  finally{if(mutation.current===controller){mutation.current=null;setBusy(false);}}
 }

 async function resume(){
  if(!journal.current||!attempt.current||mutation.current||!active.current)return;
  const saved=attempt.current,store=journal.current,controller=new AbortController();mutation.current=controller;setBusy(true);setError(false);
  try{
   await store.write({version:1,key:saved.key,body:saved.body});controller.signal.throwIfAborted();
   if(!active.current)throw new Error('REVIEW_BACKGROUND');
   const receipt=await api.decidePreparedImportReview(project,workflow,saved.body,saved.key,controller.signal);
   controller.signal.throwIfAborted();await store.clear(saved.key,receipt);controller.signal.throwIfAborted();
   attempt.current=null;mutation.current=null;setBusy(false);refresh();
  }catch{if(!controller.signal.aborted)setError(true);}
  finally{if(mutation.current===controller){mutation.current=null;if(!controller.signal.aborted)setBusy(false);}}
 }

 if(!foreground.active)return <SafeAreaView style={s.root}><Text style={s.subtitle}>{t('read_paused')}</Text></SafeAreaView>;
 const canvas=item?.kind==='defects'?item.document.canvas as {width:number;height:number}:null;
 const dustCrop=item?.kind==='dust'?item.document.raw_crop as DustCrop:null;
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={{padding:20,gap:12}}>
  <Text style={s.title}>{t('interchange_prepared_title')}</Text>
  {error&&!journalFailed?<Button   accessibilityRole="button" disabled={busy||!foreground.active} onPress={()=>{setReadCancelled(false);setRevision(value=>value+1);}} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_reload')}</Button>:null}
  {journalFailed?<Button   accessibilityRole="button" disabled={busy||!foreground.active} onPress={()=>{setReadCancelled(false);setJournalRevision(value=>value+1);setRevision(value=>value+1);}} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_reload')}</Button>:null}
  {(loading||savedLoading||!journalReady&&!journalFailed)&&!readCancelled?<Button   accessibilityRole="button" disabled={busy} onPress={()=>{for(const controller of reads.current)controller.abort();setReadCancelled(true);setLoading(false);setSavedLoading(false);setError(true);if(!journalReady)setJournalFailed(true);}} mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button>:null}
  {!journalReady&&!journalFailed&&!readCancelled?<ActivityIndicator accessibilityLabel={t('interchange_prepared_loading')}/>:null}
  {journalReady&&attempt.current?<Button   accessibilityRole="button" disabled={busy||!journalReady||!foreground.active} onPress={()=>void resume()} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_resume')}</Button>:null}
  {loading?<ActivityIndicator accessibilityLabel={t('interchange_prepared_loading')}/>:null}
  {busy?<Button   accessibilityRole="button" onPress={cancelMutation} mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button>:null}
  {error?<Text style={s.error} accessibilityRole="alert">{t('interchange_prepared_error')}</Text>:null}
  {saved?.filter(row=>row.status==='granted').map(row=><View key={`saved:${row.entry_id}:${row.proposal.artifact_id}`} style={{gap:6}}><Text style={s.subtitle}>{row.kind==='defects'?t('interchange_prepared_defects'):t('interchange_prepared_dust')} · {row.entry_id.slice(0,8)} · {t('interchange_prepared_saved')}</Text><Button   accessibilityRole="button" disabled={busy||!journalReady||!foreground.active} onPress={()=>void revokeSaved(row)} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_revoke')}</Button></View>)}
  <View style={{flexDirection:'row',gap:8}}><Button   accessibilityRole="button" disabled={busy||savedLoading||!savedHistory.length} onPress={previousSavedPage} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_saved_previous')}</Button><Button   accessibilityRole="button" disabled={busy||savedLoading||!savedNextAfter} onPress={nextSavedPage} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_saved_next')}</Button></View>
  {!loading&&items?.length===0?<Text style={s.subtitle}>{t('interchange_prepared_empty')}</Text>:null}
  {items?.map(row=><Button  key={identity(row)}  accessibilityRole="button" disabled={busy||!!attempt.current} onPress={()=>{if(mutation.current||attempt.current)return;setSelected(identity(row));setChoices({});attempt.current=null;setError(false);}} mode="contained" contentStyle={{minHeight:48}}>{row.kind==='defects'?t('interchange_prepared_defects'):t('interchange_prepared_dust')} · {row.entry_id.slice(0,8)}{selected===identity(row)?' ✓':''} {row.decision_status?__uiT('ui_5438dc61062e', {p0:(row.decision_status)}):''}</Button>)}
  <View style={{flexDirection:'row',gap:8}}><Button   accessibilityRole="button" disabled={busy||loading||!history.length} onPress={previousProposalPage} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_previous')}</Button><Button   accessibilityRole="button" disabled={busy||loading||!nextAfter} onPress={nextProposalPage} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_next')}</Button></View>
  {item&&<View style={{gap:10}}>
   <Text style={s.subtitle}>{item.kind==='defects'?t('interchange_prepared_defects'):t('interchange_prepared_dust')} · {item.document.candidates.length} {t('interchange_prepared_candidates')}</Text>
   <Button   accessibilityRole="button" disabled={busy||!foreground.active} onPress={()=>{currentPhotoProof.current='';photoExpires.current=0;photoScope.current='';setPhotoReady(false);setPhoto('');setPreviewRevision(value=>value+1);}} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_refresh_preview')}</Button>
   {foreground.active&&photo&&canvas?<View style={{width:'100%',aspectRatio:canvas.width/canvas.height,backgroundColor:'#202124'}}><Image accessibilityLabel={t('interchange_prepared_photo')} source={{uri:photo}} resizeMode="contain" style={{width:'100%',height:'100%'}} key={photoProof} onLoad={()=>{if(currentPhotoProof.current===photoProof&&active.current&&currentSelection.current===selected&&photoScope.current===selected)setPhotoReady(photoExpires.current>Date.now());}} onError={()=>{if(currentPhotoProof.current===photoProof&&currentSelection.current===selected){setPhotoReady(false);setError(true);}}}/>
    {photoReady&&item.document.candidates.map(candidate=>candidate.bbox?<View key={candidate.id} pointerEvents="none" style={{position:'absolute',left:`${candidate.bbox.x/canvas.width*100}%` as `${number}%`,top:`${candidate.bbox.y/canvas.height*100}%` as `${number}%`,width:`${candidate.bbox.width/canvas.width*100}%` as `${number}%`,height:`${candidate.bbox.height/canvas.height*100}%` as `${number}%`,borderWidth:2,borderColor:choices[candidate.id]==='accepted'?'#3ddc84':choices[candidate.id]==='rejected'?'#ff5b5b':'#ffcc33'}}/>:null)}
   </View>:null}
   {foreground.active&&photo&&!canvas?<Image accessibilityLabel={t('interchange_prepared_photo')} source={{uri:photo}} resizeMode="contain" style={{width:'100%',height:320}} key={photoProof} onLoad={()=>{if(currentPhotoProof.current===photoProof&&active.current&&currentSelection.current===selected&&photoScope.current===selected)setPhotoReady(photoExpires.current>Date.now());}} onError={()=>{if(currentPhotoProof.current===photoProof&&currentSelection.current===selected){setPhotoReady(false);setError(true);}}}/>:null}
   {dustCrop?<View style={{gap:6}}><Text style={s.subtitle}>{t('interchange_prepared_sensor_map')}</Text>
    <View accessibilityLabel={t('interchange_prepared_sensor_map')} style={{width:'100%',aspectRatio:dustCrop.width/dustCrop.height,backgroundColor:'#172330',borderWidth:1,borderColor:'#8293a6',overflow:'hidden'}}>
     {item.document.candidates.map(candidate=>{const marker=projectDustCandidateToCrop(dustCrop,candidate);return <View key={candidate.id} pointerEvents="none" style={{position:'absolute',left:`${marker.left}%` as `${number}%`,top:`${marker.top}%` as `${number}%`,width:`${marker.width}%` as `${number}%`,height:`${marker.height}%` as `${number}%`,borderWidth:2,borderRadius:999,borderColor:choices[candidate.id]==='accepted'?'#3ddc84':choices[candidate.id]==='rejected'?'#ff5b5b':'#ffcc33'}}/>;})}
    </View><Text>{t('interchange_prepared_sensor_map_note')}</Text>
   </View>:null}
   {!photo?<ActivityIndicator accessibilityLabel={t('interchange_prepared_loading')}/>:null}
   {item.document.candidates.map(candidate=><View key={candidate.id} style={{gap:6}}>
    <Text>{candidate.id.slice(0,12)} · {item.kind==='defects'?__uiT('ui_a598e9f931d0', {p0:(candidate.bbox?.x??'—'),p1:(candidate.bbox?.y??'—'),p2:(candidate.bbox?.width??'—'),p3:(candidate.bbox?.height??'—')}):__uiT('ui_eb3223cb81ce', {p0:(candidate.sensor_x??'—'),p1:(candidate.sensor_y??'—'),p2:(candidate.radius_sensor_px??'—')})}</Text>
    <View style={{flexDirection:'row',gap:8}}><TouchableRipple  style={s.button} accessibilityRole="checkbox" aria-checked={choices[candidate.id]==='accepted'} accessibilityState={{checked:choices[candidate.id]==='accepted',disabled:busy||!!attempt.current||!photoReady}} disabled={busy||!!attempt.current||!photoReady} onPress={()=>choose(candidate.id,'accepted')}><Text style={s.buttonText}>{t('interchange_prepared_accept')}</Text></TouchableRipple><TouchableRipple  style={s.button} accessibilityRole="checkbox" aria-checked={choices[candidate.id]==='rejected'} accessibilityState={{checked:choices[candidate.id]==='rejected',disabled:busy||!!attempt.current||!photoReady}} disabled={busy||!!attempt.current||!photoReady} onPress={()=>choose(candidate.id,'rejected')}><Text style={s.buttonText}>{t('interchange_prepared_reject')}</Text></TouchableRipple></View>
   </View>)}
   <Text style={s.subtitle}>{t('interchange_prepared_explain')}</Text>
   <Button   accessibilityRole="button" disabled={busy||!journalReady||!foreground.active||!attempt.current&&(!photoReady||!Object.values(choices).some(Boolean))} onPress={()=>void decide('grant','accept')} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_save')}</Button>
   <Button   accessibilityRole="button" disabled={busy||!journalReady||!foreground.active||!photoReady&&!attempt.current} onPress={()=>void decide('grant','skip')} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_skip')}</Button>
  </View>}
  <Button   accessibilityRole="button" disabled={busy||!!attempt.current||loading||savedLoading} onPress={refresh} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_refresh')}</Button>
 </ScrollView></SafeAreaView>;
}
