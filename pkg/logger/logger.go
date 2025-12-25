package logger

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Logger struct {
	logger zerolog.Logger
}

// New creates a new logger instance
func New(level string, environment string) *Logger {
	// Configure time format
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	// Set log level
	logLevel := parseLevel(level)
	zerolog.SetGlobalLevel(logLevel)

	// Create logger
	l := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Configure console output for development
	if environment == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	return &Logger{logger: l}
}

// parseLevel converts string level to zerolog.Level
func parseLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// With creates a child logger with additional fields
func (l *Logger) With() zerolog.Logger {
	return l.logger.With().Logger()
}

// Debug logs a debug message
func (l *Logger) Debug() *zerolog.Event {
	return l.logger.Debug()
}

// Info logs an info message
func (l *Logger) Info() *zerolog.Event {
	return l.logger.Info()
}

// Warn logs a warning message
func (l *Logger) Warn() *zerolog.Event {
	return l.logger.Warn()
}

// Error logs an error message
func (l *Logger) Error() *zerolog.Event {
	return l.logger.Error()
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal() *zerolog.Event {
	return l.logger.Fatal()
}

// Global logger functions for convenience
func Debug() *zerolog.Event {
	return log.Debug()
}

func Info() *zerolog.Event {
	return log.Info()
}

func Warn() *zerolog.Event {
	return log.Warn()
}

func Error() *zerolog.Event {
	return log.Error()
}

func Fatal() *zerolog.Event {
	return log.Fatal()
}
