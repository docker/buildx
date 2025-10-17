package tests

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

var versionTests = []func(t *testing.T, sb integration.Sandbox){
	testVersion,
}

func testVersion(t *testing.T, sb integration.Sandbox) {
	cmd := buildxCmd(sb, withArgs("version"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	// There should be at least one newline and the first line
	// of output should contain the name, version, and possibly a revision.
	firstLine, _, hasNewline := strings.Cut(string(out), "\n")
	require.True(t, hasNewline, "At least one newline is required in the output")

	// Log the output to make debugging easier.
	t.Log(firstLine)

	// Split by spaces into at least 2 fields.
	fields := strings.Fields(firstLine)
	require.GreaterOrEqual(t, len(fields), 2, "Expected at least 2 fields in the first line")

	// First field should be an import path.
	// This can be any valid import path for Go
	// so don't set too many restrictions here.
	// Just checking if the import path is a valid Go
	// path should be suitable enough to make sure this is ok.
	// Using CheckImportPath instead of CheckPath as it is less
	// restrictive.
	importPath := fields[0]
	require.NoError(t, module.CheckImportPath(importPath), "First field was not a valid import path: %+v", importPath)

	// Second field should be a version.
	// This defaults to something that's still compatible
	// with semver.
	version := fields[1]
	// Some downstream distributions strip the initial "v"
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	require.True(t, semver.IsValid(version), "Second field was not valid semver: %+v", version)
}
