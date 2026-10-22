import {useLocale} from "../../src/locale-context";
import {Text,TouchableRipple} from 'react-native-paper';
import {CatalogCreateControls} from '../../src/catalog-create-controls';
import {useWorkspaceAccess} from '../../src/workspace-access-context';
import {ReadPageState} from '../../src/read-page-state';
import {useReadPage} from '../../src/use-read-page';
import {ReadResourceState} from '../../src/read-resource-state';
import {useMemo} from 'react';
import {router,useLocalSearchParams} from '../../src/localized-router';
import {FlatList,SafeAreaView,View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import type {StyleProfile} from '../../src/catalog-types';
import {catalogStyle as s} from '../../src/catalog-style';
export default function Styles(){
 const {tenant,project}=useLocalSearchParams<{tenant:string;project?:string}>(),auth=useAuth();
 const {t}=useLocale();
 const canEdit=useWorkspaceAccess(tenant||'')?.canEdit===true,client=useMemo(()=>auth.client(),[auth]),api=useMemo(()=>new OwnerAPI(client,tenant||''),[client,tenant]);
 const resource=useReadPage(`${tenant}:${project||''}:${canEdit}`,api,(cursor,signal)=>{if(!tenant)throw new Error('Workspace required');return api.styles(project,cursor,signal);});
 const items=resource.data?.items??[];
 return <SafeAreaView style={s.root}><View style={s.header}><Text style={s.title}>{t('catalog_styles_title')}</Text>{project?<CatalogCreateControls key={JSON.stringify([tenant,project])} client={client} origin={auth.origin} tenant={tenant} project={project} kind="style" enabled={canEdit&&resource.phase==='ready'&&resource.morePhase!=='loading'} current={resource.current} onCreated={value=>{if(resource.current()){const row=value as StyleProfile;resource.commit({...resource.data!,items:[row,...items.filter(item=>item.id!==row.id)]});}}}/>:<Text style={s.subtitle}>{t('catalog_styles_project_hint')}</Text>}</View>{resource.phase!=='ready'?<ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel}/>:<FlatList data={items} keyExtractor={x=>x.id} contentContainerStyle={s.list} ListFooterComponent={<ReadPageState {...resource}/>} ListEmptyComponent={<Text style={s.empty}>{t('catalog_styles_empty')}</Text>} renderItem={({item})=><TouchableRipple style={s.row} onPress={()=>router.push({pathname:'/styles/[id]',params:{id:item.id,tenant}})}><View style={{flex:1,flexDirection:'row',alignItems:'center',justifyContent:'space-between'}}><View><Text style={s.name}>{item.name}</Text><Text style={s.meta}>{t(item.archived?'catalog_style_archived':'catalog_style_active')}</Text></View><Text style={s.chevron}>›</Text></View></TouchableRipple>}/>}</SafeAreaView>;
}
