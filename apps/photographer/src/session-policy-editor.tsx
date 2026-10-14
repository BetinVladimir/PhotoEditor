import {APIError} from '@photoeditor/api-client';
import {useEffect,useRef,useState} from 'react';
import {Pressable,Text,TextInput,View} from 'react-native';
import type {ShootSession,ShootSessionUpdate} from './capture-types';
import {editSessionPolicy,sessionName} from './session-policy';
import type {SessionPolicyLocal} from './session-policy-draft';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
/** Parent keys this draft by authorized scope/revision and serializes all session mutations. */
export function SessionPolicyEditor({session,disabled,onSave,onDirty,onReadCurrent,onUseCurrent,onLocalDraft,initialLocal}:{initialLocal?:SessionPolicyLocal;session:ShootSession;disabled:boolean;onSave:(request:ShootSessionUpdate,key:string)=>Promise<void>;onDirty:(dirty:boolean)=>void;onLocalDraft?:(local:SessionPolicyLocal|null)=>void;onUseCurrent?:(current:ShootSession)=>void;onReadCurrent?:()=>Promise<ShootSession>}){
 const {t}=useLocale();const [gap,setGap]=useState(initialLocal?.gap??String(session.policy.time_gap_seconds)),[neighbors,setNeighbors]=useState(initialLocal?.neighbors??String(session.policy.neighbor_count));
 const [name,setName]=useState(initialLocal?.name??session.name);
 const [cameraId,setCameraId]=useState(initialLocal?.cameraId??''),[cameraSeconds,setCameraSeconds]=useState(initialLocal?.cameraSeconds??'0');
 const [offsets,setOffsets]=useState(()=>initialLocal?.offsets.map(value=>({...value}))??session.policy.camera_offsets.map(value=>({...value,seconds:String(value.seconds)})));
 const offsetsDirty=JSON.stringify(offsets)!==JSON.stringify(session.policy.camera_offsets.map(value=>({...value,seconds:String(value.seconds)})));
 const [protect,setProtect]=useState(initialLocal?.protect??session.policy.protect_manual_picks);
 const [needsCurrent,setNeedsCurrent]=useState(false);
 const [conflict,setConflict]=useState<ShootSession>();
 const [invalid,setInvalid]=useState(false),[pending,setPending]=useState(false);
 const attempt=useRef<{request:ShootSessionUpdate;key:string}|null>(null),lock=useRef(false),alive=useRef(true);
 const dirty=name!==session.name||cameraId!==''||cameraSeconds!=='0'||offsetsDirty||protect!==session.policy.protect_manual_picks||gap!==String(session.policy.time_gap_seconds)||neighbors!==String(session.policy.neighbor_count);
 useEffect(()=>{onLocalDraft?.(dirty?{name,gap,neighbors,protect,offsets:offsets.map(value=>({...value})),cameraId,cameraSeconds}:null);},[onLocalDraft,dirty,name,gap,neighbors,protect,offsets,cameraId,cameraSeconds]);
 useEffect(()=>{onDirty(dirty);},[dirty,onDirty]);useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 async function save(base=session){
  if(disabled||lock.current||needsCurrent)return;lock.current=true;setPending(true);setInvalid(false);
  try{
   if(!attempt.current&&(cameraId!==''||cameraSeconds!=='0'))throw new Error('SESSION_CAMERA_NOT_ADDED');
   if(!attempt.current)attempt.current={request:{expected_revision:base.revision,name:sessionName(name!==session.name?name:base.name),state:base.state,policy:editSessionPolicy(base.policy,gap,neighbors,protect!==session.policy.protect_manual_picks?protect:undefined,offsetsDirty?offsets:undefined),capture_policy:structuredClone(base.capture_policy)},key:crypto.randomUUID()};
   await onSave(structuredClone(attempt.current.request),attempt.current.key);attempt.current=null;
  }catch(error){
   if(error instanceof APIError&&[409,412].includes(error.status)&&onReadCurrent&&alive.current){setConflict(undefined);setNeedsCurrent(true);try{const current=await onReadCurrent();if(alive.current&&current.id===session.id){setConflict(current);setNeedsCurrent(false);}}catch{/* Parent reports read/access failure; retain the local draft. */}}
   if(error instanceof APIError&&[401,403,404,409,410,412,422].includes(error.status))attempt.current=null;if(alive.current)setInvalid(true);}
  finally{lock.current=false;if(alive.current)setPending(false);}
 }
 async function readConflict(){if(disabled||lock.current||!onReadCurrent)return;lock.current=true;setPending(true);try{const current=await onReadCurrent();if(alive.current&&current.id===session.id){setConflict(current);setNeedsCurrent(false);}}catch{if(alive.current)setInvalid(true);}finally{lock.current=false;if(alive.current)setPending(false);}}
 return <View style={{padding:12,gap:8}}><Text style={s.name}>{t('session_policy')}</Text><TextInput accessibilityLabel={t('capture_session_name')} value={name} maxLength={400} editable={!disabled&&!pending&&!attempt.current} onChangeText={setName} style={s.input}/>
  <Text style={s.meta}>{t('session_time_gap')}</Text><TextInput accessibilityLabel={t('session_time_gap')} keyboardType="numeric" maxLength={4} editable={!disabled&&!pending&&!attempt.current} value={gap} onChangeText={setGap} style={s.input}/>
  <Text style={s.meta}>{t('session_neighbors')}</Text><TextInput accessibilityLabel={t('session_neighbors')} keyboardType="numeric" maxLength={2} editable={!disabled&&!pending&&!attempt.current} value={neighbors} onChangeText={setNeighbors} style={s.input}/>
  <Pressable accessibilityRole="checkbox" accessibilityLabel={t('session_protect_manual')} accessibilityState={{checked:protect,disabled:disabled||pending||!!attempt.current}} disabled={disabled||pending||!!attempt.current} onPress={()=>setProtect(value=>!value)} style={s.button}><Text style={s.buttonText}>{t('session_protect_manual')}</Text></Pressable>
  {offsets.map((offset,index)=><View key={offset.camera_hash}><Text style={s.meta}>{t('session_camera_offset')} {index+1} · {offset.camera_hash.slice(0,8)}</Text><TextInput accessibilityLabel={`${t('session_camera_offset')} ${index+1}`} value={offset.seconds} maxLength={6} editable={!disabled&&!pending&&!attempt.current} onChangeText={seconds=>setOffsets(values=>values.map((value,i)=>i===index?{...value,seconds}:value))} style={s.input}/><Pressable accessibilityRole="button" accessibilityLabel={`${t('session_camera_remove')} ${index+1}`} disabled={disabled||pending||!!attempt.current} onPress={()=>setOffsets(values=>values.filter(value=>value.camera_hash!==offset.camera_hash))} style={s.button}><Text style={s.buttonText}>{t('session_camera_remove')}</Text></Pressable></View>)}
  {offsets.length>=32?<Text style={s.meta}>{t('session_camera_limit')}</Text>:null}
  {cameraId!==''||cameraSeconds!=='0'?<Text style={s.meta}>{t('session_camera_unfinished')}</Text>:null}
  <TextInput accessibilityLabel={t('session_camera_id')} value={cameraId} maxLength={64} editable={!disabled&&!pending&&!attempt.current&&offsets.length<32} onChangeText={setCameraId} style={s.input}/>
  <TextInput accessibilityLabel={t('session_camera_new_offset')} value={cameraSeconds} maxLength={6} editable={!disabled&&!pending&&!attempt.current&&offsets.length<32} onChangeText={setCameraSeconds} style={s.input}/>
  <Pressable accessibilityRole="button" disabled={disabled||pending||!!attempt.current||offsets.length>=32} onPress={()=>{try{const next=[...offsets,{camera_hash:cameraId,seconds:cameraSeconds}];editSessionPolicy(session.policy,gap,neighbors,undefined,next);setOffsets(next);setCameraId('');setCameraSeconds('0');setInvalid(false);}catch{setInvalid(true);}}} style={s.button}><Text style={s.buttonText}>{t('session_camera_add')}</Text></Pressable>
  {invalid?<Text accessibilityRole="alert" style={s.error}>{t('session_policy_failed')}</Text>:null}
  {needsCurrent?<Pressable accessibilityRole="button" disabled={disabled||pending} onPress={()=>void readConflict()} style={s.button}><Text style={s.buttonText}>{t('session_policy_read_current')}</Text></Pressable>:null}
  {conflict?<View style={{gap:6}}><Text style={s.meta}>{t('session_policy_local')}: {gap} / {neighbors}</Text><Text style={s.meta}>{t('session_policy_server')}: {conflict.policy.time_gap_seconds} / {conflict.policy.neighbor_count}</Text><Text>{t('capture_session_name')}: {conflict.name}</Text><Text>{t('session_protect_manual')}: {t(protect?'session_protection_on':'session_protection_off')} / {t(conflict.policy.protect_manual_picks?'session_protection_on':'session_protection_off')}</Text>{conflict.policy.camera_offsets.map(value=><Text key={value.camera_hash}>{t('session_policy_server')} · {value.camera_hash.slice(0,8)}: {value.seconds}</Text>)}<Pressable accessibilityRole="button" disabled={disabled||pending||conflict.state!=='open'} onPress={()=>void save(conflict)} style={s.button}><Text style={s.buttonText}>{t('session_policy_resolve')}</Text></Pressable>{onUseCurrent?<Pressable accessibilityRole="button" disabled={disabled||pending||!!attempt.current} onPress={()=>onUseCurrent(structuredClone(conflict))} style={s.button}><Text style={s.buttonText}>{t('session_policy_use_server')}</Text></Pressable>:null}</View>:null}
  <Pressable accessibilityRole="button" disabled={disabled||pending||needsCurrent||!dirty||!!conflict} onPress={()=>void save()} style={s.button}><Text style={s.buttonText}>{t(attempt.current?'session_policy_retry':'session_policy_save')}</Text></Pressable>
  {!attempt.current&&dirty?<Pressable accessibilityRole="button" disabled={disabled||pending} onPress={()=>{setName(session.name);setGap(String(session.policy.time_gap_seconds));setNeighbors(String(session.policy.neighbor_count));setOffsets(session.policy.camera_offsets.map(value=>({...value,seconds:String(value.seconds)})));setCameraId('');setCameraSeconds('0');setProtect(session.policy.protect_manual_picks);setInvalid(false);setConflict(undefined);setNeedsCurrent(false);}} style={s.button}><Text style={s.buttonText}>{t('session_policy_revert')}</Text></Pressable>:null}
 </View>;
}
