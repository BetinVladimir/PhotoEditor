import {Button,Text,TouchableRipple} from 'react-native-paper';
import {readExportPresetResponse,readPresetMetadataReceipt} from "./export-preset-response";
import {readDeadline} from './read-deadline';
import type {PresetMetadataAttempt,PresetMetadataAttemptStore} from './preset-metadata-attempt-store';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {ExportPreset} from './catalog-types';
import type {OwnerAPI} from './owner-api';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type Props={journal?:PresetMetadataAttemptStore;api:OwnerAPI;preset:ExportPreset;onSaved:(preset:ExportPreset)=>void;onCancel:()=>void};
export function PresetMetadataEditor(props:Props){
 const generation=useMemo(()=>crypto.randomUUID(),[props.api]);
 return <Editor key={`${generation}:${props.preset.id}:${props.preset.revision}`} {...props}/>;
}
function Editor({journal,api,preset,onSaved,onCancel}:Props){
 const {t}=useLocale();
 const [mode,setMode]=useState(preset.config.metadata_policy),[busy,setBusy]=useState(false),[failed,setFailed]=useState(false),[terminal,setTerminal]=useState(false);
 const attempt=useRef<PresetMetadataAttempt|null>(null),conflict=useRef(false);
 const [ready,setReady]=useState(!journal),[recoveryFailed,setRecoveryFailed]=useState(false),[recoveryEpoch,setRecoveryEpoch]=useState(0);
 const bootstrap=useRef<AbortController|null>(null);
 const active=useRef(true),running=useRef(false),controller=useRef<AbortController|null>(null);
 const deadline=useRef<{request:AbortController;timer:ReturnType<typeof setTimeout>}|null>(null);
 function arm(request:AbortController){if(deadline.current)clearTimeout(deadline.current.timer);deadline.current={request,timer:setTimeout(()=>stop(request),15000)}}
 function finish(request:AbortController){if(deadline.current?.request===request){clearTimeout(deadline.current.timer);deadline.current=null}if(controller.current===request){controller.current=null;running.current=false;if(active.current)setBusy(false)}}
 useEffect(()=>{active.current=true;return()=>{active.current=false;controller.current?.abort();if(deadline.current){clearTimeout(deadline.current.timer);deadline.current=null}}},[]);
 useEffect(()=>{if(!journal){setReady(true);return}const request=new AbortController();bootstrap.current=request;setReady(false);setRecoveryFailed(false);void readDeadline(request.signal,async signal=>{const saved=await journal.read();signal.throwIfAborted();attempt.current=saved;if(saved){setMode(saved.mode);setFailed(true)}setReady(true)}).catch(()=>{if(!request.signal.aborted)setRecoveryFailed(true)});return()=>request.abort()},[journal,recoveryEpoch]);
 async function save(){
  if(!ready||running.current||terminal||!active.current)return;
  running.current=true;setBusy(true);setFailed(false);
  const request=new AbortController();controller.current=request;arm(request);
  try{
   if(!attempt.current)attempt.current={version:1,key:crypto.randomUUID(),preset:preset.id,revision:preset.revision,mode,baseline:readExportPresetResponse(preset)};
   const frozen=attempt.current;
   if(journal)await journal.write(frozen);
   request.signal.throwIfAborted();
   const requestPreset=readExportPresetResponse({...preset,revision:frozen.revision});
   const baseline=frozen.baseline??(preset.revision===frozen.revision?readExportPresetResponse(preset):null);
   const result=readPresetMetadataReceipt(await api.changePresetMetadataPolicy(requestPreset,frozen.mode,frozen.key,request.signal,baseline),{id:frozen.preset,revision:frozen.revision,mode:frozen.mode},baseline);
   request.signal.throwIfAborted();
   if(result.id!==frozen.preset||result.revision!==(BigInt(frozen.revision)+1n).toString()||result.config.metadata_policy!==frozen.mode)throw new Error('PRESET_METADATA_RECEIPT_INVALID');
   if(journal)await journal.clearAccepted(frozen,result);
   request.signal.throwIfAborted();
   if(active.current&&!request.signal.aborted)onSaved(result);
  }catch(error){
   if(active.current&&!request.signal.aborted){setFailed(true);if(error instanceof APIError&&[401,403,404,409,410].includes(error.status)){conflict.current=error.status===409&&error.code==='REVISION_CONFLICT';setTerminal(true)}}
  }finally{finish(request)}
 }
 async function close(){if(running.current)return;if(!journal||!conflict.current||!attempt.current){onCancel();return}const request=new AbortController();controller.current=request;running.current=true;setBusy(true);arm(request);try{await journal.discardRejected(attempt.current);request.signal.throwIfAborted();if(active.current)onCancel()}catch{if(active.current&&!request.signal.aborted)setFailed(true)}finally{finish(request)}}
 function stop(request:AbortController|null){if(!request||controller.current!==request)return;request.abort();finish(request);if(active.current)setFailed(true)}
 return <View>{!ready?recoveryFailed?<Button accessibilityRole="button" onPress={()=>setRecoveryEpoch(value=>value+1)}>{t('read_retry')}</Button>:<Button accessibilityRole="button" onPress={()=>{bootstrap.current?.abort();setRecoveryFailed(true)}}>{t('read_cancel')}</Button>:null}{busy?<Button accessibilityRole="button" onPress={()=>stop(controller.current)}>{t('read_cancel')}</Button>:null}<Text style={s.name}>{preset.name}</Text>
 {([['strip','delivery_metadata_strip'],['copyright','delivery_metadata_copyright'],['approved_exif','delivery_metadata_approved']] as const).map(([value,label])=><TouchableRipple key={value} accessibilityRole="radio" accessibilityLabel={t(label)} accessibilityState={{checked:mode===value,disabled:!ready||busy||attempt.current!==null}} aria-checked={mode===value} disabled={!ready||busy||attempt.current!==null} onPress={()=>setMode(value)}><Text>{mode===value?'◉':'○'} {t(label)}</Text></TouchableRipple>)}
 {failed?<Text accessibilityRole="alert">{t(terminal?'preset_metadata_conflict':'preset_metadata_failed')}</Text>:null}
 <Button accessibilityRole="button" disabled={!ready||busy||terminal||!attempt.current&&mode===preset.config.metadata_policy} onPress={()=>void save()}>{t(failed&&!terminal?'preset_metadata_retry':'preset_metadata_save')}</Button>
 <Button accessibilityRole="button" disabled={busy} onPress={()=>void close()}>{t('preset_metadata_cancel')}</Button></View>;
}
