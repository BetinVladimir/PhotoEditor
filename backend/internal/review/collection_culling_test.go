package review
import("testing";"encoding/json";"github.com/google/uuid")
func TestCollectionCullingMapsExactCurrentVersionAndCountsHistoricalTargets(t *testing.T){
 c,_:=cullingFixture();first:=c.Projection.Items[0];second:=first;second.ItemID=cullingID(30);second.AssetID=cullingID(31);second.VersionID=cullingID(32);c.Projection.Items=append(c.Projection.Items,second)
 members:=[]collectionMember{{cullingID(40),first.AssetID,first.VersionID},{cullingID(41),second.AssetID,cullingID(99)}}
 out:=mapCollectionCulling(c,members,true);if out.Total!=2||out.Matched!=1||out.Unmatched!=1||len(out.Items)!=1||out.Items[0].CollectionItemID!=cullingID(40)||out.Items[0].VersionID!=first.VersionID{t.Fatal("current version substituted or historical target hidden",out)}
 out.Items[0].SceneID="changed";if c.Projection.Items[0].SceneID=="changed"{t.Fatal("source mutated")}
 summary:=mapCollectionCulling(c,members,false);if summary.Items!=nil{t.Fatal("discovery leaked full inventory")};body,e:=json.Marshal(summary);if e!=nil||!json.Valid(body){t.Fatal(e)}
}
func TestCollectionCullingDoesNotMapSameVersionUnderDifferentAsset(t *testing.T){c,_:=cullingFixture();out:=mapCollectionCulling(c,[]collectionMember{{uuid.New(),uuid.New(),c.Projection.Items[0].VersionID}},true);if out.Matched!=0||out.Unmatched!=1||len(out.Items)!=0{t.Fatal(out)}}
