package logutil

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestPauseConcurrent(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(&bytes.Buffer{})

	firstResume := Pause(logger)
	resumed := make(chan struct{})
	go func() {
		Pause(logger)()
		close(resumed)
	}()

	select {
	case <-resumed:
	case <-time.After(time.Second):
		t.Fatal("concurrent pause waited for active pause")
	}

	firstResume()
}

func TestPauseInitializationConcurrent(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(&bytes.Buffer{})

	start := make(chan struct{})
	resumes := make(chan func(), 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			resumes <- Pause(logger)
		}()
	}

	close(start)
	for i := 0; i < 2; i++ {
		(<-resumes)()
	}
}

func TestPauseBuffersOverlappingOutput(t *testing.T) {
	output := &bytes.Buffer{}
	logger := logrus.New()
	logger.SetOutput(output)

	firstResume := Pause(logger)
	secondResume := Pause(logger)
	logger.Print("message")
	secondResume()

	if output.Len() != 0 {
		t.Fatalf("got output before all pauses resumed: %q", output.String())
	}

	firstResume()
	if !strings.Contains(output.String(), "message") {
		t.Fatalf("missing buffered output: %q", output.String())
	}
}

func TestPauseResumeIsIdempotent(t *testing.T) {
	output := &bytes.Buffer{}
	logger := logrus.New()
	logger.SetOutput(output)

	firstResume := Pause(logger)
	secondResume := Pause(logger)

	firstResume()
	firstResume()

	logger.Print("message")
	if output.Len() != 0 {
		t.Fatalf("double resume cancelled a sibling pause: %q", output.String())
	}

	secondResume()
	if !strings.Contains(output.String(), "message") {
		t.Fatalf("missing buffered output: %q", output.String())
	}
}
