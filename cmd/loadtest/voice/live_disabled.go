//go:build !voice_live_load

package main

import (
	"errors"
	"flag"
	"io"
)

var ErrLiveBuildDisabled = errors.New("live voice load mode requires the voice_live_load build tag")

func registerLiveFlags(*flag.FlagSet, commandOptions) liveCommand {
	return func(io.Writer, func(string) string) error {
		return ErrLiveBuildDisabled
	}
}
