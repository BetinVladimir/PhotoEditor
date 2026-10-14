import {useRef} from 'react';
import {Pressable,SafeAreaView,Text,View} from 'react-native';
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
 {formats.map(format=><View key={format}><Pressable accessibilityRole="button" style={s.button} onPress={()=>{if(selected.current)return;selected.current=true;try{onSelect(format);}catch(error){selected.current=false;throw error;}}}><Text style={s.buttonText}>{t(`interchange_format_${format}`)}</Text></Pressable><Text style={s.subtitle}>{t(`interchange_import_${format}_hint`)}</Text></View>)}
 </View></SafeAreaView>;
}
