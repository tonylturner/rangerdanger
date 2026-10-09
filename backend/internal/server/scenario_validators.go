package server

import (
	"slices"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

// validatorInput is the evidence a validator reads. state is fetched only
// for validators that need process.electrical, audit only for those that
// need audit.device-control.
type validatorInput struct {
	state        map[string]any
	audit        []map[string]any
	activeConfig string
}

// scenarioValidator is the Go code behind one declared validator key and
// the package capabilities it relies on.
type scenarioValidator struct {
	capabilities []string
	run          func(s *Server, in validatorInput) []ValidationCheck
}

func (v scenarioValidator) needs(capability string) bool {
	return slices.Contains(v.capabilities, capability)
}

// scenarioValidators maps each `validator:` key a scenario YAML may declare
// to its implementation. The loader rejects a key missing here, or one whose
// capabilities the scenario's package does not declare.
var scenarioValidators = map[string]scenarioValidator{
	"us-baseline-assessment": {
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityCaptureFirewall},
		run: func(s *Server, in validatorInput) []ValidationCheck {
			return s.validateBaselineAssessment(in.state, in.audit, in.activeConfig)
		},
	},
	"us-segmentation-requirements": {
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		run: func(_ *Server, in validatorInput) []ValidationCheck {
			return validateSegmentationRequirements(in.state, in.audit, in.activeConfig)
		},
	},
	"us-remediation-planning": {
		capabilities: []string{labs.CapabilityProcessElectrical},
		run: func(_ *Server, in validatorInput) []ValidationCheck {
			return validateRemediationPlanning(in.state, in.activeConfig)
		},
	},
	"us-firewall-implementation": {
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		run: func(_ *Server, in validatorInput) []ValidationCheck {
			return validateFirewallImplementation(in.state, in.audit, in.activeConfig)
		},
	},
	"us-hardening-configurations": {
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind, labs.CapabilityAuditDeviceControl},
		run: func(_ *Server, in validatorInput) []ValidationCheck {
			return validateHardeningConfigurations(in.state, in.audit, in.activeConfig)
		},
	},
	"us-vendor-rdp-compromise": {
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		run: func(_ *Server, in validatorInput) []ValidationCheck {
			return validateVendorRDPCompromise(in.state, in.audit, in.activeConfig)
		},
	},
	"us-validation-evidence": {
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		run: func(_ *Server, in validatorInput) []ValidationCheck {
			return validateValidationEvidence(in.state, in.audit, in.activeConfig)
		},
	},
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
