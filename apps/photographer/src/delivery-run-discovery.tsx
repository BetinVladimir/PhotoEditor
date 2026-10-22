import {Button,Text} from 'react-native-paper';
import {useReadResource} from './use-read-resource';
import {ReadResourceState} from './read-resource-state';
import {View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {DeliveryPackage} from './delivery-package';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

type Props={api:OwnerAPI;run:string;onOpen:(pkg:DeliveryPackage)=>void};
export function DeliveryRunDiscovery({api,run,onOpen}:Props){
 const{t}=useLocale();
 const resource=useReadResource(JSON.stringify(['delivery-discovery',run]),api,async signal=>{
  try{return await api.deliveryPackageForRun(run,signal);}catch(error){signal.throwIfAborted();if(error instanceof APIError&&error.status===404)return null;throw error;}
 });
 if(resource.phase==='error')return <View><Text accessibilityRole="alert" style={s.error}>{t('delivery_load_failed')}</Text><Button accessibilityRole="button" onPress={resource.retry}>{t('delivery_refresh')}</Button></View>;
 if(resource.phase!=='ready')return <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel}/>;
 const pkg=resource.data;
 return pkg?<View><Button  accessibilityRole="button"  onPress={()=>{if(resource.current())onOpen(pkg)}} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_title')}</Button></View>:null;
}
