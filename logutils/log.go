package logutils

import (
	"log"
	"os"
	"strings"

	"github.com/hashicorp/logutils"
)

func SetupLogger(logLevel string) {
	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "WARN", "ERROR"},
		MinLevel: logutils.LogLevel(strings.ToUpper(logLevel)),
		Writer:   os.Stderr,
	}
	log.SetOutput(filter)
}
