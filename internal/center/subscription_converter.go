package center

// Legacy external values remain readable for correction and backup. New writes
// may only select the built-in renderer.
func validSubscriptionEngine(engine string) bool {
	return engine == "" || engine == "builtin" || engine == "external"
}
