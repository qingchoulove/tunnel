package tunnel

import (
	"github.com/sirupsen/logrus"
	"os"
)

var log = logrus.New()

func init() {
	log.Out = os.Stdout
	log.Level = logrus.DebugLevel
}
