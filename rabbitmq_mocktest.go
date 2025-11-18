package turbine

import (
	"fmt"
	"log"
	"os"
	"time"
)

type MockLogger struct {
	l       *log.Logger
	listMsg []string
}

func NewMockLogger() *MockLogger {
	return &MockLogger{
		l:       log.New(os.Stdout, "", 0),
		listMsg: []string{},
	}
}

func (l *MockLogger) Debugf(format string, v ...any) {
	l.output("DEBUG", format, v...)
}

func (l *MockLogger) Infof(format string, v ...any) {
	l.output("INFO", format, v...)
}

func (l *MockLogger) Warnf(format string, v ...any) {
	l.output("WARN", format, v...)
}

func (l *MockLogger) Errorf(format string, v ...any) {
	l.output("ERROR", format, v...)
}

func (l *MockLogger) output(level, format string, v ...any) {
	timestamp := time.Now().Format(time.RFC3339)
	msg := fmt.Sprintf(format, v...)
	l.l.Printf("[%s] %s: %s", timestamp, level, msg)
	l.listMsg = append(l.listMsg, msg)
}
