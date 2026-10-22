import {useLocale} from '../../src/locale-context';
import {Text} from "react-native-paper";
import {useMemo} from 'react';
import {useLocalSearchParams} from '../../src/localized-router';
import {SafeAreaView, View} from "react-native";
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';
export default function Quotas(){
 const __r2Value=useLocale().value;

 const __uiT=useLocale().t;

 const {tenant}=useLocalSearchParams<{tenant:string}>(),auth=useAuth(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 const resource=useReadResource(JSON.stringify([auth.origin,tenant,'quotas']),api,async signal=>{if(!tenant)throw new Error('Workspace required');const [value,caps]=await Promise.all([api.tenantSettings(signal),api.capabilities(signal)]);return {value,caps};});
 const data=resource.data,{t,locale}=useLocale();
 return <SafeAreaView style={s.root}><View style={s.header}><Text variant="headlineMedium" style={s.title}>{t('settings_quotas_title')}</Text><ReadResourceState {...resource}/>{data?<><Text variant="titleMedium" style={s.name}>{t('settings_quotas_storage')}</Text><Text style={s.subtitle}>{(data.value.quotas.storage_bytes/1073741824).toLocaleString(locale,{minimumFractionDigits:1,maximumFractionDigits:1})} {__uiT('ui_66a1561b84b4')}</Text><Text variant="titleMedium" style={s.name}>{t('settings_quotas_source_files')}</Text><Text style={s.subtitle}>{data.value.quotas.source_files.toLocaleString(locale)}</Text><Text variant="titleMedium" style={s.name}>{t('settings_quotas_gpu_tasks')}</Text><Text style={s.subtitle}>{data.value.quotas.max_active_gpu_tasks}</Text><Text variant="titleMedium" style={s.name}>{t('settings_quotas_operations')}</Text>{data.caps.operations.length?data.caps.operations.map(x=><View key={x.operation} style={s.row}><Text variant="titleMedium" style={s.name}>{__r2Value(x.operation)}</Text><Text style={x.available?s.meta:s.error}>{x.available?t('settings_quotas_available'):(x.reason?__uiT('ui_07e3a63ea0ed', {p0:t('settings_quotas_unavailable'),p1:x.reason}):t('settings_quotas_unavailable'))}</Text></View>):<Text style={s.empty}>{t('settings_quotas_empty')}</Text>}</>:null}</View></SafeAreaView>;
}
