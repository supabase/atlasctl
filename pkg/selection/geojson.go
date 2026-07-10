package selection

import "encoding/json"

type geoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

type geoJSONFeature struct {
	Type       string            `json:"type"`
	Geometry   geoJSONPoint      `json:"geometry"`
	Properties geoJSONProperties `json:"properties"`
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
	Round       string   `json:"round"`
}

// GeoJSONRound serialises a single SelectedRound as a GeoJSON FeatureCollection.
func GeoJSONRound(r SelectedRound) ([]byte, error) {
	features := make([]geoJSONFeature, 0, len(r.Probes))
	for _, p := range r.Probes {
		features = append(features, geoJSONFeature{
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
				Round:       r.Round.Name,
			},
		})
	}
	return json.Marshal(geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: features,
	})
}

// GeoJSON serialises all selected rounds as a single GeoJSON FeatureCollection.
// Each probe becomes a Point feature with a "round" property identifying which
// round it was selected for. A single collection is valid GeoJSON and can be
// loaded directly into geojson.io or any GIS tool.
func GeoJSON(rounds []SelectedRound) ([]byte, error) {
	var features []geoJSONFeature
	for _, r := range rounds {
		for _, p := range r.Probes {
			features = append(features, geoJSONFeature{
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
					Round:       r.Round.Name,
				},
			})
		}
	}
	if features == nil {
		features = []geoJSONFeature{}
	}
	return json.Marshal(geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: features,
	})
}
