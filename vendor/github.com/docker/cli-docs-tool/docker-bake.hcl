// Copyright 2021 cli-docs-tool authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

variable "GO_VERSION" {
  default = "1.16"
}

group "default" {
  targets = ["test"]
}

group "validate" {
  targets = ["lint", "vendor-validate", "license-validate"]
}

target "lint" {
  args = {
    GO_VERSION = GO_VERSION
  }
  dockerfile = "./hack/lint.Dockerfile"
  target = "lint"
  output = ["type=cacheonly"]
}

target "vendor-validate" {
  args = {
    GO_VERSION = GO_VERSION
  }
  dockerfile = "./hack/vendor.Dockerfile"
  target = "validate"
  output = ["type=cacheonly"]
}

target "vendor-update" {
  args = {
    GO_VERSION = GO_VERSION
  }
  dockerfile = "./hack/vendor.Dockerfile"
  target = "update"
  output = ["."]
}

target "test" {
  args = {
    GO_VERSION = GO_VERSION
  }
  dockerfile = "./hack/test.Dockerfile"
  target = "test-coverage"
  output = ["."]
}

target "license-validate" {
  dockerfile = "./hack/license.Dockerfile"
  target = "validate"
  output = ["type=cacheonly"]
}

target "license-update" {
  dockerfile = "./hack/license.Dockerfile"
  target = "update"
  output = ["."]
}
