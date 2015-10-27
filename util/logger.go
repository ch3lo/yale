package util

import (
	"fmt"

	log "github.com/Sirupsen/logrus"
)

var Log = log.New()

func PrintfAndLogInfof(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
	Log.Printf(format, args...)
}
