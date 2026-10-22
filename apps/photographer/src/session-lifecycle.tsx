import {Button,Text} from 'react-native-paper';
import {useEffect,useRef,useState} from 'react';
import {View} from 'react-native';
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
  {attempt.current?<Button accessibilityRole="button" disabled={disabled||pending} onPress={()=>void transition()}    mode="contained" contentStyle={{minHeight:48}}>{t('session_state_retry')}</Button>:null}
  {session.state==='open'?<Button accessibilityRole="button" disabled={blocked} onPress={()=>void transition('closed')}    mode="contained" contentStyle={{minHeight:48}}>{t('session_close')}</Button>:null}
  {session.state==='closed'?<><Button accessibilityRole="button" disabled={blocked} onPress={()=>void transition('open')}    mode="contained" contentStyle={{minHeight:48}}>{t('session_reopen')}</Button>
   <Button accessibilityRole="button" disabled={blocked} onPress={()=>setConfirm(true)}    mode="contained" contentStyle={{minHeight:48}}>{t('session_archive')}</Button>{confirm?<><Text>{t('session_archive_warning')}</Text><Button accessibilityRole="button" disabled={blocked} onPress={()=>void transition('archived')}    mode="contained" contentStyle={{minHeight:48}}>{t('session_archive_confirm')}</Button></>:null}</>:null}
  {session.state==='archived'?<Text>{t('session_archived')}</Text>:null}
 </View>;
}
