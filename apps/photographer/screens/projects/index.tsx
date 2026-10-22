import {useLocale} from "../../src/locale-context";
import {Button, Card, TouchableRipple, Text} from "react-native-paper";
import {WorkspaceSettingsNavigation} from '../../src/navigation-entry-links';
import {CatalogCreateControls} from '../../src/catalog-create-controls';
import {readProjectPage} from '../../src/catalog-read-response';
import {useWorkspaceAccess} from '../../src/workspace-access-context';
import {ReadPageState} from '../../src/read-page-state';
import {useReadPage} from '../../src/use-read-page';
import {ReadResourceState} from '../../src/read-resource-state';
import {useMemo} from 'react';
import {router,useLocalSearchParams} from '../../src/localized-router';
import {FlatList, SafeAreaView, View} from "react-native";
import {useAuth} from '../../src/auth-context';
import type {Page,Project} from '../../src/catalog-types';
import {catalogStyle as s} from '../../src/catalog-style';
export default function Projects(){
 const {tenant}=useLocalSearchParams<{tenant:string}>(),auth=useAuth();
 const {t}=useLocale();
 const canEdit=useWorkspaceAccess(tenant||'')?.canEdit===true,client=useMemo(()=>auth.client(),[auth]);
 const base=`/api/v1/tenants/${encodeURIComponent(tenant||'')}/projects`;
 const resource=useReadPage(`${base}:${canEdit}`,client,async(cursor,signal)=>{if(!tenant)throw new Error('Workspace required');const raw=await client.request<unknown>(`${base}?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,{signal});signal.throwIfAborted();return readProjectPage(raw,tenant,cursor);});
 const items=resource.data?.items??[];
 return <SafeAreaView style={s.root}><View style={s.header}><Text variant="headlineMedium" accessibilityRole="header" style={s.title}>{t('catalog_projects_title')}</Text><WorkspaceSettingsNavigation tenant={tenant||''}/>{tenant?<CatalogCreateControls key={tenant} client={client} origin={auth.origin} tenant={tenant} kind="project" enabled={canEdit&&resource.phase==='ready'&&resource.morePhase!=='loading'} current={resource.current} onCreated={value=>{if(resource.current()){const p=value as Project;resource.commit({...resource.data!,items:[p,...items.filter(item=>item.id!==p.id)]});}}}/>:null}</View>{resource.phase!=='ready'?<ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel}/>:<FlatList data={items} keyExtractor={x=>x.id} contentContainerStyle={s.list} ListFooterComponent={<ReadPageState {...resource}/>} ListEmptyComponent={<Text style={s.empty}>{t('catalog_projects_empty')}</Text>} renderItem={({item})=><Card style={{marginBottom:8}}><TouchableRipple accessibilityRole="button" onPress={()=>router.push({pathname:'/projects/[id]',params:{id:item.id,tenant}})}><Card.Content><View><Text variant="titleMedium" style={s.name}>{item.name}</Text><Text style={s.meta}>{t(`catalog_visibility_${item.visibility}`)}</Text></View><Text style={s.chevron}>›</Text></Card.Content></TouchableRipple></Card>}/>}</SafeAreaView>;
}
