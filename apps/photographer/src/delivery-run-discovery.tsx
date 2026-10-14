import {useEffect,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {DeliveryPackage} from './delivery-package';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

type Props={api:OwnerAPI;run:string;onOpen:(pkg:DeliveryPackage)=>void};
export function DeliveryRunDiscovery({api,run,onOpen}:Props){
 const{t}=useLocale(),[pkg,setPackage]=useState<DeliveryPackage|null>(null),[failed,setFailed]=useState(false),[epoch,setEpoch]=useState(0);
 useEffect(()=>{
  const controller=new AbortController();let live=true;setPackage(null);setFailed(false);
  void api.deliveryPackageForRun(run,controller.signal).then(value=>{if(live&&!controller.signal.aborted)setPackage(value)}).catch(error=>{
   if(live&&!controller.signal.aborted&&!(error instanceof APIError&&error.status===404))setFailed(true);
  });
  return()=>{live=false;controller.abort()};
 },[api,run,epoch]);
 return <View>{pkg?<Pressable accessibilityRole="button" style={s.button} onPress={()=>onOpen(pkg)}><Text style={s.buttonText}>{t('delivery_title')}</Text></Pressable>:null}
  {failed?<><Text accessibilityRole="alert" style={s.error}>{t('delivery_load_failed')}</Text><Pressable accessibilityRole="button" style={s.button} onPress={()=>setEpoch(value=>value+1)}><Text style={s.buttonText}>{t('delivery_refresh')}</Text></Pressable></>:null}</View>;
}
