import {CaptureSessionCreate} from '../../src/capture-session-create';
import {LocalePicker} from "../../src/locale-picker";
import {useLocale} from "../../src/locale-context";
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from 'expo-router';
import {ActivityIndicator,FlatList,Pressable,SafeAreaView,Text,View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import type {Page} from '../../src/catalog-types';
import {catalogStyle as s} from '../../src/catalog-style';
type Session={id:string;name:string;state:'open'|'closed'|'archived'};
export default function CaptureSessions(){
 const {tenant,project}=useLocalSearchParams<{tenant:string;project:string}>();
 const {t}=useLocale();
 const auth=useAuth(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 const generation=useRef(0);
 const [creating,setCreating]=useState(false);
 const [creatingBusy,setCreatingBusy]=useState(false);
 const [cursor,setCursor]=useState<string|null>(null);
 const [items,setItems]=useState<Session[]>([]),[busy,setBusy]=useState(true),[error,setError]=useState('');
 useEffect(()=>{const controller=new AbortController();generation.current++;setCreating(false);setCreatingBusy(false);setCursor(null);setItems([]);setError('');setBusy(true);
  if(!tenant||!project){setError('capture_project_scope');setBusy(false);return;}
  api.shootSessions(project,undefined,controller.signal)
   .then(page=>{if(!controller.signal.aborted){setItems(page.items);setCursor(page.next_cursor??null)}})
   .catch(()=>{if(!controller.signal.aborted)setError('capture_sessions_error')})
   .finally(()=>{if(!controller.signal.aborted)setBusy(false)});
  return()=>{generation.current++;controller.abort()};
 },[api,project,tenant]);
 async function more(){
  if(!cursor||busy)return;
  const scope=generation.current;setBusy(true);setError('');
  try{
   const page=await api.shootSessions(project,cursor);
   if(scope!==generation.current)return;
   setItems(old=>{const seen=new Set(old.map(x=>x.id));return [...old,...page.items.filter(x=>!seen.has(x.id))]});setCursor(page.next_cursor??null);
  }catch{if(scope===generation.current)setError('capture_more_sessions_error')}
  finally{if(scope===generation.current)setBusy(false)}
 }
 return <SafeAreaView style={s.root}><View style={s.header}><LocalePicker/><Text style={s.title}>{t("capture_sessions")}</Text><Pressable accessibilityRole="button" disabled={busy||creatingBusy} style={s.button} onPress={()=>setCreating(value=>!value)}><Text style={s.buttonText}>{t(creating?'capture_cancel_create':'capture_create_session')}</Text></Pressable>{error?<Text accessibilityRole="alert" style={s.error}>{t(error as Parameters<typeof t>[0])}</Text>:null}</View>
 {busy&&items.length===0?<ActivityIndicator/>:<FlatList ListHeaderComponent={creating?<CaptureSessionCreate api={api} project={project} sessions={items} onBusy={setCreatingBusy} onCreated={item=>{setCreatingBusy(false);setItems(old=>old.some(value=>value.id===item.id)?old:[item,...old]);setCreating(false);router.push({pathname:'/capture/[id]',params:{id:item.id,tenant}})}}/>:null} data={items} keyExtractor={item=>item.id} contentContainerStyle={s.list} ListFooterComponent={cursor?<Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>void more()}><Text style={s.buttonText}>{t("capture_more_sessions")}</Text></Pressable>:null} ListEmptyComponent={<Text style={s.empty}>{t("capture_no_sessions")}</Text>} renderItem={({item})=><Pressable accessibilityRole="button" style={s.row} onPress={()=>router.push({pathname:'/capture/[id]',params:{id:item.id,tenant}})}><View><Text style={s.name}>{item.name}</Text><Text style={s.meta}>{t(`capture_state_${item.state}`)}</Text></View></Pressable>}/>}</SafeAreaView>;
}
