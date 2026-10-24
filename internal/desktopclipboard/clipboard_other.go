//go:build !darwin || !cgo

package desktopclipboard

func Image(limit int) ([]byte, error) { return nil, nil }
