package selection

import "encoding/json"

type geoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

type geoJSONFeature struct {
	Type       string              `json:"type"`
	Geometry   geoJSONPoint        `json:"geometry"`
	Properties geoJSONProperties   `json:"properties"`
}

type geoJSONPoint struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"` // GeoJSON order: [longitude, latitude]
}

type geoJSONProperties struct {
	ProbeID     uint32   `json:"probe_id"`
	ASN4        uint32   `json:"asn4"`
	CountryCode string   `json:"country_code"`
	Tags        []string `json:"tags"`
}

// GeoJSON serialises the selected probe set for this round as a GeoJSON
// FeatureCollection. Each probe becomes a Point feature.
func (r SelectedRound) GeoJSON() ([]byte, error) {
	features := make([]geoJSONFeature, len(r.Probes))
	for i, p := range r.Probes {
		features[i] = geoJSONFeature{
			Type: "Feature",
			Geometry: geoJSONPoint{
				Type:        "Point",
				Coordinates: [2]float64{p.Lon, p.Lat},
			},
			Properties: geoJSONProperties{
				ProbeID:     p.ID,
				ASN4:        p.ASN4,
				CountryCode: p.CountryCode,
				Tags:        p.Tags,
			},
		}
	}
	return json.Marshal(geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: features,
	})
}
