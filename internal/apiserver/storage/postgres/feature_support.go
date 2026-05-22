package postgres

import (
	"k8s.io/apiserver/pkg/storage"
	etcdfeature "k8s.io/apiserver/pkg/storage/feature"
)

// FeatureSupportChecker advertises storage features supported by the Postgres backend.
type FeatureSupportChecker struct {
	etcdfeature.FeatureSupportChecker
}

// NewFeatureSupportChecker returns a checker that reports support for
// features the Postgres Store implements.
func NewFeatureSupportChecker() *FeatureSupportChecker {
	return &FeatureSupportChecker{
		FeatureSupportChecker: etcdfeature.DefaultFeatureSupportChecker,
	}
}

// Supports overrides the embedded checker.
func (f *FeatureSupportChecker) Supports(feature storage.Feature) bool {
	switch feature {
	case storage.RequestWatchProgress:
		return true
	}
	return f.FeatureSupportChecker.Supports(feature)
}
