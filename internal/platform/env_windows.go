//go:build windows

package platform

// envCaseSensitive reports whether environment variable names distinguish
// case. Windows does not: GetEnvironmentVariable and os.Getenv fold case, so
// `anthropic_api_key` and `ANTHROPIC_API_KEY` are one variable there. A prefix
// test that ignored this would leave a lowercase spelling of a variable that
// was supposed to be scrubbed sitting in the child's environment.
const envCaseSensitive = false
