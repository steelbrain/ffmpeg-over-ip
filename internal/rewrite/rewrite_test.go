package rewrite

import "testing"

func TestApply(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		rewrites [][2]string
		want     []string
		sameRef  bool // if true, expect returned slice is the same reference as input
	}{
		{
			name:     "empty rewrites returns same slice",
			args:     []string{"-c:v", "h264_nvenc", "-i", "input.mp4"},
			rewrites: nil,
			want:     []string{"-c:v", "h264_nvenc", "-i", "input.mp4"},
			sameRef:  true,
		},
		{
			name:     "empty rewrites with explicit empty slice returns same slice",
			args:     []string{"-c:v", "h264_nvenc"},
			rewrites: [][2]string{},
			want:     []string{"-c:v", "h264_nvenc"},
			sameRef:  true,
		},
		{
			name:     "single rewrite replaces codec",
			args:     []string{"-c:v", "h264_nvenc", "-i", "input.mp4"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"-c:v", "h264_qsv", "-i", "input.mp4"},
		},
		{
			name: "multiple rewrites applied in order",
			args: []string{"-c:v", "h264_nvenc", "-preset", "fast"},
			rewrites: [][2]string{
				{"h264_nvenc", "h264_qsv"},
				{"fast", "medium"},
			},
			want: []string{"-c:v", "h264_qsv", "-preset", "medium"},
		},
		{
			name:     "rewrite that does not match leaves args unchanged",
			args:     []string{"-c:v", "libx264", "-i", "input.mp4"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"-c:v", "libx264", "-i", "input.mp4"},
		},
		{
			name:     "rewrite applied to all args not just first match",
			args:     []string{"h264_nvenc", "foo", "h264_nvenc"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"h264_qsv", "foo", "h264_qsv"},
		},
		{
			name:     "rewrite with empty replacement deletes pattern",
			args:     []string{"-nostdin", "-c:v", "libx264"},
			rewrites: [][2]string{{"-nostdin", ""}},
			want:     []string{"", "-c:v", "libx264"},
		},
		{
			name:     "multiple occurrences of pattern in one arg",
			args:     []string{"aa-bb-aa", "cc"},
			rewrites: [][2]string{{"aa", "xx"}},
			want:     []string{"xx-bb-xx", "cc"},
		},
		{
			name: "chained rewrites where first produces text matched by second",
			args: []string{"alpha"},
			rewrites: [][2]string{
				{"alpha", "beta"},
				{"beta", "gamma"},
			},
			want: []string{"gamma"},
		},
		{
			name:     "empty args with non-empty rewrites returns empty result",
			args:     []string{},
			rewrites: [][2]string{{"a", "b"}},
			want:     []string{},
		},
		{
			name:     "rewrite matching part of arg",
			args:     []string{"-c:v h264_nvenc", "-preset fast"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"-c:v h264_qsv", "-preset fast"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Apply(tt.args, tt.rewrites)

			if len(got) != len(tt.want) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tt.want))
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}

			if tt.sameRef {
				if len(tt.args) > 0 && &got[0] != &tt.args[0] {
					t.Error("expected returned slice to be the same reference as input, but got a copy")
				}
			}
		})
	}
}

func TestApplyDoesNotMutateInput(t *testing.T) {
	original := []string{"h264_nvenc", "fast"}
	argsCopy := make([]string, len(original))
	copy(argsCopy, original)

	rewrites := [][2]string{{"h264_nvenc", "h264_qsv"}}
	_ = Apply(original, rewrites)

	for i := range original {
		if original[i] != argsCopy[i] {
			t.Errorf("input was mutated: arg[%d] = %q, want %q", i, original[i], argsCopy[i])
		}
	}
}
