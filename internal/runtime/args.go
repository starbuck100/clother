package runtime

import "strings"

func NormalizeClaudeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	hasDangerous := false
	for _, arg := range args {
		if arg == "--dangerously-skip-permissions" {
			hasDangerous = true
		}
	}
	for _, arg := range args {
		if arg == "--yolo" {
			if hasDangerous {
				continue
			}
			arg = "--dangerously-skip-permissions"
			hasDangerous = true
		}
		out = append(out, arg)
	}
	return out
}

func ModelOverride(args []string) string {
	model := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--model" && i+1 < len(args) {
			i++
			model = strings.TrimSpace(args[i])
		} else if strings.HasPrefix(arg, "--model=") {
			model = strings.TrimSpace(strings.TrimPrefix(arg, "--model="))
		}
	}
	return model
}
