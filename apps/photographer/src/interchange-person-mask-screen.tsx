import {ActivityIndicator,Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {importMaskDecisionEpoch} from './import-mask-decision-admission';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {ImportMaskAttemptStore} from './import-mask-attempt-store';
import {ImportMaskDecisionAttempt} from './import-mask-decision-attempt';
import type {ImportPersonMaskDecision} from './interchange-person-mask-decision';
import {useEffect,useMemo,useRef,useState} from 'react';
import {Image,Platform,SafeAreaView,ScrollView,View} from 'react-native';
import {APIError,type ApiClient} from '@photoeditor/api-client';
import {useReviewForeground} from './use-review-foreground';
import {OwnerAPI} from './owner-api';
import type {ImportPersonMaskCandidate,ImportPersonMaskEntry} from './interchange-person-mask-candidates';
import type {ImportPersonMaskStatus} from './interchange-person-mask-status';
import {readPersonMaskPreviewPNG} from './interchange-person-mask-preview';
import {perspectivePreviewBase64} from './perspective-preview';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

type Props={client:ApiClient;tenant:string;project:string;workflow:string};
type Visible={proofID:string;entry:string;mask:string;override:string;photo:string;overlay:string;expiresAt:number;dispose():void};
type MaskErrorKey='interchange_mask_entries_error'|'interchange_mask_candidates_error'|'interchange_mask_preview_error'|'interchange_mask_decision_error'|'';
const identity=(candidate:ImportPersonMaskCandidate)=>`${candidate.decision.override_artifact_id}:${candidate.decision.mask_artifact_id}`;

export function InterchangePersonMaskScreen(props:Props){
 const generation=useMemo(()=>crypto.randomUUID(),[props.client]);
  const key=JSON.stringify([props.client.origin,props.tenant,props.project,props.workflow]);
  return <ScopedScreen key={`${generation}:${key}`} {...props}/>;
}

function ScopedScreen({client,tenant,project,workflow}:Props){
  const foreground=useReviewForeground();
  const active=useRef(foreground.active);active.current=foreground.active;
  const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
  const [entries,setEntries]=useState<ImportPersonMaskEntry[]|null>(null),[entryID,setEntryID]=useState('');
  const [candidates,setCandidates]=useState<ImportPersonMaskCandidate[]|null>(null),[selected,setSelected]=useState('');
  const [visible,setVisible]=useState<Visible|null>(null),[error,setError]=useState<MaskErrorKey>(''),[busy,setBusy]=useState(false),[decision,setDecision]=useState<'granted'|'revoked'|''>('');
  const [photoLoaded,setPhotoLoaded]=useState(false),[maskLoaded,setMaskLoaded]=useState(false);
  const [statuses,setStatuses]=useState<ImportPersonMaskStatus[]|null>(null);
  const attempt=useRef(new ImportMaskDecisionAttempt());
  const journal=useRef<ImportMaskAttemptStore|null>(null),attemptKey=useRef<string|undefined>(undefined);
  const [journalReady,setJournalReady]=useState(false),[journalFailed,setJournalFailed]=useState(false),[journalRevision,setJournalRevision]=useState(0);
  const [readRevision,setReadRevision]=useState(0),[previewRevision,setPreviewRevision]=useState(0);
  const reads=useRef(new Set<AbortController>()),[readCancelled,setReadCancelled]=useState(false);
  const mutation=useRef<AbortController|null>(null);
  const statusRead=useRef<AbortController|null>(null);
  const displayedScope=useRef(''),displayedProof=useRef('');
  const entry=entries?.find(item=>item.entry_id===entryID);
  const candidate=candidates?.find(item=>identity(item)===selected);

  useEffect(()=>{
    const controller=new AbortController();reads.current.add(controller);journal.current=null;setJournalReady(false);setJournalFailed(false);
    if(!foreground.active)return()=>{reads.current.delete(controller);controller.abort();};
    void readDeadline(controller.signal,async signal=>{
      const user=await api.me(signal);signal.throwIfAborted();
      const store=new ImportMaskAttemptStore(AsyncStorage,client.origin,user.id,tenant,project,workflow);
      const saved=await store.read();signal.throwIfAborted();
      if(saved){attempt.current.prepare(saved.body);attemptKey.current=saved.key;}else{attempt.current=new ImportMaskDecisionAttempt();attemptKey.current=undefined;}
      journal.current=store;setJournalReady(true);
    }).catch(()=>{if(!controller.signal.aborted){setJournalFailed(true);setError('interchange_mask_decision_error');}});
    return()=>{reads.current.delete(controller);controller.abort();journal.current=null;};
  },[api,client.origin,tenant,project,workflow,journalRevision,foreground.active,foreground.generation]);

  useEffect(()=>{
    const controller=new AbortController();reads.current.add(controller);setEntries(null);setEntryID('');setError('');
    if(!foreground.active)return()=>{reads.current.delete(controller);controller.abort();};
    void api.importPersonMaskEntries(project,workflow,controller.signal).then(page=>{if(!controller.signal.aborted)setEntries(page.entries);}).catch(()=>{if(!controller.signal.aborted){setEntries([]);setError('interchange_mask_entries_error');}});
    return()=>{reads.current.delete(controller);controller.abort();};
  },[api,project,workflow,readRevision,foreground.active,foreground.generation]);
  useEffect(()=>{
    const controller=new AbortController();reads.current.add(controller);statusRead.current=controller;setStatuses(null);
    if(!foreground.active)return()=>{reads.current.delete(controller);controller.abort();};
    void api.importPersonMaskStatuses(project,workflow,controller.signal).then(items=>{if(!controller.signal.aborted)setStatuses(items);}).catch(()=>{if(!controller.signal.aborted)setError('interchange_mask_decision_error');});
    return()=>{reads.current.delete(controller);controller.abort();if(statusRead.current===controller)statusRead.current=null;};
  },[api,project,workflow,readRevision,foreground.active,foreground.generation]);
  useEffect(()=>{
    const controller=new AbortController();reads.current.add(controller);setCandidates(null);setSelected('');setDecision('');setVisible(null);
    if(!foreground.active)return()=>{reads.current.delete(controller);controller.abort();};
    if(entryID)void api.importPersonMaskCandidates(project,workflow,entryID,controller.signal).then(page=>{
      if(!controller.signal.aborted&&entries?.some(item=>item.entry_id===entryID&&page.candidates.every(candidate=>candidate.decision.target_version_id===item.target_version_id)))setCandidates(page.candidates);
    }).catch(()=>{if(!controller.signal.aborted)setError('interchange_mask_candidates_error');});
    return()=>{reads.current.delete(controller);controller.abort();};
  },[api,project,workflow,entryID,entries,readRevision,foreground.active,foreground.generation]);
  useEffect(()=>{
    const controller=new AbortController();let objectURL='';let expiry:ReturnType<typeof setTimeout>|undefined;
    const deadline=foreground.active&&entry&&candidate?setTimeout(()=>{if(!controller.signal.aborted){controller.abort();displayedProof.current='';displayedScope.current='';setVisible(value=>{value?.dispose();return null;});setError('interchange_mask_preview_error');}},8000):undefined;
    displayedScope.current='';displayedProof.current='';setVisible(null);setPhotoLoaded(false);setMaskLoaded(false);
    if(foreground.active&&entry&&candidate)void(async()=>{
      try{
        const [png,media]=await Promise.all([
          api.importPersonMaskPreview(project,workflow,entry.entry_id,candidate.decision.override_artifact_id,candidate.decision.mask_artifact_id,controller.signal),
          api.mediaLinks(entry.asset_id,entry.target_version_id,['preview1600','thumb640'],controller.signal),
        ]);
        controller.signal.throwIfAborted();
        const mask=JSON.parse(candidate.decision.mask_jcs!) as {canvas:{width:number;height:number}};
        const dimensions=readPersonMaskPreviewPNG(png,candidate.decision.mask_jcs!);
        const link=media.items.find(item=>item.asset_version_id===entry.target_version_id&&['preview1600','thumb640'].includes(item.variant)&&['image/jpeg','image/png','image/webp'].includes(item.media_type)&&item.width>0&&item.height>0&&Date.parse(item.expires_at)>Date.now()+5000&&Math.abs(item.width/item.height-mask.canvas.width/mask.canvas.height)<0.01);
        if(!link)throw new Error('PHOTO_PREVIEW_UNAVAILABLE');
        const url=new URL(link.url);
        if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))throw new Error('PHOTO_PREVIEW_UNAVAILABLE');
        let overlay='';
        if(Platform.OS==='web'){
          const blob=new Blob([new Uint8Array(png)],{type:'image/png'});
          if(typeof createImageBitmap!=='function')throw new Error('MASK_PREVIEW_DECODER_UNAVAILABLE');
          const bitmap=await createImageBitmap(blob);
          try{if(bitmap.width!==dimensions.width||bitmap.height!==dimensions.height)throw new Error('MASK_PREVIEW_DIMENSIONS');}finally{bitmap.close();}
          controller.signal.throwIfAborted();objectURL=URL.createObjectURL(blob);overlay=objectURL;
        }else{
          overlay=`data:image/png;base64,${perspectivePreviewBase64(png)}`;
          const imageSize=await new Promise<{width:number;height:number}>((resolve,reject)=>Image.getSize(overlay,(width,height)=>resolve({width,height}),reject));
          if(imageSize.width!==dimensions.width||imageSize.height!==dimensions.height)throw new Error('MASK_PREVIEW_DIMENSIONS');
        }
        controller.signal.throwIfAborted();
        displayedScope.current=`${entry.entry_id}:${identity(candidate)}`;
        const proofID=crypto.randomUUID();displayedProof.current=proofID;
        setVisible({proofID,entry:entry.entry_id,mask:candidate.decision.mask_artifact_id,override:candidate.decision.override_artifact_id,photo:url.toString(),overlay,expiresAt:Date.parse(link.expires_at),dispose:()=>{if(objectURL){URL.revokeObjectURL(objectURL);objectURL='';}}});
        expiry=setTimeout(()=>{displayedProof.current='';displayedScope.current='';setPhotoLoaded(false);setMaskLoaded(false);setVisible(value=>{value?.dispose();return null;});setError('interchange_mask_preview_error');},Math.min(2147483647,Date.parse(link.expires_at)-Date.now()));
      }catch{if(!controller.signal.aborted)setError('interchange_mask_preview_error');}
      finally{clearTimeout(deadline);}
    })();
    return()=>{clearTimeout(deadline);controller.abort();displayedProof.current='';displayedScope.current='';if(expiry)clearTimeout(expiry);if(objectURL)URL.revokeObjectURL(objectURL);};
  },[api,project,workflow,entry,candidate,foreground.active,foreground.generation,previewRevision]);
  useEffect(()=>()=>mutation.current?.abort(),[]);
  useEffect(()=>{if(!foreground.active&&mutation.current)cancelMutation();},[foreground.active]);
  useEffect(()=>{if(!busy)return;const timer=setTimeout(cancelMutation,15000);return()=>clearTimeout(timer);},[busy]);
  function cancelMutation(){mutation.current?.abort();mutation.current=null;setBusy(false);setError('interchange_mask_decision_error');}

  async function submitDecision(request:ImportPersonMaskDecision,signal:AbortSignal){
    const store=journal.current;if(!store)throw new Error('IMPORT_MASK_JOURNAL_UNAVAILABLE');
    const body=attempt.current.prepare(request);
    const key=attemptKey.current??crypto.randomUUID();attemptKey.current=key;
    await store.write({version:1,key,body});signal.throwIfAborted();
    if(!active.current)throw new Error('IMPORT_MASK_BACKGROUND');
    let receipt;
    try{receipt=await api.decideImportPersonMask(project,workflow,body,signal);}
    catch(error){
      if(error instanceof APIError&&error.status===409&&error.code==='INTERCHANGE_MASK_REVISION_CONFLICT'){
        const statuses=await api.importPersonMaskStatuses(project,workflow,signal);signal.throwIfAborted();
        await store.clearSuperseded(key,statuses);signal.throwIfAborted();
        attempt.current.acknowledge(body);attemptKey.current=undefined;signal.throwIfAborted();setStatuses(statuses);
      }
      throw error;
    }
    signal.throwIfAborted();await store.clear(key,receipt);signal.throwIfAborted();attempt.current.acknowledge(body);attemptKey.current=undefined;signal.throwIfAborted();return receipt;
  }

  async function decide(action:'grant'|'revoke'){
    if(!journal.current||attempt.current.snapshot()||statuses===null||!active.current||displayedScope.current!==`${entryID}:${selected}`||!candidate||!entry||!visible||displayedProof.current!==visible.proofID||visible.expiresAt<=Date.now()||!photoLoaded||!maskLoaded||visible.entry!==entry.entry_id||visible.mask!==candidate.decision.mask_artifact_id||visible.override!==candidate.decision.override_artifact_id||mutation.current)return;
    const controller=new AbortController();mutation.current=controller;setBusy(true);setError('');
    try{
      const source=candidate.decision;
      const expected_epoch=importMaskDecisionEpoch(statuses,source,action);
      if(expected_epoch===null)return;
      const request=action==='grant'?{...source,expected_epoch}:{expected_epoch,action:'revoke' as const,entry_id:source.entry_id,target_version_id:source.target_version_id,override_artifact_id:source.override_artifact_id,mask_artifact_id:source.mask_artifact_id};
      const receipt=await submitDecision(request,controller.signal);
      if(!controller.signal.aborted){
        statusRead.current?.abort();
        setDecision(receipt.status);
        setStatuses(items=>{
          const match=(item:ImportPersonMaskStatus)=>item.entry_id===source.entry_id&&item.override_artifact_id===source.override_artifact_id&&item.mask_artifact_id===source.mask_artifact_id&&item.target_version_id===source.target_version_id;
          const next:ImportPersonMaskStatus={entry_id:source.entry_id,override_artifact_id:source.override_artifact_id,mask_artifact_id:source.mask_artifact_id,target_version_id:source.target_version_id,status:receipt.status,epoch:receipt.epoch};
          return items?.some(match)?items.map(item=>match(item)?next:item):[...(items??[]),next];
        });
      }
    }catch{if(!controller.signal.aborted)setError('interchange_mask_decision_error');}
    finally{if(mutation.current===controller){mutation.current=null;setBusy(false);}}
  }

  async function revokeSaved(item:ImportPersonMaskStatus){
    if(!journal.current||attempt.current.snapshot()||!active.current||item.status!=='granted'||mutation.current)return;
    const controller=new AbortController();mutation.current=controller;setBusy(true);setError('');
    try{
      const receipt=await submitDecision({action:'revoke',expected_epoch:item.epoch,entry_id:item.entry_id,target_version_id:item.target_version_id,override_artifact_id:item.override_artifact_id,mask_artifact_id:item.mask_artifact_id},controller.signal);
      if(!controller.signal.aborted){statusRead.current?.abort();setStatuses(items=>items?.map(row=>row===item?{...row,status:'revoked',epoch:receipt.epoch}:row)??null);}
    }catch{if(!controller.signal.aborted)setError('interchange_mask_decision_error');}
    finally{if(mutation.current===controller){mutation.current=null;setBusy(false);}}
  }

  async function retryDecision(){
    const body=attempt.current.snapshot();if(!journal.current||!body||!active.current||mutation.current)return;
    const controller=new AbortController();mutation.current=controller;setBusy(true);setError('');
    try{
      const receipt=await submitDecision(body,controller.signal);
      if(!controller.signal.aborted){
        statusRead.current?.abort();setDecision(receipt.status);
        setStatuses(items=>{
          const match=(item:ImportPersonMaskStatus)=>item.entry_id===body.entry_id&&item.override_artifact_id===body.override_artifact_id&&item.mask_artifact_id===body.mask_artifact_id&&item.target_version_id===body.target_version_id;
          const next:ImportPersonMaskStatus={entry_id:body.entry_id,override_artifact_id:body.override_artifact_id,mask_artifact_id:body.mask_artifact_id,target_version_id:body.target_version_id,status:receipt.status,epoch:receipt.epoch};
          return [...(items??[]).filter(item=>!match(item)),next];
        });
      }
    }catch{if(!controller.signal.aborted)setError('interchange_mask_decision_error');}
    finally{if(mutation.current===controller){mutation.current=null;setBusy(false);}}
  }

  if(!foreground.active)return <SafeAreaView style={s.root}><Text style={s.subtitle}>{t('read_paused')}</Text></SafeAreaView>;
  const current=foreground.active&&visible&&entry&&candidate&&visible.entry===entry.entry_id&&visible.mask===candidate.decision.mask_artifact_id&&visible.override===candidate.decision.override_artifact_id?visible:null;
  return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={{padding:20,gap:12}}>
    <Text style={s.title}>{t('interchange_mask_title')}</Text>
    {(!entries||statuses===null||entry&&!candidates||!journalReady&&!journalFailed)&&!readCancelled?<Button   accessibilityRole="button" disabled={busy} onPress={()=>{for(const controller of reads.current)controller.abort();setReadCancelled(true);setError('interchange_mask_entries_error');if(!journalReady)setJournalFailed(true);}} mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button>:null}
    {statuses?.map(item=><View key={`${item.entry_id}:${item.override_artifact_id}:${item.mask_artifact_id}:${item.target_version_id}`}><Text style={s.subtitle}>{item.entry_id.slice(0,8)} · {item.mask_artifact_id.slice(0,8)} · {t(item.status==='granted'?'interchange_mask_granted':'interchange_mask_revoked')}</Text>{item.status==='granted'?<Button   accessibilityRole="button" disabled={busy||!journalReady||!foreground.active} onPress={()=>void revokeSaved(item)} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_mask_revoke')}</Button>:null}</View>)}
    {!entries&&!readCancelled?<ActivityIndicator accessibilityLabel={t('interchange_mask_loading')}/>:entries?.length===0?<Text style={s.subtitle}>{t('interchange_mask_entries_empty')}</Text>:entries?.map(item=><Button  key={item.entry_id}  accessibilityRole="button" disabled={busy} onPress={()=>{if(mutation.current)return;setEntryID(item.entry_id);setError('');}} mode="contained" contentStyle={{minHeight:48}}>{item.external_id}{item.entry_id===entryID?' ✓':''}</Button>)}
    {entry&&!candidates&&!readCancelled?<ActivityIndicator accessibilityLabel={t('interchange_mask_loading')}/>:null}
    {entry&&candidates?.length===0?<Text style={s.subtitle}>{t('interchange_mask_candidates_empty')}</Text>:null}
    {candidates?.map(item=><Button  key={identity(item)}  accessibilityRole="button" disabled={busy} onPress={()=>{if(mutation.current)return;setSelected(identity(item));setError('');}} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_mask_candidate')} · {item.decision.mask_artifact_id.slice(0,8)}{selected===identity(item)?' ✓':''}</Button>)}
    {candidate?<Button   accessibilityRole="button" disabled={busy||!foreground.active} onPress={()=>{displayedProof.current='';displayedScope.current='';setPhotoLoaded(false);setMaskLoaded(false);setVisible(value=>{value?.dispose();return null;});setError('');setPreviewRevision(value=>value+1);}} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_mask_refresh_preview')}</Button>:null}
    {candidate&&!current?<ActivityIndicator accessibilityLabel={t('interchange_mask_loading')}/>:null}
    {current?<View><Text style={s.subtitle}>{t('interchange_mask_preview')}</Text><View style={{width:'100%',aspectRatio:1,position:'relative',backgroundColor:'#202124'}}><Image key={`${current.proofID}:photo`} accessibilityLabel={t('interchange_mask_photo')} source={{uri:current.photo}} resizeMode="contain" style={{width:'100%',height:'100%'}} onLoad={()=>{if(displayedProof.current===current.proofID&&active.current&&current.expiresAt>Date.now()&&displayedScope.current===`${entryID}:${selected}`)setPhotoLoaded(true);}} onError={()=>{if(displayedProof.current!==current.proofID)return;displayedProof.current='';displayedScope.current='';setVisible(value=>{value?.dispose();return null;});}}/><Image key={`${current.proofID}:mask`} accessibilityLabel={t('interchange_mask_overlay')} source={{uri:current.overlay}} resizeMode="contain" style={{position:'absolute',left:0,right:0,top:0,bottom:0,width:'100%',height:'100%'}} onLoad={()=>{if(displayedProof.current===current.proofID&&active.current&&current.expiresAt>Date.now()&&displayedScope.current===`${entryID}:${selected}`)setMaskLoaded(true);}} onError={()=>{if(displayedProof.current!==current.proofID)return;displayedProof.current='';displayedScope.current='';setVisible(value=>{value?.dispose();return null;});}}/></View>
      <Text style={s.subtitle}>{t('interchange_mask_explain')}</Text>
      {decision?<Text style={s.subtitle} accessibilityLiveRegion="polite">{t(decision==='granted'?'interchange_mask_granted':'interchange_mask_revoked')}</Text>:null}
      <Button   accessibilityRole="button" disabled={busy||!journalReady||!photoLoaded||!maskLoaded} onPress={()=>void decide('grant')} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_mask_grant')}</Button>
      <Button   accessibilityRole="button" disabled={busy||!journalReady||!photoLoaded||!maskLoaded} onPress={()=>void decide('revoke')} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_mask_revoke')}</Button>
    </View>:null}
    {error&&!journalFailed?<Button   accessibilityRole="button" disabled={busy||!foreground.active} onPress={()=>{setReadCancelled(false);setReadRevision(value=>value+1);}} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_mask_reload')}</Button>:null}
    {journalFailed?<Button   accessibilityRole="button" disabled={busy||!foreground.active} onPress={()=>{setReadCancelled(false);setJournalRevision(value=>value+1);setReadRevision(value=>value+1);}} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_mask_reload')}</Button>:null}
    {attempt.current.snapshot()?<Button   accessibilityRole="button" disabled={busy||!journalReady||!foreground.active} onPress={()=>void retryDecision()} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_mask_retry')}</Button>:null}
    {busy?<Button   accessibilityRole="button" onPress={cancelMutation} mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button>:null}
    {error?<Text style={s.error} accessibilityRole="alert">{t(error)}</Text>:null}
  </ScrollView></SafeAreaView>;
}
