// Package settings defines persisted display preferences, not transient input.
package settings

type Display struct {
	RegistrationFormVisible bool
}

func DefaultDisplay() Display {
	return Display{RegistrationFormVisible: true}
}
