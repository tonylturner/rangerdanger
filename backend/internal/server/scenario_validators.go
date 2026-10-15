package server

import (
	"context"
	"slices"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// validatorInput is the evidence a validator reads. state is fetched only
// for validators that need process.electrical, audit only for those that
// need audit.device-control.
type validatorInput struct {
	state        map[string]any
	audit        []map[string]any
	activeConfig string
	gen          *lifecycle.Generation // the range, for capture evidence
}

// scenarioValidator is the Go code behind one declared validator key and
// the package capabilities it relies on.
type scenarioValidator struct {
	capabilities []string
	run          func(ctx context.Context, s *Server, in validatorInput) []ValidationCheck
}

func (v scenarioValidator) needs(capability string) bool {
	return slices.Contains(v.capabilities, capability)
}

// scenarioValidators is populated by the files that implement validators.
// Registration happens only at startup; request handlers only read it.
var scenarioValidators = map[string]scenarioValidator{}

func registerScenarioValidator(key string, validator scenarioValidator) {
	if key == "" || validator.run == nil {
		panic("invalid scenario validator registration: " + key)
	}
	if _, exists := scenarioValidators[key]; exists {
		panic("duplicate scenario validator: " + key)
	}
	scenarioValidators[key] = validator
}

// ValidatorRequirements reports every registered validator key and the
// capabilities it needs, for the package loader.
func ValidatorRequirements() labs.ValidatorRequirements {
	requirements := make(labs.ValidatorRequirements, len(scenarioValidators))
	for key, validator := range scenarioValidators {
		requirements[key] = slices.Clone(validator.capabilities)
	}
	return requirements
}
