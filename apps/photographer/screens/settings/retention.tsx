import {Button, Text, TextInput} from "react-native-paper";
import {useCASMutation} from '../../src/use-cas-mutation';
import {useLocale} from '../../src/locale-context';
import {useWorkspaceAccess} from '../../src/workspace-access-context';
import {useEffect,useMemo,useState} from 'react';
import {useLocalSearchParams} from '../../src/localized-router';
import {SafeAreaView, View} from "react-native";
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';
export default function Retention(){
 const {tenant}=useLocalSearchParams<{tenant:string}>(),auth=useAuth(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 const canAdmin=useWorkspaceAccess(tenant||'')?.canAdmin===true;
 const resource=useReadResource(JSON.stringify([auth.origin,tenant,'retention',canAdmin]),api,signal=>{if(!tenant)throw new Error('Workspace required');return api.tenantSettings(signal);});
 const value=resource.data,[fields,setFields]=useState<string[]>([]),[error,setError]=useState('');
 useEffect(()=>{setFields(value?[String(value.retention.processing_days),String(value.retention.exports_days),String(value.retention.temporary_hours)]:[]);setError('');},[value]);
 const {t}=useLocale();const mutation=useCASMutation(api,resource.current,resource.retry),busy=mutation.busy;
 async function save(){if(!canAdmin||!value||busy)return;const nums=fields.map(Number);if(nums.length!==3||nums.some((n,i)=>!Number.isInteger(n)||n<1||n>(i===2?168:365))){setError('retention_invalid');return;}setError('');await mutation.run(signal=>api.updateTenant(value,{retention:{processing_days:nums[0],exports_days:nums[1],temporary_hours:nums[2]}},signal),resource.commit);}
 const labels=[t('retention_processing_days'),t('retention_exports_days'),t('retention_temporary_hours')];
 return <SafeAreaView style={s.root}><View style={s.header}><Text variant="headlineMedium" style={s.title}>{t('retention_title')}</Text><ReadResourceState {...resource}/>{value?<>{fields.map((x,i)=><TextInput mode="outlined" key={labels[i]} editable={canAdmin&&!busy} accessibilityLabel={labels[i]} value={x} onChangeText={v=>setFields(a=>a.map((old,j)=>i===j?v:old))} keyboardType="number-pad" placeholder={labels[i]} placeholderTextColor="#888" style={s.input}/>)}<Button mode="outlined" disabled={!canAdmin||busy} onPress={()=>void save()}>{t('retention_save')}</Button></>:null}{busy?<Button mode="outlined" accessibilityRole="button" onPress={mutation.cancel}>{t('read_cancel')}</Button>:null}{mutation.uncertain?<Text accessibilityRole="alert" style={s.error}>{t('settings_cas_uncertain')}</Text>:null}{error?<><Text accessibilityRole="alert" style={s.error}>{t('retention_invalid')}</Text><Button mode="outlined" disabled={busy} onPress={resource.retry}>{t('retention_reload')}</Button></>:null}</View></SafeAreaView>;
}
