package gossip

import (
	"strings"

	"go.uber.org/zap"
)

type logger struct {
	l *zap.Logger
}

func (l *logger) Write(p []byte) (n int, err error) {
	msg := string(p)
	parts := strings.SplitN(msg, " ", 2)
	lvl := "[DEBUG]"
	if len(parts) > 1 {
		lvl = parts[0]
		msg = strings.TrimPrefix(parts[1], "memberlist: ")
	}

	switch lvl {
	case "[DEBUG]":
		l.l.Debug(msg)
	case "[WARN]":
		l.l.Warn(msg)
	case "[INFO]":
		l.l.Info(msg)
	}
	return len(p), nil
}
