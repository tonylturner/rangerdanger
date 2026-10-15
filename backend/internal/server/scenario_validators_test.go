package server

import (
	"context"
	"slices"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestValidatorRegistrations(t *testing.T) {
	expected := map[string][]string{
		"us-baseline-assessment":       {labs.CapabilityProcessElectrical, labs.CapabilityCaptureFirewall},
		"us-segmentation-requirements": {labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		"us-remediation-planning":      {labs.CapabilityProcessElectrical},
		"us-firewall-implementation":   {labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		"us-hardening-configurations":  {labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind, labs.CapabilityAuditDeviceControl},
		"us-vendor-rdp-compromise":     {labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		"us-validation-evidence":       {labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		"eu-iec104-telecontrol":        {labs.CapabilityPolicyContaind, labs.CapabilityProtocolIEC104, labs.CapabilityProcessTruth},
	}
	requirements := ValidatorRequirements()
	for key, want := range expected {
		if !slices.Equal(requirements[key], want) || scenarioValidators[key].run == nil {
			t.Errorf("registration %s = %v; want %v", key, requirements[key], want)
		}
		// A loader cannot mutate the live registry through the returned slice.
		requirements[key][0] = "mutated"
		if scenarioValidators[key].capabilities[0] != want[0] {
			t.Errorf("requirements aliased %s", key)
		}
	}
}

func TestValidatorRegistrationRejectsInvalid(t *testing.T) {
	run := func(context.Context, *Server, validatorInput) []ValidationCheck { return nil }
	for _, test := range []struct {
		name, key string
		validator scenarioValidator
	}{
		{"duplicate", "eu-iec104-telecontrol", scenarioValidator{run: run}},
		{"empty", "", scenarioValidator{run: run}},
		{"missing implementation", "invalid", scenarioValidator{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid validator registration did not panic")
				}
			}()
			registerScenarioValidator(test.key, test.validator)
		})
	}
}
