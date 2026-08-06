package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"invoice-backend/internal/voice/loadtest"
)

var ErrInvalidMode = errors.New("invalid voice load-test mode")

type commandOptions struct {
	mode     *string
	sessions *int
	duration *time.Duration
	dryRun   *bool
}

type liveCommand func(io.Writer, func(string) string) error

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("voice-load", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := commandOptions{
		mode:     flags.String("mode", "fake", "execution mode: fake; live requires a tagged build"),
		sessions: flags.Int("sessions", 120, "virtual or gated live session count"),
		duration: flags.Duration("duration", 30*time.Minute, "virtual or gated live duration"),
		dryRun:   flags.Bool("dry-run", true, "must remain true for fake mode"),
	}
	live := registerLiveFlags(flags, options)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return ErrInvalidMode
	}

	switch *options.mode {
	case "fake":
		report, err := loadtest.RunDry(context.Background(), loadtest.DryRunConfig{
			Sessions: *options.sessions, VirtualDuration: *options.duration, DryRun: *options.dryRun,
		})
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return fmt.Errorf("encode offline voice load report: %w", err)
		}
		return nil
	case "live":
		return live(stdout, getenv)
	default:
		return ErrInvalidMode
	}
}
