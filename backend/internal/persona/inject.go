package persona

import "github.com/mydisha/keirouter/backend/internal/core"

// Prepend returns msgs with the prompt installed as the leading system
// message.
//
// Policy:
//   - no prompt, or an empty message list -> messages unchanged
//   - a system message already present -> left alone, so a client that
//     deliberately sends its own persona (Hermes, Claude Code) always wins
//   - otherwise the persona is prepended ahead of the conversation
func Prepend(msgs []core.Message, prompt string) []core.Message {
	if prompt == "" {
		return msgs
	}
	for _, m := range msgs {
		if m.Role == core.RoleSystem {
			return msgs
		}
	}
	sys := core.Message{
		Role:    core.RoleSystem,
		Content: []core.ContentPart{{Type: core.PartText, Text: prompt}},
	}
	// Build a new slice so we never mutate the caller's backing array.
	out := make([]core.Message, 0, len(msgs)+1)
	out = append(out, sys)
	out = append(out, msgs...)
	return out
}
