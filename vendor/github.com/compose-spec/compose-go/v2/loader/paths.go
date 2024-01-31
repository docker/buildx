/*
   Copyright 2020 The Compose Specification Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package loader

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
)

// ResolveRelativePaths resolves relative paths based on project WorkingDirectory
func ResolveRelativePaths(project *types.Project) error {
	absWorkingDir, err := filepath.Abs(project.WorkingDir)
	if err != nil {
		return err
	}
	project.WorkingDir = absWorkingDir

	absComposeFiles, err := absComposeFiles(project.ComposeFiles)
	if err != nil {
		return err
	}
	project.ComposeFiles = absComposeFiles

	// don't coerce a nil map to an empty map
	if project.IncludeReferences != nil {
		absIncludes := make(map[string][]types.IncludeConfig, len(project.IncludeReferences))
		for filename, config := range project.IncludeReferences {
			filename = absPath(project.WorkingDir, filename)
			absConfigs := make([]types.IncludeConfig, len(config))
			for i, c := range config {
				absConfigs[i] = types.IncludeConfig{
					Path:             resolvePaths(project.WorkingDir, c.Path),
					ProjectDirectory: absPath(project.WorkingDir, c.ProjectDirectory),
					EnvFile:          resolvePaths(project.WorkingDir, c.EnvFile),
				}
			}
			absIncludes[filename] = absConfigs
		}
		project.IncludeReferences = absIncludes
	}

	return nil
}

func absPath(workingDir string, filePath string) string {
	if strings.HasPrefix(filePath, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, filePath[1:])
	}
	if filepath.IsAbs(filePath) {
		return filePath
	}
	return filepath.Join(workingDir, filePath)
}

func absComposeFiles(composeFiles []string) ([]string, error) {
	for i, composeFile := range composeFiles {
		absComposefile, err := filepath.Abs(composeFile)
		if err != nil {
			return nil, err
		}
		composeFiles[i] = absComposefile
	}
	return composeFiles, nil
}

func resolvePaths(basePath string, in types.StringList) types.StringList {
	if in == nil {
		return nil
	}
	ret := make(types.StringList, len(in))
	for i := range in {
		ret[i] = absPath(basePath, in[i])
	}
	return ret
}
