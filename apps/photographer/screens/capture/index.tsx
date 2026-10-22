import {Button,TouchableRipple,ActivityIndicator,Text} from 'react-native-paper';
import {readCaptureSessions} from '../../src/capture-list-contract';
import {useWorkspaceAccess} from '../../src/workspace-access-context';
import {useReadResource} from "../../src/use-read-resource";
import {ReadResourceState} from "../../src/read-resource-state";
import {CaptureSessionCreate} from '../../src/capture-session-create';
import {LocalePicker} from "../../src/locale-picker";
import {useLocale} from "../../src/locale-context";
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../src/localized-router';
import {FlatList,SafeAreaView,View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import type {Page} from '../../src/catalog-types';
import {catalogStyle as s} from '../../src/catalog-style';
type Session={id:string;name:string;state:'open'|'closed'|'archived'};
export default function CaptureSessions(){
 const {tenant,project}=useLocalSearchParams<{tenant:string;project:string}>();
 const {t}=useLocale();
 const auth=useAuth(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 const canEdit=useWorkspaceAccess(tenant||'')?.canEdit===true;
 const [creating,setCreating]=useState(false),[creatingBusy,setCreatingBusy]=useState(false);
 const [busy,setBusy]=useState(false),[error,setError]=useState('');
 const moreFlight=useRef<AbortController|null>(null);
 const resource=useReadResource(`${tenant}:${project}`,api,signal=>{
  if(!tenant||!project)throw new Error('capture_project_scope');
  return api.shootSessions(project,undefined,signal).then(page=>readCaptureSessions(page,tenant,project));
 });
 const items=resource.data?.items??[],cursor=resource.data?.next_cursor??null;
 useEffect(()=>{moreFlight.current?.abort();setCreating(false);setCreatingBusy(false);setBusy(false);setError('');return()=>{moreFlight.current?.abort();};},[api,project,tenant,resource.phase]);
 async function more(){
  if(!cursor||moreFlight.current||busy||resource.phase!=='ready'||!resource.current())return;
  const controller=new AbortController();moreFlight.current=controller;setBusy(true);setError('');
  const timeout=setTimeout(()=>{if(moreFlight.current===controller&&resource.current()){controller.abort();setError('capture_more_sessions_error');setBusy(false);}},8000);
  try{
   const page=readCaptureSessions(await api.shootSessions(project,cursor,controller.signal),tenant,project);
   if(controller.signal.aborted||moreFlight.current!==controller||!resource.current())return;
   const seen=new Set(items.map(x=>x.id));
   if(page.next_cursor===cursor||page.items.some(x=>seen.has(x.id))||new Set(page.items.map(x=>x.id)).size!==page.items.length)throw new Error('Invalid page');
   resource.commit({...page,items:[...items,...page.items]});
  }catch{if(!controller.signal.aborted&&resource.current())setError('capture_more_sessions_error');}
  finally{clearTimeout(timeout);if(moreFlight.current===controller){moreFlight.current=null;if(resource.current())setBusy(false);}}
 }
 const displayedMore=moreFlight.current;
 return <SafeAreaView style={s.root}><View style={s.header}><LocalePicker/><Text style={s.title}>{t("capture_sessions")}</Text><Button accessibilityRole="button" disabled={!canEdit||busy||creatingBusy||resource.phase!=="ready"}  onPress={()=>setCreating(value=>!value)}   mode="contained" contentStyle={{minHeight:48}}>{t(creating?'capture_cancel_create':'capture_create_session')}</Button>{busy&&displayedMore?<Button accessibilityRole="button" onPress={()=>{if(!resource.current()||moreFlight.current!==displayedMore||displayedMore.signal.aborted)return;displayedMore.abort();moreFlight.current=null;setBusy(false);setError('capture_more_sessions_error')}} mode="outlined">{t('read_cancel')}</Button>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t(error as Parameters<typeof t>[0])}</Text>:null}</View>
 {resource.phase!=="ready"?<ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel}/>:<FlatList ListHeaderComponent={canEdit&&creating?<CaptureSessionCreate api={api} origin={auth.origin} project={project} sessions={items} onBusy={setCreatingBusy} onCreated={item=>{if(!resource.current())return;setCreatingBusy(false);resource.commit({...resource.data!,items:items.some(value=>value.id===item.id)?items:[item,...items]});setCreating(false);router.push({pathname:'/capture/[id]',params:{id:item.id,tenant}})}}/>:null} data={items} keyExtractor={item=>item.id} contentContainerStyle={s.list} ListFooterComponent={cursor?<Button accessibilityRole="button" disabled={busy}  onPress={()=>void more()}   mode="contained" contentStyle={{minHeight:48}}>{t("capture_more_sessions")}</Button>:null} ListEmptyComponent={<Text style={s.empty}>{t("capture_no_sessions")}</Text>} renderItem={({item})=><TouchableRipple accessibilityRole="button" style={s.row} onPress={()=>router.push({pathname:'/capture/[id]',params:{id:item.id,tenant}})}><View style={{flex:1,flexDirection:'row',alignItems:'center',justifyContent:'space-between'}}><View><Text style={s.name}>{item.name}</Text><Text style={s.meta}>{t(`capture_state_${item.state}`)}</Text></View></View></TouchableRipple>}/>}</SafeAreaView>;
}
