package catalog

// MonochromeManifestAttributes holds the playback attributes a Monochrome
// instance returns for a track. `URI` points at a time-limited manifest hosted
// on TIDAL's CDN, which must be fetched separately.
type MonochromeManifestAttributes struct {
	URI               string   `json:"uri"`
	Hash              string   `json:"hash"`
	TrackPresentation string   `json:"trackPresentation"`
	PreviewReason     string   `json:"previewReason"`
	Formats           []string `json:"formats"`
}

// MonochromeManifestResource is the JSON:API resource wrapping the attributes.
type MonochromeManifestResource struct {
	ID         string                       `json:"id"`
	Type       string                       `json:"type"`
	Attributes MonochromeManifestAttributes `json:"attributes"`
}

// MonochromeTrackManifestResponse is the /trackManifests/ envelope. Instances
// differ on how deeply the resource is nested, so both layouts are accepted.
type MonochromeTrackManifestResponse struct {
	Data struct {
		Data       *MonochromeManifestResource   `json:"data"`
		Attributes *MonochromeManifestAttributes `json:"attributes"`
	} `json:"data"`
}

// Attributes returns the playback attributes regardless of nesting, or nil when
// the response carried none.
func (r *MonochromeTrackManifestResponse) Attributes() *MonochromeManifestAttributes {
	if r.Data.Data != nil {
		return &r.Data.Data.Attributes
	}
	return r.Data.Attributes
}
