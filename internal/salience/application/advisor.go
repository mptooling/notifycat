package application

import "github.com/mptooling/notifycat/internal/salience/domain"

// NewAdvisor picks the Advisor binding for the deployment: the deterministic
// advisor when the feature is off (or no gateway was built), the resilient
// model-backed advisor when it is on. Consumers never know which they got.
func NewAdvisor(params domain.AdvisorParams) domain.Advisor {
	if !params.Config.Enabled || params.Gateway == nil {
		return NewDeterministicAdvisor()
	}
	return NewResilientAdvisor(params)
}
