package confutil

import (
	"os"
	"strconv"
)

// MetadataProvenanceMode is the type for setting provenance in the metadata file
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

// MetadataStatusMode is the type for setting status in the metadata file
type MetadataStatusMode int

const (
	// MetadataStatusModeDisabled doesn't set status (default)
	MetadataStatusModeDisabled MetadataStatusMode = iota
	// MetadataStatusModeWarnings sets only status warnings
	MetadataStatusModeWarnings
	// MetadataStatusModeMax sets full status
	MetadataStatusModeMax
)

// MetadataStatus returns the status mode to set in the metadata file
func MetadataStatus() MetadataStatusMode {
	bmp := os.Getenv("BUILDX_METADATA_STATUS")
	switch bmp {
	case "warnings":
		return MetadataStatusModeWarnings
	case "max":
		return MetadataStatusModeMax
	case "disabled":
		return MetadataStatusModeDisabled
	}
	if ok, err := strconv.ParseBool(bmp); err == nil && !ok {
		return MetadataStatusModeDisabled
	}
	return MetadataStatusModeDisabled
}
