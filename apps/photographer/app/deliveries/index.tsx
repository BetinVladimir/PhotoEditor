import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from 'expo-router';
import {ActivityIndicator,FlatList,Pressable,SafeAreaView,Text,View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import type {DeliverySummary} from '../../src/delivery-list';
import {useLocale} from '../../src/locale-context';
import {catalogStyle as s} from '../../src/catalog-style';

export default function DeliveryListRoute(){
 const{tenant,project}=useLocalSearchParams<{tenant:string;project:string}>(),auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 const api=useMemo(()=>new OwnerAPI(client,tenant||''),[client,tenant]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <Scope key={JSON.stringify([generation,auth.origin,tenant,project])} api={api} tenant={tenant||''} project={project||''}/>;
}
function Scope({api,tenant,project}:{api:OwnerAPI;tenant:string;project:string}){
 const{t}=useLocale(),[items,setItems]=useState<DeliverySummary[]>([]),[cursor,setCursor]=useState<string|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(false),[epoch,setEpoch]=useState(0);
 const flight=useRef<AbortController|null>(null),seen=useRef(new Set<string>()),used=useRef(new Set<string>());
 useEffect(()=>{
  seen.current=new Set();used.current=new Set();setItems([]);setCursor(null);setError(false);
  void load();return()=>{flight.current?.abort();flight.current=null};
 },[api,project,epoch]);
 async function load(next?:string){
  if(flight.current||next&&used.current.has(next))return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  try{
   const page=await api.deliveryPackages(project,next,seen.current,controller.signal,next?items.at(-1):undefined);
   if(controller.signal.aborted)return;
   if(next)used.current.add(next);
   if(page.next_cursor&&used.current.has(page.next_cursor))throw new Error('DELIVERY_CURSOR_REPEATED');
   for(const item of page.items)seen.current.add(item.id);
   setItems(old=>next?[...old,...page.items]:page.items);setCursor(page.next_cursor);
  }catch(cause){if(!controller.signal.aborted){
   if(cause instanceof APIError&&[401,403,404].includes(cause.status)){
    seen.current.clear();used.current.clear();setItems([]);setCursor(null);
   }
   setError(true);
  }}
  finally{if(flight.current===controller){flight.current=null;setBusy(false)}}
 }
 return <SafeAreaView style={s.root}><View style={s.header}><Text accessibilityRole="header" style={s.title}>{t('delivery_list_title')}</Text>
  {error?<Text accessibilityRole="alert" style={s.error}>{t('delivery_list_failed')}</Text>:null}
  <Pressable accessibilityRole="button" disabled={busy} onPress={()=>setEpoch(value=>value+1)} style={s.button}><Text style={s.buttonText}>{t('delivery_refresh')}</Text></Pressable>
 </View><FlatList data={items} keyExtractor={item=>item.id} contentContainerStyle={s.list} ListEmptyComponent={!busy&&!error?<Text style={s.empty}>{t('delivery_list_empty')}</Text>:null}
  renderItem={({item})=><Pressable accessibilityRole="button" style={s.row} onPress={()=>router.push({pathname:'/deliveries/[id]',params:{tenant,project,id:item.id}})}><View><Text style={s.name}>{item.name}</Text><Text style={s.meta}>{t(`delivery_state_${item.state}`)} · {item.created_at}</Text></View><Text style={s.chevron}>›</Text></Pressable>}
  ListFooterComponent={busy?<ActivityIndicator/>:cursor&&!error?<Pressable accessibilityRole="button" onPress={()=>void load(cursor)} style={s.button}><Text style={s.buttonText}>{t('delivery_list_more')}</Text></Pressable>:null}/></SafeAreaView>;
}
