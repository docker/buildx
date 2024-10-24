package main

import (
	"context"
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/moby/buildkit/util/bklog"
	"github.com/sirupsen/logrus"
)

func setupDebugProfiles(ctx context.Context) (stop func()) {
	var stopFuncs []func()
	if fn := setupCPUProfile(ctx); fn != nil {
		stopFuncs = append(stopFuncs, fn)
	}
	if fn := setupHeapProfile(ctx); fn != nil {
		stopFuncs = append(stopFuncs, fn)
	}
	return func() {
		for _, fn := range stopFuncs {
			fn()
		}
	}
}

func setupCPUProfile(ctx context.Context) (stop func()) {
	if cpuProfile := os.Getenv("BUILDX_CPU_PROFILE"); cpuProfile != "" {
		f, err := os.Create(cpuProfile)
		if err != nil {
			bklog.G(ctx).Warn("could not create cpu profile", logrus.WithError(err))
			return nil
		}

		if err := pprof.StartCPUProfile(f); err != nil {
			bklog.G(ctx).Warn("could not start cpu profile", logrus.WithError(err))
			_ = f.Close()
			return nil
		}

		return func() {
			pprof.StopCPUProfile()
			if err := f.Close(); err != nil {
				bklog.G(ctx).Warn("could not close file for cpu profile", logrus.WithError(err))
			}
		}
	}
	return nil
}

func setupHeapProfile(ctx context.Context) (stop func()) {
	if heapProfile := os.Getenv("BUILDX_MEM_PROFILE"); heapProfile != "" {
		// Memory profile is only created on stop.
		return func() {
			f, err := os.Create(heapProfile)
			if err != nil {
				bklog.G(ctx).Warn("could not create memory profile", logrus.WithError(err))
				return
			}

			// get up-to-date statistics
			runtime.GC()

			if err := pprof.WriteHeapProfile(f); err != nil {
				bklog.G(ctx).Warn("could not write memory profile", logrus.WithError(err))
			}

			if err := f.Close(); err != nil {
				bklog.G(ctx).Warn("could not close file for memory profile", logrus.WithError(err))
			}
		}
	}
	return nil
}
