package confutil

import (
	"os"
)

// MetadataProvenanceMode is the type for setting provenance in the metdata file
type MetadataProvenanceMode int

const (
	// MetadataProvenanceModeNone doesn't set provenance (default)
	MetadataProvenanceModeNone MetadataProvenanceMode = iota
	// MetadataProvenanceModeMin sets provenance without buildConfig and metadata
	MetadataProvenanceModeMin
	// MetadataProvenanceModeMax sets full provenance
	MetadataProvenanceModeMax
)

// MetadataProvenance returns the provenance mode to set in the metadata file
func MetadataProvenance() MetadataProvenanceMode {
	switch os.Getenv("BUILDX_METADATA_PROVENANCE") {
	case "min":
		return MetadataProvenanceModeMin
	case "max":
		return MetadataProvenanceModeMax
	}
	return MetadataProvenanceModeNone
}
