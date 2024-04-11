package confutil

import (
	"os"
	"strconv"
)

// MetadataProvenanceMode is the type for setting provenance in the metdata file
type MetadataProvenanceMode int

const (
	// MetadataProvenanceModeMin sets minimal provenance (default)
	MetadataProvenanceModeMin MetadataProvenanceMode = iota
	// MetadataProvenanceModeMax sets full provenance
	MetadataProvenanceModeMax
	// MetadataProvenanceModeDisabled doesn't set provenance
	MetadataProvenanceModeDisabled
)

// MetadataProvenance returns the provenance mode to set in the metadata file
func MetadataProvenance() MetadataProvenanceMode {
	bmp := os.Getenv("BUILDX_METADATA_PROVENANCE")
	switch bmp {
	case "min":
		return MetadataProvenanceModeMin
	case "max":
		return MetadataProvenanceModeMax
	case "disabled":
		return MetadataProvenanceModeDisabled
	}
	if ok, err := strconv.ParseBool(bmp); err == nil && !ok {
		return MetadataProvenanceModeDisabled
	}
	return MetadataProvenanceModeMin
}
