package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestSuggestCommand(t *testing.T) {
	commands := []*cli.Command{
		{Name: "create"},
		{Name: "retrieve"},
		{Name: "list"},
		{Name: "delete"},
		{Name: "chat:completions"},
		{Name: "completions"},
	}

	tests := []struct {
		name     string
		provided string
		want     string
	}{
		{
			name:     "close typo suggests the corrected command",
			provided: "creat",
			want:     "Did you mean 'create'?",
		},
		{
			name:     "near-exact suggests the corrected command",
			provided: "chat:completion",
			want:     "Did you mean 'chat:completions'?",
		},
		{
			name:     "exact match still suggests itself",
			provided: "create",
			want:     "Did you mean 'create'?",
		},
		{
			name:     "uppercase input is matched ignoring case",
			provided: "CREAT",
			want:     "Did you mean 'create'?",
		},
		{
			name:     "unrelated input returns no suggestion",
			provided: "zzzzz",
			want:     "",
		},
		{
			name:     "low-similarity input returns no suggestion",
			provided: "totallybogus",
			want:     "",
		},
		{
			name:     "empty input returns no suggestion",
			provided: "",
			want:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := suggestCommand(commands, tc.provided)
			if got != tc.want {
				t.Errorf("suggestCommand(%q) = %q, want %q", tc.provided, got, tc.want)
			}
		})
	}
}

func TestSuggestCommandEmptyCommands(t *testing.T) {
	if got := suggestCommand(nil, "anything"); got != "" {
		t.Errorf("suggestCommand(nil, %q) = %q, want empty string", "anything", got)
	}
}

func TestWithinOneEdit(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"", "a", true},
		{"", "ab", false},
		{"run", "run", true},
		{"run", "rn", true},   // deletion
		{"run", "runn", true}, // insertion
		{"run", "ran", true},  // substitution
		{"run", "rnu", true},  // transposition
		{"run", "urn", true},  // transposition
		{"ab", "ba", true},    // transposition
		{"run", "rm", false},
		{"run", "nur", false},
		{"list", "ls", false},
		{"create", "craete", true},
		{"create", "carete", false},
		{"create", "caerte", false},
		{"run", "urm", false}, // swapped pair plus another change
		{"run", "xrn", false}, // only one side of the swap matches
	}
	for _, tc := range tests {
		for _, pair := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
			if got := withinOneEdit(pair[0], pair[1]); got != tc.want {
				t.Errorf("withinOneEdit(%q, %q) = %v, want %v", pair[0], pair[1], got, tc.want)
			}
		}
	}
}

// findCommand resolves a command path in the real command tree.
func findCommand(t *testing.T, path ...string) *cli.Command {
	t.Helper()
	command := Command
	for _, name := range path {
		var next *cli.Command
		for _, sub := range command.Commands {
			if sub.Name == name {
				next = sub
			}
		}
		if next == nil {
			t.Fatalf("command %q not found under %q", name, command.Name)
		}
		command = next
	}
	return command
}

// Parent links are only set once the command tree runs, so these suggestions
// name the matched command alone; cmd/openai checks the full "openai ..." form.
func TestSuggestCommandRealTree(t *testing.T) {
	tests := []struct {
		path     []string
		provided string
		want     string
	}{
		{[]string{"fine-tuning:alpha:graders"}, "rn", "run"},
		{[]string{"fine-tuning:alpha:graders"}, "rnu", "run"},
		{[]string{"fine-tuning:alpha:graders"}, "urn", "run"},
		{[]string{"fine-tuning:alpha:graders"}, "RN", "run"},
		{[]string{"fine-tuning:alpha:graders"}, "valdate", "validate"},
		{[]string{"fine-tuning:alpha:graders"}, "zzz", ""},
		{[]string{"responses"}, "zzzzz", ""},
		{[]string{"models"}, "ls", "list"},     // jaro-winkler 0.85
		{[]string{"models"}, "lete", "delete"}, // jaro-winkler 0.72
		// Both are one edit away; the higher jaro-winkler score wins.
		{[]string{"admin:organization:certificates"}, "dactivate", "deactivate"},
		// Exact 7/10 jaro scores that float error would otherwise let through.
		{[]string{"fine-tuning:jobs"}, "status", ""},
		{nil, "hi", ""},
		{nil, "RESPONSES", "responses"},
		{nil, "respones", "responses"},
		{nil, "chat:completion", "chat:completions"},
		{nil, "comp", "completions"},
		// Jaro-winkler alone prefers admin:organization:spend-alerts here.
		{nil, "admin:prganization:roles", "admin:organization:roles"},
		{nil, "version", ""}, // jaro-winkler 0.69
		{nil, "totallybogus", ""},
	}
	for _, tc := range tests {
		t.Run(strings.Join(append(tc.path, tc.provided), " "), func(t *testing.T) {
			want := ""
			if tc.want != "" {
				want = fmt.Sprintf("Did you mean '%s'?", tc.want)
			}
			if got := suggestCommand(findCommand(t, tc.path...).Commands, tc.provided); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// Every command in the real tree keeps its suggestion for a dropped or swapped
// character or for all-caps input, unless the typo is as close to a sibling.
func TestSuggestCommandRealTreeSingleEdits(t *testing.T) {
	var visit func(parent *cli.Command)
	visit = func(parent *cli.Command) {
		for _, command := range parent.Commands {
			name := command.Name
			typos := []string{strings.ToUpper(name)}
			for i := range name {
				typos = append(typos, name[:i]+name[i+1:])
				if i+1 < len(name) {
					typos = append(typos, name[:i]+name[i+1:i+2]+name[i:i+1]+name[i+2:])
				}
			}
		typos:
			for _, typo := range typos {
				for _, sibling := range parent.Commands {
					if sibling != command && withinOneEdit(sibling.Name, strings.ToLower(typo)) {
						continue typos
					}
				}
				want := fmt.Sprintf("Did you mean '%s'?", name)
				if got := suggestCommand(parent.Commands, typo); got != want {
					t.Errorf("%s: suggestCommand(%q) = %q, want %q", parent.Name, typo, got, want)
				}
			}
			visit(command)
		}
	}
	visit(Command)
}
