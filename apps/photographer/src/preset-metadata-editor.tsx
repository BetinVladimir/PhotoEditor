import {useEffect,useMemo,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {ExportPreset} from './catalog-types';
import type {OwnerAPI} from './owner-api';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type Props={api:OwnerAPI;preset:ExportPreset;onSaved:(preset:ExportPreset)=>void;onCancel:()=>void};
export function PresetMetadataEditor(props:Props){
 const generation=useMemo(()=>crypto.randomUUID(),[props.api]);
 return <Editor key={`${generation}:${props.preset.id}:${props.preset.revision}`} {...props}/>;
}
function Editor({api,preset,onSaved,onCancel}:Props){
 const {t}=useLocale();
 const [mode,setMode]=useState(preset.config.metadata_policy),[busy,setBusy]=useState(false),[failed,setFailed]=useState(false),[terminal,setTerminal]=useState(false);
 const attempt=useRef<{key:string;mode:ExportPreset['config']['metadata_policy']}|null>(null);
 const active=useRef(true),running=useRef(false),controller=useRef<AbortController|null>(null);
 useEffect(()=>{active.current=true;return()=>{active.current=false;controller.current?.abort()}},[]);
 async function save(){
  if(running.current||terminal||!active.current)return;
  if(!attempt.current)attempt.current={key:crypto.randomUUID(),mode};
  const frozen=attempt.current;running.current=true;setBusy(true);setFailed(false);
  const request=new AbortController();controller.current=request;
  try{
   const result=await api.changePresetMetadataPolicy(preset,frozen.mode,frozen.key,request.signal);
   if(active.current&&!request.signal.aborted)onSaved(result);
  }catch(error){
   if(active.current&&!request.signal.aborted){setFailed(true);if(error instanceof APIError&&[403,404,409].includes(error.status))setTerminal(true)}
  }finally{running.current=false;if(active.current)setBusy(false)}
 }
 return <View><Text style={s.name}>{preset.name}</Text>
 {([['strip','delivery_metadata_strip'],['copyright','delivery_metadata_copyright'],['approved_exif','delivery_metadata_approved']] as const).map(([value,label])=><Pressable key={value} accessibilityRole="radio" accessibilityLabel={t(label)} accessibilityState={{checked:mode===value,disabled:busy||attempt.current!==null}} disabled={busy||attempt.current!==null} onPress={()=>setMode(value)}><Text>{mode===value?'◉':'○'} {t(label)}</Text></Pressable>)}
 {failed?<Text accessibilityRole="alert">{t(terminal?'preset_metadata_conflict':'preset_metadata_failed')}</Text>:null}
 <Pressable accessibilityRole="button" disabled={busy||terminal||mode===preset.config.metadata_policy} onPress={()=>void save()}><Text>{t(failed&&!terminal?'preset_metadata_retry':'preset_metadata_save')}</Text></Pressable>
 <Pressable accessibilityRole="button" disabled={busy} onPress={onCancel}><Text>{t('preset_metadata_cancel')}</Text></Pressable></View>;
}
