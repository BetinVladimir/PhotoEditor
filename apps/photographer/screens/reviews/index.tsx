import {Button,TouchableRipple,ActivityIndicator,Text} from 'react-native-paper';
import {useLocale} from '../../src/locale-context';
import {readDeadline} from '../../src/read-deadline';
import {APIError} from '@photoeditor/api-client';
import {useCallback,useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../src/localized-router';
import {FlatList,SafeAreaView,View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {useReviewForeground} from '../../src/use-review-foreground';
import {OwnerAPI} from '../../src/owner-api';
import type {Review} from '../../src/catalog-types';
import {catalogStyle as s} from '../../src/catalog-style';
export default function Reviews(){
 const {t}=useLocale();const {tenant}=useLocalSearchParams<{tenant:string}>(),auth=useAuth(),foreground=useReviewForeground();
 const identity=useRef({auth,generation:0});if(identity.current.auth!==auth)identity.current={auth,generation:identity.current.generation+1};
 if(!foreground.active)return <SafeAreaView style={s.root}><Text>{t('reviews_paused')}</Text></SafeAreaView>;
 return <ReviewsScope key={`${tenant}:${auth.origin}:${identity.current.generation}:${foreground.generation}`} tenant={tenant}/>;
}
function ReviewsScope({tenant}:{tenant:string}){
 const __r2Value=useLocale().value;

 const __uiT=useLocale().t;

 const {t}=useLocale();const auth=useAuth(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 const [items,setItems]=useState<Review[]>([]),[busy,setBusy]=useState(false),[error,setError]=useState(''),[cursor,setCursor]=useState<string|null|undefined>(undefined);
 const rows=useRef(items);rows.current=items;
 const pending=useRef(false),active=useRef(true),controller=useRef<AbortController|null>(null),seen=useRef(new Set<string>());
 const load=useCallback(async(next?:string)=>{
  if(pending.current||!active.current)return;pending.current=true;setBusy(true);setError('');const request=new AbortController();controller.current=request;
  try{
   const page=await readDeadline(request.signal,signal=>api.reviews('pending',next,signal));request.signal.throwIfAborted();if(!active.current)return;
   if(!Array.isArray(page.items)||page.items.some(item=>!item||typeof item.id!=='string'||!item.id)||new Set(page.items.map(item=>item.id)).size!==page.items.length||next&&page.items.some(item=>rows.current.some(previous=>previous.id===item.id)))throw new Error('Invalid review page');
   const following=page.next_cursor??null;if(following&&(typeof following!=='string'||following.length>4096))throw new Error('Invalid review cursor');if(following&&(following===next||seen.current.has(following)))throw new Error('Repeated review cursor');
   if(next)seen.current.add(next);
   setItems(previous=>{const merged=new Map((next?previous:[]).map(item=>[item.id,item]));for(const item of page.items)merged.set(item.id,item);return [...merged.values()]});setCursor(following);
  }catch(error){if(!request.signal.aborted&&active.current){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){setItems([]);setCursor(undefined);seen.current.clear()}setError('Unable to load reviews. Retry this page.')}}
  finally{if(controller.current===request){controller.current=null;pending.current=false;if(active.current)setBusy(false)}}
 },[api]);
 useEffect(()=>{active.current=true;void load();return()=>{active.current=false;controller.current?.abort();controller.current=null;pending.current=false}},[load]);
 function cancel(){controller.current?.abort();controller.current=null;pending.current=false;setBusy(false);setError('Unable to load reviews. Retry this page.');}
 return <SafeAreaView style={s.root}><View style={s.header}><Text style={s.title}>{t('reviews_title')}</Text><Text style={s.subtitle}>{t('reviews_subtitle')}</Text>{busy?<Button accessibilityRole="button" onPress={cancel} mode="outlined">{t('reviews_cancel')}</Button>:null}{error?<View><Text accessibilityRole="alert" style={s.error}>{t('reviews_load_error')}</Text><Button accessibilityRole="button" disabled={busy} onPress={()=>void load(cursor??undefined)} mode="outlined">{t('reviews_retry')}</Button></View>:null}</View><FlatList data={items} keyExtractor={item=>item.id} contentContainerStyle={s.list} ListEmptyComponent={busy?<ActivityIndicator style={s.loader}/>:<Text style={s.empty}>{t('reviews_empty')}</Text>} ListFooterComponent={busy&&items.length?<ActivityIndicator/>:cursor?<Button accessibilityRole="button" onPress={()=>void load(cursor)} mode="outlined">{t('reviews_more')}</Button>:null} renderItem={({item})=><TouchableRipple style={s.row} onPress={()=>router.push({pathname:'/reviews/[id]',params:{id:item.id,tenant}})}><View style={{flex:1,flexDirection:'row',alignItems:'center',justifyContent:'space-between'}}><View><Text style={s.name}>{__r2Value(item.kind)}</Text><Text style={s.meta}>{__r2Value(item.state)} {__uiT('ui_90991b82e59a')}{' '}{item.run_id.slice(0,8)}</Text></View><Text style={s.chevron}>›</Text></View></TouchableRipple>}/></SafeAreaView>;
}
