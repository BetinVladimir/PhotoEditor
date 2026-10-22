import {Button,Text,TextInput,TouchableRipple} from 'react-native-paper';
import {useWorkspaceAccess} from '../../src/workspace-access-context';
import {APIError} from '@photoeditor/api-client';
import {PresetMetadataAttemptStore} from '../../src/preset-metadata-attempt-store';
import {readDeadline} from '../../src/read-deadline';
import {ExportWatermarkPicker} from '../../src/export-watermark-picker';
import {ExportBatchPanel} from '../../src/export-batch-panel';
import {ExportPresetForm} from '../../src/export-preset-form';
import {useReviewForeground} from '../../src/use-review-foreground';
import {PresetCreateAttemptStore,type PresetCreateAttempt} from '../../src/preset-create-attempt-store';
import AsyncStorage from "@react-native-async-storage/async-storage";
import {DownloadJobStore,type DownloadStartAttempt} from "../../src/download-job-store";
import {DownloadJobPanel} from "../../src/download-job-panel";
import {useEffect,useMemo,useRef,useState} from 'react';
import {useLocalSearchParams} from '../../src/localized-router';
import {FlatList,SafeAreaView,View} from 'react-native';
import {PresetMetadataEditor} from '../../src/preset-metadata-editor';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import type {ExportPreset} from '../../src/catalog-types';
import {useLocale} from '../../src/locale-context';
import {catalogStyle as s} from '../../src/catalog-style';

const config:ExportPreset['config']={format:'jpeg',icc_id:'srgb_v1',quality:92,subsampling:'444',alpha:'matte',matte_rgb:[1,1,1],metadata_policy:'strip',naming_template:'{original_name}_edited'};
type PresetPage={items:ExportPreset[];next_cursor:string|null};
function readPresetPage(value:unknown,existing:ReadonlySet<string>,usedCursors:ReadonlySet<string>,requested?:string):PresetPage{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('EXPORT_PRESET_PAGE_INVALID');
 const page=value as Record<string,unknown>;
 if(!Array.isArray(page.items)||page.items.length>100||!(page.next_cursor===null||typeof page.next_cursor==='string'&&page.next_cursor.length>0&&page.next_cursor.length<=4096)||page.items.length===0&&page.next_cursor!==null)throw new Error('EXPORT_PRESET_PAGE_INVALID');
 const ids=new Set(existing);
 for(const item of page.items){
  if(!item||typeof item!=='object'||typeof item.id!=='string'||!item.id||ids.has(item.id))throw new Error('EXPORT_PRESET_PAGE_INVALID');
  ids.add(item.id);
 }
 if(page.next_cursor&&(page.next_cursor===requested||usedCursors.has(page.next_cursor)))throw new Error('EXPORT_PRESET_CURSOR_REPEATED');
 return {items:page.items as ExportPreset[],next_cursor:page.next_cursor as string|null};
}

