package domain

// FeatureState combines the three switches that control a feature.
type FeatureState struct {
	Key             string
	GloballyEnabled bool
	InPlan          bool
	TenantEnabled   bool
}

// Enabled applies the rule: the platform allows it, the plan includes it and
// the store turned it on.
func (f FeatureState) Enabled() bool {
	return f.GloballyEnabled && f.InPlan && f.TenantEnabled
}
