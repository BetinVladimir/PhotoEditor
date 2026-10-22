import {Button,Text} from 'react-native-paper';
import {useRef} from 'react';
import {SafeAreaView,View} from 'react-native';
import {archiveUUID} from './interchange-archive-upload';
import type {InterchangeFormat} from './interchange-create';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type Props={tenant:string;project:string;onSelect:(format:InterchangeFormat)=>void};
export function InterchangeImportFormatScreen({tenant,project,onSelect}:Props){
 const {t}=useLocale(),selected=useRef(false);
 if(!archiveUUID(tenant)||!archiveUUID(project))return <Text accessibilityRole="alert">{t('interchange_archive_failed')}</Text>;
 const formats:InterchangeFormat[]=['photoeditor_bundle','xmp','captureone_sidecar'];
 return <SafeAreaView style={s.root}><View style={s.header}><Text style={s.title}>{t('interchange_import_format_title')}</Text>
 {formats.map(format=><View key={format}><Button  accessibilityRole="button"  onPress={()=>{if(selected.current)return;selected.current=true;try{onSelect(format);}catch(error){selected.current=false;throw error;}}} mode="contained" contentStyle={{minHeight:48}}>{t(`interchange_format_${format}`)}</Button><Text style={s.subtitle}>{t(`interchange_import_${format}_hint`)}</Text></View>)}
 </View></SafeAreaView>;
}
