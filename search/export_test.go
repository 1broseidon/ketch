package search

// RegisterTestProvider provides a single registration point for cross-package
// integration tests. Production registration remains the explicit ordered slice.
func RegisterTestProvider(p Provider) func() {
	previous := providers
	providers = append(Providers(), p)
	return func() { providers = previous }
}
