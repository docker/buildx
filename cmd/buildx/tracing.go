package main

import (
	"github.com/moby/buildkit/util/tracing/detect"
	"go.opentelemetry.io/otel"

	_ "github.com/moby/buildkit/util/tracing/env"
)

func init() {
	detect.ServiceName = "buildx"
	// do not log tracing errors to stdio
	otel.SetErrorHandler(skipErrors{})
}

type skipErrors struct{}

func (skipErrors) Handle(err error) {}
