package app

import "encoding/json"

func (value bundleManifest) MarshalJSON() ([]byte, error) {
	type manifestJSON bundleManifest
	bounded := manifestJSON(value)
	bounded.Included = []string{"manifest.json", bundleCollectionEntryPath, bundleAnalysisEntryPath}
	return json.Marshal(bounded)
}
