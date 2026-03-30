package bot

import "testing"

func TestFormatBody(t *testing.T) {
	tests := []struct {
		name      string
		crewID    string
		verbosity string
		text      string
		want      string
	}{
		{
			name:      "standard crew response",
			crewID:    "maren",
			verbosity: "dispatch",
			text:      "Aye, the hull looks sound today.",
			want:      "[maren:dispatch] Aye, the hull looks sound today.",
		},
		{
			name:      "different crew and verbosity",
			crewID:    "crest",
			verbosity: "detail",
			text:      "Signal received from port.",
			want:      "[crest:detail] Signal received from port.",
		},
		{
			name:      "empty text",
			crewID:    "bosun",
			verbosity: "dispatch",
			text:      "",
			want:      "[bosun:dispatch] ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBody(tt.crewID, tt.verbosity, tt.text)
			if got != tt.want {
				t.Errorf("formatBody() = %q, want %q", got, tt.want)
			}
		})
	}
}
