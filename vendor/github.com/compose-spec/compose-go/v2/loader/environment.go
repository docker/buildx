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
	"fmt"

	"github.com/compose-spec/compose-go/v2/types"
)

// Will update the environment variables for the format {- VAR} (without interpolation)
// This function should resolve context environment vars for include (passed in env_file)
func resolveServicesEnvironment(dict map[string]any, config types.ConfigDetails) {
	services, ok := dict["services"].(map[string]any)
	if !ok {
		return
	}

	for service, cfg := range services {
		serviceConfig, ok := cfg.(map[string]any)
		if !ok {
			continue
		}
		serviceEnv, ok := serviceConfig["environment"].([]any)
		if !ok {
			continue
		}
		envs := []any{}
		for _, env := range serviceEnv {
			varEnv, ok := env.(string)
			if !ok {
				continue
			}
			if found, ok := config.Environment[varEnv]; ok {
				envs = append(envs, fmt.Sprintf("%s=%s", varEnv, found))
			} else {
				// either does not exist or it was already resolved in interpolation
				envs = append(envs, varEnv)
			}
		}
		serviceConfig["environment"] = envs
		services[service] = serviceConfig
	}
	dict["services"] = services
}
