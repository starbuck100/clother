//go:build windows

package platform

// IsWindows lets callers pick their wording without each of them repeating a
// GOOS comparison. It is a constant, so the branches it guards are resolved at
// compile time and the other side is not even type-checked into the binary.
const IsWindows = true
