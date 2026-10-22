import {Button,Text} from 'react-native-paper';
import {useRef} from 'react';
import {SafeAreaView,View} from 'react-native';
import {assertInterchangeScope} from './interchange-job';
import type {InterchangeFormat,InterchangeCreateRequest} from './interchange-create';
import {freezeInterchangeCreate} from './interchange-create';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type Props={tenant:string;project:string;snapshot:string;onSelect:(request:InterchangeCreateRequest)=>void};
export function InterchangeExportFormatScreen({tenant,project,snapshot,onSelect}:Props){
 const {t}=useLocale(),selected=useRef(false);
 try{assertInterchangeScope(tenant,project,snapshot);}catch{return <Text accessibilityRole="alert">{t('interchange_create_failed')}</Text>}
 const formats:InterchangeFormat[]=['photoeditor_bundle','xmp','captureone_sidecar'];
 return <SafeAreaView style={s.root}><View style={s.header}><Text style={s.title}>{t('interchange_format_title')}</Text>
 {formats.map(format=><View key={format}><Button  accessibilityRole="button"  onPress={()=>{if(selected.current)return;const request=freezeInterchangeCreate({project_id:project,direction:'export',format,snapshot_id:snapshot},'format-selection');selected.current=true;try{onSelect(request);}catch(error){selected.current=false;throw error;}}} mode="contained" contentStyle={{minHeight:48}}>{t(`interchange_format_${format}`)}</Button><Text style={s.subtitle}>{t(`interchange_format_${format}_hint`)}</Text></View>)}
 </View></SafeAreaView>;
}