export default function Exports(){
 const foreground=useReviewForeground();
 const {tenant,snapshot}=useLocalSearchParams<{tenant:string;snapshot?:string}>(),auth=useAuth();
 const workspace=useWorkspaceAccess(tenant||'');
 const client=useMemo(()=>auth.client(),[auth]);
 const api=useMemo(()=>new OwnerAPI(client,tenant||''),[client,tenant]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 if(!foreground.active)return <SafeAreaView style={s.root}/>;
 return <ScopedExports key={JSON.stringify([generation,auth.origin,tenant,snapshot,foreground.generation,workspace?.actor,workspace?.canEdit])} api={api} snapshot={snapshot} origin={auth.origin} tenant={tenant||''}/>;
}

function ScopedExports({api,snapshot,origin,tenant}:{api:OwnerAPI;snapshot?:string;origin:string;tenant:string}){
 const __displayValue=useLocale().value;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const canEdit=useWorkspaceAccess(tenant)?.canEdit===true;
 const {t}=useLocale();
 const [actorID,setActorID]=useState<string|null>(null),[denied,setDenied]=useState(false);
 const [downloadJobID,setDownloadJobID]=useState<string|null>(null);
 const [pendingDownload,setPendingDownload]=useState<DownloadStartAttempt|null>(null);
 const startFlight=useRef<AbortController|null>(null);
 const jobStore=useRef<DownloadJobStore|null>(null);
 const presetStore=useRef<PresetCreateAttemptStore|null>(null),presetFlight=useRef<AbortController|null>(null);
 const [pendingPreset,setPendingPreset]=useState<PresetCreateAttempt|null>(null);
 const [jobRecoveryReady,setJobRecoveryReady]=useState(false),[recoveryEpoch,setRecoveryEpoch]=useState(0),[recoveryFailed,setRecoveryFailed]=useState(false);
 const recoveryFlight=useRef<AbortController|null>(null),initialPage=useRef<AbortController|null>(null),beginFlight=useRef<AbortController|null>(null);
 useEffect(()=>{
  const controller=new AbortController();recoveryFlight.current=controller;jobStore.current=null;setRecoveryFailed(false);setJobRecoveryReady(false);
  void readDeadline(controller.signal,async signal=>{const actor=await api.me(signal);signal.throwIfAborted();const store=new DownloadJobStore(AsyncStorage,origin,actor.id,tenant);const saved=await store.read();const pending=await store.readAttempt();signal.throwIfAborted();const authoring=new PresetCreateAttemptStore(AsyncStorage,origin,actor.id,tenant,tenant);const savedPreset=await authoring.read();signal.throwIfAborted();setActorID(actor.id);presetStore.current=authoring;setPendingPreset(savedPreset);setPendingDownload(pending);jobStore.current=store;setDownloadJobID(saved);setJobRecoveryReady(true)}).catch(error=>{if(!controller.signal.aborted&&!revoke(error)){setRecoveryFailed(true);setNotice('Unable to restore download job')}});
  return()=>{controller.abort();startFlight.current?.abort();startFlight.current=null;beginFlight.current?.abort();beginFlight.current=null;jobStore.current=null;presetFlight.current?.abort();presetFlight.current=null;presetStore.current=null};
 },[api,origin,tenant,recoveryEpoch]);

 const [presetConfig,setPresetConfig]=useState<ExportPreset['config']>(config);
 const [metadataPolicy,setMetadataPolicy]=useState<ExportPreset['config']['metadata_policy']>('strip');
 const [reload,setReload]=useState(0),[editing,setEditing]=useState<ExportPreset|null>(null);
 const metadataJournal=useMemo(()=>actorID&&editing?new PresetMetadataAttemptStore(AsyncStorage,origin,actorID,tenant,editing.id):undefined,[actorID,origin,tenant,editing?.id]);
 const [items,setItems]=useState<ExportPreset[]>([]),[cursor,setCursor]=useState<string|null>(null);
 const [name,setName]=useState('Web JPEG'),[busy,setBusy]=useState(false),[pageLoading,setPageLoading]=useState(true),[notice,setNotice]=useState('');
 const pageFlight=useRef<AbortController|null>(null),usedCursors=useRef(new Set<string>());
 useEffect(()=>{
  let active=true;const controller=new AbortController();initialPage.current=controller;
  pageFlight.current?.abort();pageFlight.current=null;usedCursors.current.clear();
  setEditing(null);setItems([]);setCursor(null);setNotice('');setPageLoading(true);
  void readDeadline(controller.signal,signal=>api.presets(undefined,signal)).then(value=>{
   if(!active||controller.signal.aborted)return;
   const page=readPresetPage(value,new Set(),usedCursors.current);
   setItems(page.items);setCursor(page.next_cursor);
  }).catch(error=>{if(active&&!controller.signal.aborted&&!revoke(error))setNotice('Unable to load presets');}).finally(()=>{if(active&&!controller.signal.aborted)setPageLoading(false)});
  return()=>{active=false;controller.abort();pageFlight.current?.abort();pageFlight.current=null};
 },[api,reload]);
 async function loadMore(){
  if(!cursor||pageFlight.current||busy)return;
  const requested=cursor,controller=new AbortController();pageFlight.current=controller;setPageLoading(true);setNotice('');
  try{
   if(usedCursors.current.has(requested))throw new Error('EXPORT_PRESET_CURSOR_REPEATED');
   const value=await readDeadline(controller.signal,signal=>api.presets(requested,signal));
   if(controller.signal.aborted)return;
   const page=readPresetPage(value,new Set(items.map(item=>item.id)),usedCursors.current,requested);
   usedCursors.current.add(requested);setItems(prior=>[...prior,...page.items]);setCursor(page.next_cursor);
  }catch(error){if(!controller.signal.aborted&&!revoke(error))setNotice('Unable to load presets')}
  finally{if(pageFlight.current===controller){pageFlight.current=null;setPageLoading(false)}}
 }
 async function create(){
  const store=presetStore.current;if(!canEdit||!store||presetFlight.current||busy)return;
  const controller=new AbortController();presetFlight.current=controller;setBusy(true);
  let frozen:PresetCreateAttempt|null=pendingPreset;
  try{
   frozen=frozen??await store.read();controller.signal.throwIfAborted();
   if(!frozen){if(!name.trim()||Array.from(name.trim()).length>120)throw new Error('PRESET_NAME_INVALID');frozen={schema_version:1,tenant_scope:tenant,key:crypto.randomUUID(),request:{name:name.trim(),config:{...presetConfig,metadata_policy:metadataPolicy}},ticket:null};setPendingPreset(frozen);}
   await store.write(frozen);controller.signal.throwIfAborted();
   if(presetStore.current!==store)return;setPendingPreset(frozen);
   const preset=await api.createPreset(frozen.request.name,frozen.request.config,frozen.key,controller.signal);
   controller.signal.throwIfAborted();if(presetStore.current!==store)return;
   await store.write({...frozen,ticket:{preset_id:preset.id}});controller.signal.throwIfAborted();
   await store.clearAccepted(frozen.key,preset.id);controller.signal.throwIfAborted();if(presetStore.current!==store)return;
   setPendingPreset(null);setItems(values=>[preset,...values.filter(value=>value.id!==preset.id)]);setNotice('');
  }catch(error){if(!controller.signal.aborted&&presetStore.current===store&&!revoke(error)){
   if(frozen&&frozen.ticket===null&&error instanceof APIError&&error.status===422&&error.code==='VALIDATION_FAILED'){
    try{await store.discardRejected(frozen.key);controller.signal.throwIfAborted();if(presetStore.current===store)setPendingPreset(null)}catch{/* retain exact intent if durable rejection cleanup failed */}
   }
   if(!controller.signal.aborted&&presetStore.current===store)setNotice('Unable to create preset');
  }}
  finally{if(presetFlight.current===controller){presetFlight.current=null;setBusy(false)}}
 }
 async function start(preset:ExportPreset){
  if(!jobRecoveryReady||!jobStore.current||beginFlight.current||startFlight.current||busy)return;
  if(preset.config.watermark||preset.config.sharpen){setNotice('Use a delivery package for watermark and output sharpening.');return}
  if(preset.config.metadata_policy!=='strip'){setNotice(t('delivery_metadata_delivery_only'));return}
  if(!snapshot){setNotice('Open exports from a frozen selection snapshot.');return}
  const current=jobStore.current,controller=new AbortController();beginFlight.current=controller;setBusy(true);
  try{const attempt=await current.begin(snapshot,preset.id);controller.signal.throwIfAborted();if(jobStore.current!==current)return;setPendingDownload(attempt);beginFlight.current=null;setBusy(false);await submitDownload(attempt)}catch{if(!controller.signal.aborted&&jobStore.current===current)setNotice('Unable to start download job')}finally{if(beginFlight.current===controller){beginFlight.current=null;setBusy(false)}}
 }
 async function submitDownload(attempt:DownloadStartAttempt){
  const store=jobStore.current;if(!store||presetFlight.current||startFlight.current)return;
  const controller=new AbortController();startFlight.current=controller;setBusy(true);
  try{
   const out=await api.startDownload(attempt.snapshot,attempt.preset,attempt.key,controller.signal);
   controller.signal.throwIfAborted();if(jobStore.current!==store)return;
   await store.complete(attempt,out.download_job_id);
   controller.signal.throwIfAborted();if(jobStore.current!==store)return;
   setPendingDownload(null);setDownloadJobID(out.download_job_id);setNotice(`Download job ${out.download_job_id.slice(0,8)} is ${out.status}.`);
  }catch(error){
   if(!controller.signal.aborted&&jobStore.current===store&&!revoke(error)){
    try{if(await store.refuse(attempt,error)){controller.signal.throwIfAborted();if(jobStore.current===store)setPendingDownload(null)}}catch{/* preserve unresolved receipt for explicit retry */}
    if(!controller.signal.aborted&&jobStore.current===store)setNotice('Unable to start download job');
   }
  }
  finally{if(startFlight.current===controller){startFlight.current=null;setBusy(false)}}
 }

 function revoke(error:unknown){if(!(error instanceof APIError)||![401,403,404,410].includes(error.status))return false;recoveryFlight.current?.abort();initialPage.current?.abort();pageFlight.current?.abort();beginFlight.current?.abort();startFlight.current?.abort();presetFlight.current?.abort();jobStore.current=null;presetStore.current=null;setDenied(true);setJobRecoveryReady(false);setItems([]);setCursor(null);setEditing(null);setActorID(null);setPendingPreset(null);setPendingDownload(null);setDownloadJobID(null);setBusy(false);setPageLoading(false);return true}
 function cancelWork(){beginFlight.current?.abort();beginFlight.current=null;startFlight.current?.abort();startFlight.current=null;presetFlight.current?.abort();presetFlight.current=null;setBusy(false);setNotice('Unable to finish request. Retry preserves the saved intent.');}
 useEffect(()=>{if(!busy)return;const timer=setTimeout(cancelWork,15000);return()=>clearTimeout(timer)},[busy]);

 if(denied)return <SafeAreaView style={s.root}><Text accessibilityRole="alert">{t('delivery_access_lost')}</Text><Button accessibilityRole="button" onPress={()=>{setDenied(false);setRecoveryEpoch(value=>value+1);setReload(value=>value+1)}}>{t('read_retry')}</Button></SafeAreaView>;
 return <SafeAreaView style={s.root}><View style={s.header}>
  <Text style={s.title}>{__uiT('ui_2f4dcf59ff0c')}</Text>
  {!jobRecoveryReady?recoveryFailed?<Button accessibilityRole="button" onPress={()=>setRecoveryEpoch(value=>value+1)}>{t('read_retry')}</Button>:<Button accessibilityRole="button" onPress={()=>{recoveryFlight.current?.abort();setRecoveryFailed(true)}}>{t('read_cancel')}</Button>:null}
  {pageLoading?<Button accessibilityRole="button" onPress={()=>{initialPage.current?.abort();pageFlight.current?.abort();pageFlight.current=null;setPageLoading(false);setNotice('Unable to load presets')}}>{t('read_cancel')}</Button>:null}
  {busy?<Button accessibilityRole="button" onPress={cancelWork}>{t('read_cancel')}</Button>:null}
  <Button accessibilityRole="button" disabled={busy} onPress={()=>setReload(value=>value+1)}>{t('preset_metadata_reload')}</Button>
  <View style={s.toolbar}><TextInput editable={canEdit&&!pendingPreset&&!busy} value={name} onChangeText={setName} placeholder={__uiT('ui_e534a6662ac8')} placeholderTextColor="#888" style={s.input}/><Button  accessibilityRole="button" disabled={!canEdit||busy||pageLoading||!jobRecoveryReady||!pendingPreset&&!name.trim()} onPress={create}  mode="contained" contentStyle={{minHeight:48}}>{pendingPreset?__uiT('ui_660bd844487a'):__uiT('ui_4759498ac2a7')}</Button></View>
  <ExportWatermarkPicker api={api} value={presetConfig.watermark?.asset_id} disabled={!canEdit||busy||!!pendingPreset} onChange={asset_id=>setPresetConfig(current=>({...current,watermark:asset_id?{asset_id,opacity:current.watermark?.opacity??.5,anchor:current.watermark?.anchor??'bottom_right',margin_x:current.watermark?.margin_x??24,margin_y:current.watermark?.margin_y??24}:undefined}))}/>
  <ExportPresetForm value={presetConfig} onChange={setPresetConfig} disabled={!canEdit||busy||!!pendingPreset}/>
  <Text style={s.subtitle}>{t('delivery_metadata_title')}</Text>
  {([['strip','delivery_metadata_strip'],['copyright','delivery_metadata_copyright'],['approved_exif','delivery_metadata_approved']] as const).map(([mode,label])=><TouchableRipple  key={mode} accessibilityRole="radio" accessibilityLabel={t(label)} accessibilityState={{checked:metadataPolicy===mode,disabled:!canEdit||busy||!!pendingPreset}} aria-checked={metadataPolicy===mode} disabled={!canEdit||busy||!!pendingPreset} onPress={()=>setMetadataPolicy(mode)}><Text style={s.name}>{metadataPolicy===mode?'◉':'○'} {t(label)}</Text></TouchableRipple>)}
  <Text style={s.meta}>{t('delivery_metadata_delivery_only')}</Text>
  {notice?<Text accessibilityRole={notice.startsWith('Unable')?'alert':undefined} style={notice.startsWith('Unable')?s.error:s.subtitle}>{__uiMessage(notice)}</Text>:null}
  </View>
  {canEdit&&snapshot?<ExportBatchPanel api={api} origin={origin} tenant={tenant} snapshot={snapshot} presets={items}/>:null}
  {pendingDownload?<Button accessibilityRole="button" disabled={busy||!jobRecoveryReady} onPress={()=>void submitDownload(pendingDownload)}>{__uiT('ui_7c26824867c8')}</Button>:null}
  {downloadJobID?<DownloadJobPanel key={downloadJobID} api={api} id={downloadJobID}/>:null}
  {canEdit&&editing?<PresetMetadataEditor journal={metadataJournal} api={api} preset={editing} onCancel={()=>{setEditing(null);setReload(value=>value+1)}} onSaved={updated=>{setItems(values=>values.map(value=>value.id===updated.id?updated:value));setEditing(null)}}/>:null}
  <FlatList data={items} keyExtractor={item=>item.id} contentContainerStyle={s.list}
   ListEmptyComponent={!pageLoading&&!notice.startsWith('Unable')?<Text style={s.empty}>{__uiT('ui_a260f7d518ad')}</Text>:null}
   renderItem={({item})=><View><TouchableRipple accessibilityRole="button" disabled={busy||pageLoading} onPress={()=>start(item)} style={s.row}><View style={{flex:1,flexDirection:'row',alignItems:'center',justifyContent:'space-between'}}><View><Text style={s.name}>{item.name}</Text><Text style={s.meta}>{item.config.format.toUpperCase()} · {__displayValue(item.config.icc_id)} · {t(item.config.metadata_policy==='copyright'?'delivery_metadata_copyright':item.config.metadata_policy==='approved_exif'?'delivery_metadata_approved':'delivery_metadata_strip')}</Text></View><Text style={s.chevron}>›</Text></View></TouchableRipple><Button accessibilityRole="button" disabled={!canEdit||busy||pageLoading||!jobRecoveryReady} onPress={()=>setEditing(item)}>{t('preset_metadata_edit')}</Button></View>}
   ListFooterComponent={cursor?<Button  accessibilityRole="button" disabled={busy||pageLoading} onPress={()=>void loadMore()}  mode="contained" contentStyle={{minHeight:48}}>{t('delivery_create_more')}</Button>:null}/>
 </SafeAreaView>;
}
