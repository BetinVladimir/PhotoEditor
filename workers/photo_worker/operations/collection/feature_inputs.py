"""Read canonical bounded feature batches without undeclared nested object IO."""
from photo_worker.contracts.collection import SceneFeatures,SceneFeaturePack


def load_features(artifacts,params,check_active=lambda:None):
    refs=[];features=[]
    if params.feature_batches:
        for batch in params.feature_batches:
            check_active()
            pack=artifacts.contract(batch,SceneFeaturePack)
            refs.extend(entry.feature for entry in pack.features)
            features.extend(entry.value for entry in pack.features)
    else:
        for ref in params.features:
            check_active();refs.append(ref);features.append(artifacts.contract(ref,SceneFeatures))
    if not 1<=len(features)<=5000 or len({f.item_id for f in features})!=len(features) or len({(r.artifact_id,r.sha256) for r in refs})!=len(refs):
        raise ValueError('Duplicate or excessive immutable collection feature inventory')
    return refs,features
