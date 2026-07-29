package config

import "fmt"

type ConfigurationError struct {
	Resource string
	Reason   string
}

func (e *ConfigurationError) Error() string {
	return fmt.Sprintf("invalid runtime configuration for %s: %s", e.Resource, e.Reason)
}

func IsConfigurationError(err error) bool {
	_, ok := err.(*ConfigurationError)
	return ok
}

type ResolutionError struct {
	Resource string
}

func (e *ResolutionError) Error() string {
	return fmt.Sprintf("runtime configuration resolution failed for %s", e.Resource)
}
