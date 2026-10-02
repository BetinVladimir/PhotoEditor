import {FlatList,Pressable,Text,View} from 'react-native';
import type {SceneBoard} from './scene-board';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
/** Reference selection requests a new analysis; the displayed immutable plan is unchanged. */
export function SceneBoardView({board,onReference,busy=false}:{board:SceneBoard;onReference?:(groupId:string,versionId:string)=>void;busy?:boolean}){
 const {t}=useLocale();
 return <FlatList data={board.groups} keyExtractor={group=>group.id} initialNumToRender={8} maxToRenderPerBatch={8} windowSize={5}
  ListHeaderComponent={<Text accessibilityRole="header" style={s.title}>{t('scene_board_title')}</Text>}
  renderItem={({item:group,index})=><View style={{padding:12,gap:8}}><Text style={s.name}>{t('scene_group')} {index+1}</Text><Text style={s.meta}>{t('scene_confidence')}: {Math.round(group.confidence*100)}%</Text><Text style={s.meta}>{t('scene_outliers_unavailable')}</Text>
   <FlatList horizontal data={group.frames} keyExtractor={frame=>frame.versionId} initialNumToRender={10} maxToRenderPerBatch={10} windowSize={5} contentContainerStyle={{gap:6}}
    renderItem={({item:frame})=><Pressable accessibilityRole="button" accessibilityLabel={`${t('scene_reference_select')} ${frame.ordinal+1}`} accessibilityState={{selected:frame.ordinal===group.reference.ordinal,disabled:busy||!onReference}} disabled={busy||!onReference} onPress={()=>onReference?.(group.id,frame.versionId)} style={s.button}><Text style={s.buttonText}>{t('compare_frame')} {frame.ordinal+1}{frame.ordinal===group.reference.ordinal?` · ${t('scene_reference')}`:''}</Text></Pressable>}/>
  </View>}/>;
}
