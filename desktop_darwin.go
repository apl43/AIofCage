package main

// Wails v2.17's build command adds this framework for modern macOS file dialogs.
// Declare it here as well so the documented plain go build command links correctly.

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
