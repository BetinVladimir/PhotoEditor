import {useEffect,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {ShootSession,ShootSessionUpdate} from './capture-types';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
/** CAS state transitions preserve policy and use one immutable request for uncertain retries. */
export function SessionLifecycle({session,disabled,onSave,onPending}:{onPending:(pending:boolean)=>void;session:ShootSession;disabled:boolean;onSave:(request:ShootSessionUpdate,key:string)=>Promise<void>}){
 const {t}=useLocale(),lock=useRef(false),alive=useRef(true),attempt=useRef<{request:ShootSessionUpdate;key:string}|null>(null);
 const [pending,setPending]=useState(false),[failed,setFailed]=useState(false),[conflict,setConflict]=useState(false),[confirm,setConfirm]=useState(false);
 async function transition(state?:ShootSession['state']){
  if(disabled||lock.current||conflict)return;lock.current=true;onPending(true);setPending(true);setFailed(false);
  try{if(!attempt.current){if(!state||!((session.state==='open'&&state==='closed')||(session.state==='closed'&&(state==='open'||state==='archived'))))throw new Error('SESSION_STATE_INVALID');attempt.current={request:{expected_revision:session.revision,name:session.name,state,policy:structuredClone(session.policy),capture_policy:structuredClone(session.capture_policy)},key:crypto.randomUUID()};}
   await onSave(structuredClone(attempt.current.request),attempt.current.key);if(!alive.current)return;attempt.current=null;
  }catch(e){if(!alive.current)return;if(e instanceof APIError&&[401,403,404,409,410,412,422].includes(e.status)){attempt.current=null;setConflict(true);}setFailed(true);}finally{lock.current=false;if(alive.current){onPending(!!attempt.current);setPending(false);}}
 }
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;onPending(false);};},[onPending]);
 const blocked=disabled||pending||conflict||!!attempt.current;
 return <View style={{padding:12,gap:8}}>{failed?<Text accessibilityRole="alert">{t(conflict?'session_state_conflict':'session_state_failed')}</Text>:null}
  {attempt.current?<Pressable accessibilityRole="button" disabled={disabled||pending} onPress={()=>void transition()} style={s.button}><Text style={s.buttonText}>{t('session_state_retry')}</Text></Pressable>:null}
  {session.state==='open'?<Pressable accessibilityRole="button" disabled={blocked} onPress={()=>void transition('closed')} style={s.button}><Text style={s.buttonText}>{t('session_close')}</Text></Pressable>:null}
  {session.state==='closed'?<><Pressable accessibilityRole="button" disabled={blocked} onPress={()=>void transition('open')} style={s.button}><Text style={s.buttonText}>{t('session_reopen')}</Text></Pressable>
   <Pressable accessibilityRole="button" disabled={blocked} onPress={()=>setConfirm(true)} style={s.button}><Text style={s.buttonText}>{t('session_archive')}</Text></Pressable>{confirm?<><Text>{t('session_archive_warning')}</Text><Pressable accessibilityRole="button" disabled={blocked} onPress={()=>void transition('archived')} style={s.button}><Text style={s.buttonText}>{t('session_archive_confirm')}</Text></Pressable></>:null}</>:null}
  {session.state==='archived'?<Text>{t('session_archived')}</Text>:null}
 </View>;
}
