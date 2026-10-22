import {ActivityIndicator,Button,Text,TouchableRipple} from 'react-native-paper';
import {readDeadline} from '../../src/read-deadline';
import {useReviewForeground} from '../../src/use-review-foreground';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../src/localized-router';
import {FlatList,SafeAreaView,View} from 'react-native';
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
 const foreground=useReviewForeground(),foregroundActive=useRef(foreground.active);
 foregroundActive.current=foreground.active;
 const{t}=useLocale(),[items,setItems]=useState<DeliverySummary[]>([]),[cursor,setCursor]=useState<string|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(false),[epoch,setEpoch]=useState(0);
 const flight=useRef<AbortController|null>(null),seen=useRef(new Set<string>()),used=useRef(new Set<string>()),requested=useRef<string|undefined>(undefined);
 useEffect(()=>{
  seen.current=new Set();used.current=new Set();setItems([]);setCursor(null);setError(false);
  if(!foreground.active){setBusy(false);return;}
  void load();return()=>{flight.current?.abort();flight.current=null};
 },[api,project,epoch,foreground.active,foreground.generation]);
 async function load(next?:string){
  if(!foregroundActive.current||flight.current||next&&used.current.has(next))return;
  requested.current=next;const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  try{
   const page=await readDeadline(controller.signal,signal=>api.deliveryPackages(project,next,seen.current,signal,next?items.at(-1):undefined));
   if(!foregroundActive.current||controller.signal.aborted)return;
   if(next)used.current.add(next);
   if(page.next_cursor&&used.current.has(page.next_cursor))throw new Error('DELIVERY_CURSOR_REPEATED');
   for(const item of page.items)seen.current.add(item.id);
   setItems(old=>next?[...old,...page.items]:page.items);setCursor(page.next_cursor);
  }catch(cause){if(!controller.signal.aborted){
   if(cause instanceof APIError&&[401,403,404,410].includes(cause.status)){
    seen.current.clear();used.current.clear();setItems([]);setCursor(null);
   }
   setError(true);
  }}
  finally{if(flight.current===controller){flight.current=null;setBusy(false)}}
 }
 function cancel(){const owned=flight.current;if(!owned)return;owned.abort();flight.current=null;setBusy(false);setError(true)}
 if(!foreground.active)return <SafeAreaView style={s.root}><Text style={s.title}>{t('delivery_list_title')}</Text></SafeAreaView>;
 return <SafeAreaView style={s.root}><View style={s.header}><Text accessibilityRole="header" style={s.title}>{t('delivery_list_title')}</Text>
  {error?<><Text accessibilityRole="alert" style={s.error}>{t('delivery_list_failed')}</Text><Button accessibilityRole="button" onPress={()=>void load(requested.current)}>{t('read_retry')}</Button></>:null}{busy?<Button accessibilityRole="button" onPress={cancel}>{t('read_cancel')}</Button>:null}
  <Button  accessibilityRole="button" disabled={busy} onPress={()=>setEpoch(value=>value+1)}  mode="contained" contentStyle={{minHeight:48}}>{t('delivery_refresh')}</Button>
 </View><FlatList data={items} keyExtractor={item=>item.id} contentContainerStyle={s.list} ListEmptyComponent={!busy&&!error?<Text style={s.empty}>{t('delivery_list_empty')}</Text>:null}
  renderItem={({item})=><TouchableRipple accessibilityRole="button" style={s.row} onPress={()=>{if(foregroundActive.current)router.push({pathname:'/deliveries/[id]',params:{tenant,project,id:item.id}})}}><View style={{flex:1,flexDirection:'row',alignItems:'center',justifyContent:'space-between'}}><View><Text style={s.name}>{item.name}</Text><Text style={s.meta}>{t(`delivery_state_${item.state}`)} · {item.created_at}</Text></View><Text style={s.chevron}>›</Text></View></TouchableRipple>}
  ListFooterComponent={busy?<ActivityIndicator/>:cursor&&!error?<Button  accessibilityRole="button" onPress={()=>void load(cursor)}  mode="contained" contentStyle={{minHeight:48}}>{t('delivery_list_more')}</Button>:null}/></SafeAreaView>;
}
