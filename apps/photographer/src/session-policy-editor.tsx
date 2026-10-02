import {APIError} from '@photoeditor/api-client';
import {useEffect,useRef,useState} from 'react';
import {Pressable,Text,TextInput,View} from 'react-native';
import type {ShootSession,ShootSessionUpdate} from './capture-types';
import {editSessionPolicy} from './session-policy';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
/** Parent keys this draft by authorized scope/revision and serializes all session mutations. */
export function SessionPolicyEditor({session,disabled,onSave,onDirty,onReadCurrent}:{session:ShootSession;disabled:boolean;onSave:(request:ShootSessionUpdate,key:string)=>Promise<void>;onDirty:(dirty:boolean)=>void;onReadCurrent?:()=>Promise<ShootSession>}){
 const {t}=useLocale();const [gap,setGap]=useState(String(session.policy.time_gap_seconds)),[neighbors,setNeighbors]=useState(String(session.policy.neighbor_count));
 const [conflict,setConflict]=useState<ShootSession>();
 const [invalid,setInvalid]=useState(false),[pending,setPending]=useState(false);
 const attempt=useRef<{request:ShootSessionUpdate;key:string}|null>(null),lock=useRef(false),alive=useRef(true);
 const dirty=gap!==String(session.policy.time_gap_seconds)||neighbors!==String(session.policy.neighbor_count);
 useEffect(()=>{onDirty(dirty);},[dirty,onDirty]);useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 async function save(base=session){
  if(disabled||lock.current)return;lock.current=true;setPending(true);setInvalid(false);
  try{
   if(!attempt.current)attempt.current={request:{expected_revision:base.revision,name:base.name,state:base.state,policy:editSessionPolicy(base.policy,gap,neighbors),capture_policy:structuredClone(base.capture_policy)},key:crypto.randomUUID()};
   await onSave(structuredClone(attempt.current.request),attempt.current.key);attempt.current=null;
  }catch(error){
   if(error instanceof APIError&&[409,412].includes(error.status)&&onReadCurrent&&alive.current){try{const current=await onReadCurrent();if(alive.current&&current.id===session.id)setConflict(current);}catch{/* Parent reports read/access failure; retain the local draft. */}}
   if(error instanceof APIError&&[401,403,404,409,410,412,422].includes(error.status))attempt.current=null;if(alive.current)setInvalid(true);}
  finally{lock.current=false;if(alive.current)setPending(false);}
 }
 return <View style={{padding:12,gap:8}}><Text style={s.name}>{t('session_policy')}</Text>
  <Text style={s.meta}>{t('session_time_gap')}</Text><TextInput accessibilityLabel={t('session_time_gap')} keyboardType="numeric" maxLength={4} editable={!disabled&&!pending&&!attempt.current} value={gap} onChangeText={setGap} style={s.input}/>
  <Text style={s.meta}>{t('session_neighbors')}</Text><TextInput accessibilityLabel={t('session_neighbors')} keyboardType="numeric" maxLength={2} editable={!disabled&&!pending&&!attempt.current} value={neighbors} onChangeText={setNeighbors} style={s.input}/>
  {invalid?<Text accessibilityRole="alert" style={s.error}>{t('session_policy_failed')}</Text>:null}
  {conflict?<View style={{gap:6}}><Text style={s.meta}>{t('session_policy_local')}: {gap} / {neighbors}</Text><Text style={s.meta}>{t('session_policy_server')}: {conflict.policy.time_gap_seconds} / {conflict.policy.neighbor_count}</Text><Pressable accessibilityRole="button" disabled={disabled||pending||conflict.state!=='open'} onPress={()=>void save(conflict)} style={s.button}><Text style={s.buttonText}>{t('session_policy_resolve')}</Text></Pressable></View>:null}
  <Pressable accessibilityRole="button" disabled={disabled||pending||!dirty||!!conflict} onPress={()=>void save()} style={s.button}><Text style={s.buttonText}>{t(attempt.current?'session_policy_retry':'session_policy_save')}</Text></Pressable>
  {!attempt.current&&dirty?<Pressable accessibilityRole="button" disabled={disabled||pending} onPress={()=>{setGap(String(session.policy.time_gap_seconds));setNeighbors(String(session.policy.neighbor_count));setInvalid(false);setConflict(undefined);}} style={s.button}><Text style={s.buttonText}>{t('session_policy_revert')}</Text></Pressable>:null}
 </View>;
}
