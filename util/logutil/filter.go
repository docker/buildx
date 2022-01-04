package logutil

import (
	"io/ioutil"
	"strings"

	"github.com/sirupsen/logrus"
)

func NewFilter(filters ...string) logrus.Hook {
	dl := logrus.New()
	dl.SetOutput(ioutil.Discard)
	return &logsFilter{
		filters:       filters,
		discardLogger: dl,
	}
}

type logsFilter struct {
	filters       []string
	discardLogger *logrus.Logger
}

func (d *logsFilter) Levels() []logrus.Level {
	return []logrus.Level{logrus.DebugLevel}
}

func (d *logsFilter) Fire(entry *logrus.Entry) error {
	for _, f := range d.filters {
		if strings.Contains(entry.Message, f) {
			entry.Logger = d.discardLogger
			return nil
		}
	}
	return nil
}
