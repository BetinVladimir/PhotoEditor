import {ActivityIndicator,Button,Text,TouchableRipple} from 'react-native-paper';
import {useWorkspaceAccess} from './workspace-access-context';
import {executeInterchangeReview} from './interchange-review-controller';
import {xmpMappingDisplay} from './interchange-mapping-display';
import type {InterchangeReviewAttemptStore,InterchangeReviewAttempt} from './interchange-review-attempt-store';
import {useEffect,useMemo,useState,useRef} from 'react';
import {Linking,SafeAreaView,ScrollView,View} from 'react-native';
import {router} from './localized-router';
import {APIError,type ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {InterchangeJob,InterchangeInspection} from './interchange-job';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type JobErrorKey='interchange_error_review_changed_selection'|'interchange_error_review_unavailable'|'interchange_error_review_changed'|'interchange_error_decisions'|'interchange_error_archive_unavailable'|'interchange_error_download'|'interchange_error_close'|'interchange_error_job_unavailable'|'interchange_error_job_load'|'';
type Props={journal?:InterchangeReviewAttemptStore;client:ApiClient;tenant:string;project:string;id:string};
export function InterchangeJobScreen(props:Props){
 const access=useWorkspaceAccess(props.tenant);
 const generation=useMemo(()=>crypto.randomUUID(),[props.client,props.journal,access?.actor,access?.canEdit]);
 return <Scope key={`${generation}:${props.tenant}:${props.project}:${props.id}`} {...props}/>;
}
function Scope({client,tenant,project,id,journal}:Props){
 const __displayValue=useLocale().value;

 const __uiT=useLocale().t;

 const canEdit=useWorkspaceAccess(tenant)?.canEdit===true;
 const {t}=useLocale();
 const [recoveryError,setRecoveryError]=useState<'interchange_recovery_unresolved'|'interchange_review_unavailable'|null>(null);
 const api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [data,setData]=useState<{job:InterchangeJob;inspection:InterchangeInspection}|null>(null);
 const mutation=useRef<AbortController|null>(null),attempt=useRef<{fingerprint:string;key:string}|null>(null);
 const pollGeneration=useRef(0);
 const revoked=useRef(false),mounted=useRef(true);
 const [accessReady,setAccessReady]=useState(false);
 const [journalReady,setJournalReady]=useState(!journal),[recovery,setRecovery]=useState<InterchangeReviewAttempt|null>(null);
 async function executeSaved(record:InterchangeReviewAttempt,requireExisting=false){
  if(!journal||!journalReady||!accessReady||mutation.current||!canEdit&&record.receipt===null)return;
  const controller=new AbortController();mutation.current=controller;pollGeneration.current++;setSaving(true);setDecisionError('');setRecoveryError(null);
  let owned=structuredClone(record);setRecovery(owned);
  try{
   await executeInterchangeReview({api,journal,tenant,project,workflow:id,record:owned,requireExisting,signal:controller.signal,onAttempt:current=>{if(!controller.signal.aborted)setRecovery(current);}});
   if(controller.signal.aborted)return;
   setRecovery(null);setChoices({});setEpoch(value=>value+1);
  }catch(error){if(!controller.signal.aborted){
   if(error instanceof APIError&&[401,403,404,410].includes(error.status)){
    revoked.current=true;setAccessReady(false);setData(null);setChoices({});
    setRecoveryError('interchange_review_unavailable');
   }else setRecoveryError('interchange_recovery_unresolved');
  }}
  finally{if(mutation.current===controller)mutation.current=null;if(mounted.current)setSaving(false);}
 }
 const [choices,setChoices]=useState<Record<string,'approve'|'reject'>>({});
 const decisionAttempt=useRef<{fingerprint:string;key:string}|null>(null),selectionScope=useRef('');
 const [saving,setSaving]=useState(false),[decisionError,setDecisionError]=useState<JobErrorKey>('');
 const scopeFingerprint=data?`${data.inspection.revision}:${data.inspection.review?.report.sha256??''}`:'';
 useEffect(()=>{
  if(scopeFingerprint&&scopeFingerprint!==selectionScope.current){
   if(selectionScope.current&&Object.keys(choices).length)setDecisionError('interchange_error_review_changed_selection');
   selectionScope.current=scopeFingerprint;setChoices({});
  }
 },[scopeFingerprint]);
 function choose(entry:string,decision:'approve'|'reject'){
  if(mutation.current||!journalReady||recovery)return;
  setChoices(current=>{const next={...current};if(next[entry]===decision)delete next[entry];else next[entry]=decision;return next;});
 }
 async function saveDecisions(){
  if(!canEdit||!data?.inspection.review?.can_decide||mutation.current||!journalReady||recovery)return;
  const inspection=structuredClone(data.inspection),decisions=(inspection.review!.targets??[]).filter(t=>choices[t.entry_id]).map(t=>({entry_id:t.entry_id,decision:choices[t.entry_id]}));
  if(!decisions.length)return;
  if(journal){await executeSaved({version:1,kind:'decide',key:crypto.randomUUID(),inspection,decisions,receipt:null});return;}
  const fingerprint=JSON.stringify({revision:inspection.revision,report:inspection.review!.report.sha256,decisions});
  if(decisionAttempt.current?.fingerprint!==fingerprint)decisionAttempt.current={fingerprint,key:crypto.randomUUID()};
  const controller=new AbortController();mutation.current=controller;pollGeneration.current++;setSaving(true);setDecisionError('');
  try{await api.decideInterchangeReview(project,inspection,decisions,decisionAttempt.current!.key,controller.signal);if(!controller.signal.aborted){setChoices({});setEpoch(value=>value+1);}}
  catch(error){if(!controller.signal.aborted){setDecisionError(error instanceof APIError&&[401,403,404,410].includes(error.status)?'interchange_error_review_unavailable':error instanceof APIError&&error.status===409?'interchange_error_review_changed':'interchange_error_decisions');if(error instanceof APIError&&[401,403,404,410].includes(error.status))setData(null);}}
  finally{if(mutation.current===controller)mutation.current=null;if(mounted.current)setSaving(false);}
 }
 const [downloading,setDownloading]=useState(false),[downloadError,setDownloadError]=useState<JobErrorKey>('');
 async function downloadArchive(){
  if(mutation.current)return;
  const controller=new AbortController();mutation.current=controller;pollGeneration.current++;setDownloading(true);setDownloadError('');
  try{
   const output=await api.interchangeDownload(project,id,controller.signal);
   if(!controller.signal.aborted)await Linking.openURL(output.url);
  }catch(error){if(!controller.signal.aborted){
   setDownloadError(error instanceof APIError&&[401,403,404,410].includes(error.status)?'interchange_error_archive_unavailable':'interchange_error_download');
   setData(null);
  }}finally{if(mutation.current===controller)mutation.current=null;if(mounted.current)setDownloading(false);}
 }
 const [closing,setClosing]=useState(false),[closeError,setCloseError]=useState<JobErrorKey>('');
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;mutation.current?.abort();};},[]);
 async function closeReview(){
  if(!canEdit||!data?.inspection.review?.can_close||mutation.current||!journalReady||recovery)return;
  const inspection=structuredClone(data.inspection),fingerprint=`${inspection.revision}:${inspection.review!.report.sha256}`;
  if(journal){await executeSaved({version:1,kind:'close',key:crypto.randomUUID(),inspection,decisions:[],receipt:null});return;}
  if(attempt.current?.fingerprint!==fingerprint)attempt.current={fingerprint,key:crypto.randomUUID()};
  const controller=new AbortController();mutation.current=controller;pollGeneration.current++;setClosing(true);setCloseError('');
  try{await api.closeInterchangeReview(project,inspection,attempt.current!.key,controller.signal);if(!controller.signal.aborted)setEpoch(value=>value+1);}
  catch(error){if(!controller.signal.aborted){setCloseError(error instanceof APIError&&[401,403,404,410].includes(error.status)?'interchange_error_review_unavailable':error instanceof APIError&&error.status===409?'interchange_error_review_changed':'interchange_error_close');if(error instanceof APIError&&[401,403,404,410].includes(error.status))setData(null);}}
  finally{if(mutation.current===controller)mutation.current=null;if(mounted.current)setClosing(false);}
 }
 const [error,setError]=useState<JobErrorKey>(''),[busy,setBusy]=useState(true),[epoch,setEpoch]=useState(0);
 useEffect(()=>{
  const controller=new AbortController();let timer:ReturnType<typeof setTimeout>|undefined;
  revoked.current=false;setAccessReady(false);setData(null);setError('');setDownloadError('');setBusy(true);
  async function load(){
   if(revoked.current)return;
   if(mutation.current){timer=setTimeout(load,5000);return;}
   const generation=pollGeneration.current;
   try{
    if(journal){
     journal.assertScope(tenant,project,id);const saved=await journal.read();if(controller.signal.aborted)return;
     if(generation!==pollGeneration.current||mutation.current){timer=setTimeout(load,5000);return;}
     setRecovery(saved);setJournalReady(true);
    }
    const {job,inspection}=await api.interchangeSnapshot(project,id,controller.signal);
    if(controller.signal.aborted)return;
    if(generation!==pollGeneration.current||mutation.current){timer=setTimeout(load,5000);return;}
    setData({job,inspection});setAccessReady(true);setError('');
    if(!['completed','completed_with_errors','failed','cancelled'].includes(inspection.status))timer=setTimeout(load,5000);
   }catch(e){
    if(controller.signal.aborted)return;
    if(generation!==pollGeneration.current||mutation.current){timer=setTimeout(load,5000);return;}
    setAccessReady(false);setData(null);setError(e instanceof APIError&&[401,403,404,410].includes(e.status)?'interchange_error_job_unavailable':'interchange_error_job_load');
    setBusy(false);controller.abort();if(timer!==undefined)clearTimeout(timer);
   }finally{if(!controller.signal.aborted)setBusy(false);}
  }
  void load();return()=>{controller.abort();if(timer!==undefined)clearTimeout(timer);};
 },[api,project,id,epoch]);
 const labels:Record<string,string>={queued:t('interchange_stage_queued'),parsing:t('interchange_stage_parsing'),ingesting:t('interchange_stage_ingesting'),preparing_import:t('interchange_stage_preparing_import'),exporting:t('interchange_stage_exporting'),finalizing:t('interchange_stage_finalizing'),review:t('interchange_stage_review'),completed:t('interchange_stage_completed'),completed_with_errors:t('interchange_stage_completed_with_errors'),failed:t('interchange_stage_failed'),cancelled:t('interchange_stage_cancelled'),cancelling:t('interchange_stage_cancelling')};
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={{padding:20,gap:12}}>
  <Text style={s.title}>{t('interchange_job_title')}</Text>
  {busy&&<ActivityIndicator accessibilityLabel={t('interchange_job_loading')}/>}
  {error!==''&&<Text style={s.error} accessibilityRole="alert">{t(error)}</Text>}
  {(saving||closing||downloading)&&<Button   accessibilityRole="button" onPress={()=>mutation.current?.abort()} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_review_stop')}</Button>}
  {recovery&&<View><Text>{t('interchange_recovery_pending')}</Text><Button accessibilityRole="button" disabled={busy||saving||closing||downloading||!journalReady||!accessReady||!canEdit&&recovery.receipt===null} onPress={()=>void executeSaved(recovery,true)}>{t('interchange_recovery_retry')}</Button></View>}
  {recoveryError&&<Text style={s.error} accessibilityRole="alert">{t(recoveryError)}</Text>}
  {decisionError!==''&&<Text style={s.error} accessibilityRole="alert">{t(decisionError)}</Text>}
  {downloadError!==''&&<Text style={s.error} accessibilityRole="alert">{t(downloadError)}</Text>}
  {closeError!==''&&<Text style={s.error} accessibilityRole="alert">{t(closeError)}</Text>}
  {data&&<>
   <Text style={s.subtitle}>{t(data.job.direction==='import'?'interchange_import':'interchange_export')} · {__displayValue(data.job.format)}</Text>
   <Text style={s.subtitle} accessibilityLiveRegion="polite">{labels[data.inspection.stage]}</Text>
   <Button   accessibilityRole="button" onPress={()=>router.push({pathname:'/runs/[id]',params:{id:data.inspection.run_id,tenant}})} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_open_run')}</Button>
   {data.job.direction==='import'&&<Button   accessibilityRole="button" onPress={()=>router.push({pathname:'/interchange/masks/[id]',params:{id,tenant,project}})} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_mask_open')}</Button>}
   {data.job.direction==='import'&&<Button   accessibilityRole="button" onPress={()=>router.push({pathname:'/interchange/prepared/[id]',params:{id,tenant,project}})} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_prepared_open')}</Button>}
   {data.job.direction==='export'&&data.job.bundle_artifact_id&&data.inspection.status==='completed'&&<Button   accessibilityRole="button" disabled={downloading||saving||closing} onPress={()=>void downloadArchive()} mode="contained" contentStyle={{minHeight:48}}>{t(downloading?'interchange_download_preparing':'interchange_download_archive')}</Button>}
   {data.inspection.collection_id&&<Button   accessibilityRole="button" onPress={()=>router.push({pathname:'/collections/[id]',params:{id:data.inspection.collection_id!,tenant,project}})} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_open_collection')}</Button>}
   {data.inspection.review&&<Text style={s.subtitle}>{data.inspection.review.proposed_count} {t('interchange_awaiting_decision')}</Text>}
   {data.inspection.review?.targets?.map(target=><View key={target.entry_id} style={{gap:8}}>
    <Text style={s.subtitle}>{target.external_id}</Text>
    {target.fields.map(field=><Text key={field.name}>{xmpMappingDisplay(field,t)??`${field.name}: ${field.status}${field.reason?` — ${field.reason}`:''}`}</Text>)}
    {canEdit&&data.inspection.review?.can_decide&&<View style={{flexDirection:'row',gap:8}}>
     <TouchableRipple  style={s.button} accessibilityRole="checkbox" accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(t('interchange_approve')),p1:(target.external_id)})} accessibilityState={{checked:choices[target.entry_id]==='approve',disabled:saving||closing||!journalReady||!!recovery}} aria-checked={choices[target.entry_id]==='approve'} disabled={saving||closing||!journalReady||!!recovery} onPress={()=>choose(target.entry_id,'approve')}><Text style={s.buttonText}>{__uiT('ui_380e3dbd6730', {p0:(choices[target.entry_id]==='approve'?'✓ ':''),p1:(t('interchange_approve'))})}</Text></TouchableRipple>
     <TouchableRipple  style={s.button} accessibilityRole="checkbox" accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(t('interchange_reject')),p1:(target.external_id)})} accessibilityState={{checked:choices[target.entry_id]==='reject',disabled:saving||closing||!journalReady||!!recovery}} aria-checked={choices[target.entry_id]==='reject'} disabled={saving||closing||!journalReady||!!recovery} onPress={()=>choose(target.entry_id,'reject')}><Text style={s.buttonText}>{__uiT('ui_380e3dbd6730', {p0:(choices[target.entry_id]==='reject'?'✓ ':''),p1:(t('interchange_reject'))})}</Text></TouchableRipple>
    </View>}
   </View>)}
   {canEdit&&data.inspection.review?.can_decide&&<Button   accessibilityRole="button" disabled={saving||closing||!journalReady||!!recovery||!Object.keys(choices).length} onPress={()=>void saveDecisions()} mode="contained" contentStyle={{minHeight:48}}>{saving?t('interchange_saving_decisions'):__uiT('ui_ff46a851b217', {p0:(t('interchange_save_decisions')),p1:(Object.keys(choices).length)})}</Button>}
   {canEdit&&data.inspection.review?.can_close&&<Button   accessibilityRole="button" disabled={closing||saving||!journalReady||!!recovery} onPress={()=>void closeReview()} mode="contained" contentStyle={{minHeight:48}}>{t(closing?'interchange_closing_review':'interchange_close_review')}</Button>}
   {!data.job.report_artifact_id&&!data.inspection.review&&<Text style={s.subtitle}>{t('interchange_mapping_pending')}</Text>}
  </>}
  <Button   accessibilityRole="button" disabled={busy||saving||closing||downloading} onPress={()=>setEpoch(value=>value+1)} mode="contained" contentStyle={{minHeight:48}}>{t('interchange_refresh')}</Button>
 </ScrollView></SafeAreaView>;
}
