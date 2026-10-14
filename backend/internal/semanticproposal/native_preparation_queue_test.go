package semanticproposal

import (
 "testing"
 "errors"
 "photoeditor/backend/internal/v2input"
)

func TestNativePreparationSavedQueue(t *testing.T) {
 for _, tc := range []struct { config string; legacy, valid bool }{
  {`{"project_id":"historical"}`, true, true},
  {`{"queue_class":"batch","v27_defect_native_prepare":{}}`, false, true},
  {`{"queue_class":"interactive"}`, false, false},
  {`{"queue_class":"training"}`, false, false},
  {`{"queue_class":null}`, false, false},
  {`{"queue_class":""}`, false, false},
  {`{"queue_class":1}`, false, false},
  {`{"queue_class":[]}`, false, false},
  {`null`, false, false},
  {`[]`, false, false},
  {`{`, false, false},
 } {
  t.Run(tc.config, func(t *testing.T) {
   legacy, err := nativePreparationLegacyQueue(tc.config)
   if tc.valid { if err != nil || legacy != tc.legacy { t.Fatal(legacy, err) } } else if !errors.Is(err, v2input.ErrInvalid) { t.Fatal("invalid saved queue accepted", err) }
  })
 }
}
