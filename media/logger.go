package media

import (
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func init() {
	log.SetFormatter(&logrus.JSONFormatter{})
	log.SetLevel(logrus.InfoLevel)
}

func SetLogLevel(level logrus.Level) {
	log.SetLevel(level)
}

func GetLogger() *logrus.Logger {
	return log
}
