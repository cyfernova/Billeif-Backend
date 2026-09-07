package verify

import (
	"fmt"
	"strings"
)

// Environment identifies the deployment environment the harness is allowed to
// exercise. Selection is always explicit: the harness never infers the target
// from ambient configuration.
type Environment string

const (
	// EnvironmentProduction is the production deployment. It is refused unless
	// the operator passes the deliberate override flag.
	EnvironmentProduction Environment = "production"

	// ReasonProductionRequiresOverride is the stable refusal reason emitted when
	// a production target is selected without the deliberate override.
	ReasonProductionRequiresOverride = "production_requires_explicit_override"
)

// ProductionRefusalError is returned when the harness is pointed at production
// without the deliberate --allow-production override.
type ProductionRefusalError struct {
	Environment string
	Reason      string
}

func (e *ProductionRefusalError) Error() string {
	return fmt.Sprintf(
		"refusing to verify environment %q: %s (re-run with --allow-production to accept responsibility for a production target)",
		e.Environment, e.Reason,
	)
}

// SelectEnvironment normalizes the requested environment and refuses
// production unless allowProduction is explicitly true. Non-production
// environments are passed through normalized so reports carry a stable value.
func SelectEnvironment(raw string, allowProduction bool) (Environment, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "":
		return "", fmt.Errorf("environment selection is required (--environment staging|dev|qa)")
	case "prod", "production":
		if !allowProduction {
			return "", &ProductionRefusalError{
				Environment: EnvironmentProduction.String(),
				Reason:      ReasonProductionRequiresOverride,
			}
		}
		return EnvironmentProduction, nil
	case "dev", "local", "staging", "qa", "preview":
		return Environment(normalized), nil
	default:
		return "", fmt.Errorf("unknown environment %q: expected one of dev, local, staging, qa, preview", raw)
	}
}

// String returns the stable serialization of the environment.
func (e Environment) String() string { return string(e) }

// IsProduction reports whether the environment is the production deployment.
func (e Environment) IsProduction() bool { return e == EnvironmentProduction }
