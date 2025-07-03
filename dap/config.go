package dap

import "github.com/docker/buildx/dap/common"

type LaunchConfig interface {
	GetConfig() common.Config
}
