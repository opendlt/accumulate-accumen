package logz

import (
	"fmt"
	"log"
	"os"
	"time"
)

// LogLevel represents the severity level of log messages
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging with levels
type Logger struct {
	level  LogLevel
	prefix string
	logger *log.Logger
}

// New creates a new logger with the specified minimum level and prefix
func New(level LogLevel, prefix string) *Logger {
	return &Logger{
		level:  level,
		prefix: prefix,
		logger: log.New(os.Stdout, "", 0), // We'll handle our own formatting
	}
}

// Default creates a logger with INFO level and no prefix
func Default() *Logger {
	return New(INFO, "")
}

// WithPrefix creates a new logger with an additional prefix
func (l *Logger) WithPrefix(prefix string) *Logger {
	newPrefix := l.prefix
	if newPrefix != "" {
		newPrefix = fmt.Sprintf("%s:%s", newPrefix, prefix)
	} else {
		newPrefix = prefix
	}
	return &Logger{
		level:  l.level,
		prefix: newPrefix,
		logger: l.logger,
	}
}

// SetLevel sets the minimum logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// shouldLog returns true if the message should be logged based on current level
func (l *Logger) shouldLog(level LogLevel) bool {
	return level >= l.level
}

// formatMessage formats a log message with timestamp, level, prefix, and message
func (l *Logger) formatMessage(level LogLevel, format string, args ...interface{}) string {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	message := fmt.Sprintf(format, args...)

	if l.prefix != "" {
		return fmt.Sprintf("[%s] %s [%s] %s", timestamp, level.String(), l.prefix, message)
	}
	return fmt.Sprintf("[%s] %s %s", timestamp, level.String(), message)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.shouldLog(DEBUG) {
		l.logger.Print(l.formatMessage(DEBUG, format, args...))
	}
}

// Info logs an info message
func (l *Logger) Info(format string, args ...interface{}) {
	if l.shouldLog(INFO) {
		l.logger.Print(l.formatMessage(INFO, format, args...))
	}
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	if l.shouldLog(WARN) {
		l.logger.Print(l.formatMessage(WARN, format, args...))
	}
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	if l.shouldLog(ERROR) {
		l.logger.Print(l.formatMessage(ERROR, format, args...))
	}
}

// Fatal logs an error message and exits the program
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.logger.Print(l.formatMessage(ERROR, format, args...))
	os.Exit(1)
}

// Package-level logger instance
var defaultLogger = Default()

// Package-level logging functions

// SetDefaultLevel sets the level for the default logger
func SetDefaultLevel(level LogLevel) {
	defaultLogger.SetLevel(level)
}

// SetDefaultPrefix sets a prefix for the default logger
func SetDefaultPrefix(prefix string) {
	defaultLogger.prefix = prefix
}

// Debug logs a debug message using the default logger
func Debug(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

// Info logs an info message using the default logger
func Info(format string, args ...interface{}) {
	defaultLogger.Info(format, args...)
}

// Warn logs a warning message using the default logger
func Warn(format string, args ...interface{}) {
	defaultLogger.Warn(format, args...)
}

// Error logs an error message using the default logger
func Error(format string, args ...interface{}) {
	defaultLogger.Error(format, args...)
}

// Fatal logs an error message and exits using the default logger
func Fatal(format string, args ...interface{}) {
	defaultLogger.Fatal(format, args...)
}

// ParseLevel parses a string log level
func ParseLevel(level string) (LogLevel, error) {
	switch level {
	case "debug", "DEBUG":
		return DEBUG, nil
	case "info", "INFO":
		return INFO, nil
	case "warn", "WARN", "warning", "WARNING":
		return WARN, nil
	case "error", "ERROR":
		return ERROR, nil
	default:
		return INFO, fmt.Errorf("unknown log level: %s", level)
	}
}
