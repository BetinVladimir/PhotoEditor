import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,FlatList,Pressable,Text,View} from 'react-native';
import {router} from 'expo-router';
import {APIError} from '@photoeditor/api-client';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {SceneBoard} from './scene-board';
import type {StyleProfile,StyleVersion,Page} from './catalog-types';
import {sceneApplyDecision,type SceneApplyDecision} from './scene-apply';
import {SceneApplyAttemptStore} from './scene-apply-attempt-store';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
/** Explicit group decisions are independent of reference changes. Lost responses retain the exact request. */
type SceneApplyProps={api:OwnerAPI;sessionId:string;analysisId:string;board:SceneBoard;disabled:boolean;onBusy:(busy:boolean)=>void;onAccessDenied:()=>void};
export function SceneApplyPanel(props:SceneApplyProps){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.sessionId,props.analysisId,props.board]);
 return <SceneApplyScope key={scope} {...props}/>;
}
function SceneApplyScope({api,sessionId,analysisId,board,disabled,onBusy,onAccessDenied}:SceneApplyProps){
 const {t}=useLocale();const life=useRef<AbortController|null>(null),lock=useRef(false),attempt=useRef<{body:SceneApplyDecision;key:string}|null>(null),journal=useRef<SceneApplyAttemptStore|null>(null);
 const [accepted,setAccepted]=useState(false);
 const [open,setOpen]=useState(false),[loading,setLoading]=useState(false),[submitting,setSubmitting]=useState(false),[error,setError]=useState<'scene_apply_failed'|'scene_apply_conflict'|null>(null);
 const [revision,setRevision]=useState<string>(),[styles,setStyles]=useState<StyleProfile[]>([]),[versions,setVersions]=useState<StyleVersion[]>([]),[recipe,setRecipe]=useState<StyleVersion>(),[selected,setSelected]=useState<string[]>([]);
 useEffect(()=>{const controller=new AbortController();life.current=controller;return()=>{controller.abort();life.current=null;journal.current=null;onBusy(false);};},[api,sessionId,analysisId,board]);
 async function pages<T extends {id:string}>(controller:AbortController,load:(cursor?:string)=>Promise<Page<T>>):Promise<T[]>{
  const result:T[]=[],ids=new Set<string>(),cursors=new Set<string>();let cursor:string|undefined;
  do{if(controller.signal.aborted||life.current!==controller)throw new Error('SCENE_SCOPE_ABORTED');const page=await load(cursor);if(controller.signal.aborted||life.current!==controller)throw new Error('SCENE_SCOPE_ABORTED');if(!Array.isArray(page.items)||result.length+page.items.length>5000)throw new Error('SCENE_APPLY_INVALID');for(const item of page.items){if(!item.id||ids.has(item.id))throw new Error('SCENE_APPLY_INVALID');ids.add(item.id);result.push(item);}cursor=page.next_cursor??undefined;if(cursor!==undefined&&(typeof cursor!=='string'||cursor.length<1||cursor.length>2048))throw new Error('SCENE_APPLY_INVALID');if(cursor){if(cursors.has(cursor)||cursors.size>=5000)throw new Error('SCENE_APPLY_INVALID');cursors.add(cursor);}}while(cursor);
  return result;
 }
 function failure(e:unknown,controller:AbortController){if(controller.signal.aborted||life.current!==controller)return;if(e instanceof APIError&&[401,403,404].includes(e.status)){attempt.current=null;onAccessDenied();}else if(e instanceof APIError&&[409,410,412,422].includes(e.status)){attempt.current=null;setError('scene_apply_conflict');}else setError('scene_apply_failed');}
 async function start(){const controller=life.current;if(!controller||disabled||lock.current)return;lock.current=true;setOpen(true);onBusy(true);setLoading(true);setError(null);
  try{const user=await api.me(controller.signal);if(controller.signal.aborted||life.current!==controller)return;
   const savedJournal=new SceneApplyAttemptStore(AsyncStorage,process.env.EXPO_PUBLIC_API_ORIGIN||'',user.id,api.tenant,sessionId,analysisId);
   const saved=await savedJournal.read();if(controller.signal.aborted||life.current!==controller)return;journal.current=savedJournal;
   if(saved){if(saved.body.plan_sha!==board.planSha)throw new Error('SCENE_APPLY_JOURNAL_CONFLICT');attempt.current={body:saved.body,key:saved.key};setRevision(saved.body.expected_revision);if(saved.ticket){setAccepted(true);router.push({pathname:'/runs/[id]',params:{id:saved.ticket.run_id,tenant:api.tenant}});}return;}
   const session=await api.shootSession(sessionId,controller.signal);if(controller.signal.aborted||life.current!==controller)return;const analysis=await api.get<{id:string;revision:string;document:{sha256:string};data:{session_id:string;project_id:string}}>(`/api/v1/tenants/{t}/session-analyses/${encodeURIComponent(analysisId)}`,controller.signal);
   if(analysis.id!==analysisId||analysis.data.session_id!==sessionId||analysis.data.project_id!==session.project_id||analysis.document.sha256!==board.planSha||analysis.revision!=='1')throw new Error('SCENE_APPLY_INVALID');
   if(controller.signal.aborted||life.current!==controller)return;const available=await pages(controller,cursor=>api.styles(session.project_id,cursor,controller.signal));if(controller.signal.aborted||life.current!==controller)return;setRevision(analysis.revision);setStyles(available.filter(style=>style.project_id===session.project_id&&!style.archived));
  }catch(e){failure(e,controller);}finally{lock.current=false;if(!controller.signal.aborted)setLoading(false);}
 }
 async function chooseStyle(style:StyleProfile){const controller=life.current;if(!controller||disabled||lock.current||attempt.current)return;lock.current=true;setLoading(true);setError(null);setRecipe(undefined);setVersions([]);
  try{const available=await pages(controller,cursor=>api.styleVersions(style.id,cursor,controller.signal));if(!controller.signal.aborted&&life.current===controller)setVersions(available.filter(version=>version.style_id===style.id&&version.approval_status==='accepted'&&!!version.export_artifact_id&&/^[0-9a-f]{64}$/.test(version.config_hash)));}catch(e){failure(e,controller);}finally{lock.current=false;if(!controller.signal.aborted&&life.current===controller)setLoading(false);}
 }
 async function submit(){const controller=life.current;if(!controller||disabled||lock.current||accepted||!journal.current)return;lock.current=true;setSubmitting(true);setError(null);let sent=false;
  try{if(!attempt.current){if(!recipe?.export_artifact_id||!revision)throw new Error('SCENE_APPLY_INVALID');attempt.current={body:sceneApplyDecision(board,selected,{artifact_id:recipe.export_artifact_id,sha256:recipe.config_hash},revision),key:crypto.randomUUID()};}
   const current=attempt.current;await journal.current.write({schema_version:1,session_id:sessionId,analysis_id:analysisId,key:current.key,body:current.body,ticket:null});if(controller.signal.aborted||life.current!==controller)return;
   sent=true;const ticket=await api.applySceneAnalysis(analysisId,current.body,current.key,controller.signal);if(controller.signal.aborted||life.current!==controller)return;
   await journal.current.write({schema_version:1,session_id:sessionId,analysis_id:analysisId,key:current.key,body:current.body,ticket:{run_id:ticket.run_id}});if(controller.signal.aborted||life.current!==controller)return;setAccepted(true);router.push({pathname:'/runs/[id]',params:{id:ticket.run_id,tenant:api.tenant}});
  }catch(e){if(sent&&e instanceof APIError&&[409,410,412,422].includes(e.status)&&attempt.current&&journal.current){try{await journal.current.discardRejected(attempt.current.key);}catch{setError('scene_apply_failed');return;}}failure(e,controller);}finally{lock.current=false;if(!controller.signal.aborted&&life.current===controller)setSubmitting(false);}
 }
 if(!open)return <Pressable accessibilityRole="button" disabled={disabled} onPress={()=>void start()} style={s.button}><Text style={s.buttonText}>{t('scene_apply_open')}</Text></Pressable>;
 const pending=accepted||loading||submitting||!!attempt.current,frozen=disabled||pending||error==='scene_apply_conflict';
 return <View style={{padding:12,gap:8}}><Text accessibilityRole="header" style={s.name}>{t('scene_apply_open')}</Text><Text style={s.meta}>{t('scene_apply_omitted')}</Text>{error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}{loading?<ActivityIndicator/>:null}
  <FlatList data={styles} keyExtractor={style=>style.id} initialNumToRender={8} style={{maxHeight:140}} renderItem={({item})=><Pressable accessibilityRole="button" disabled={frozen} onPress={()=>void chooseStyle(item)} style={s.button}><Text style={s.buttonText}>{item.name}</Text></Pressable>}/>
  <FlatList data={versions} keyExtractor={version=>version.id} initialNumToRender={8} style={{maxHeight:140}} renderItem={({item})=><Pressable accessibilityRole="radio" accessibilityState={{checked:recipe?.id===item.id,disabled:frozen}} disabled={frozen} onPress={()=>setRecipe(item)} style={s.button}><Text style={s.buttonText}>{t('scene_apply_recipe')} {item.version_no}</Text></Pressable>}/>
  {!loading&&!styles.length?<Text>{t('scene_apply_no_styles')}</Text>:null}
  <FlatList data={board.groups} keyExtractor={group=>group.id} initialNumToRender={8} maxToRenderPerBatch={8} windowSize={5} style={{maxHeight:200}} renderItem={({item,index})=><Pressable accessibilityRole="checkbox" accessibilityLabel={`${t('scene_group')} ${index+1}`} accessibilityState={{checked:selected.includes(item.id),disabled:frozen}} disabled={frozen} onPress={()=>setSelected(value=>value.includes(item.id)?value.filter(id=>id!==item.id):[...value,item.id])} style={s.button}><Text style={s.buttonText}>{t('scene_group')} {index+1} · {item.frames.length}</Text></Pressable>}/>
  <Pressable accessibilityRole="button" disabled={disabled||accepted||loading||submitting||error==='scene_apply_conflict'||(!attempt.current&&(!recipe||!selected.length||!revision))} onPress={()=>void submit()} style={s.button}><Text style={s.buttonText}>{t(attempt.current?'scene_apply_retry':'scene_apply_submit')}</Text></Pressable>
  <Pressable accessibilityRole="button" disabled={pending} onPress={()=>{setOpen(false);onBusy(false);setError(null);setSelected([]);setRecipe(undefined);setVersions([]);setStyles([]);setRevision(undefined);}} style={s.button}><Text style={s.buttonText}>{t('cancel')}</Text></Pressable>
 </View>;
}
