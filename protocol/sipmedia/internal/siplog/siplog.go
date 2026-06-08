// Package siplog provides the shared logrus logger for protocol/sipmedia subpackages.
package siplog

import "github.com/sirupsen/logrus"

// L is the standard logger; no initialization required.
var L = logrus.StandardLogger()
