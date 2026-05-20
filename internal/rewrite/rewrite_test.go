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
		// --- Identity / no-op ---
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
			name:     "empty args with non-empty rewrites returns empty result",
			args:     []string{},
			rewrites: [][2]string{{"a", "b"}},
			want:     []string{},
		},
		{
			name:     "nil args with non-empty rewrites returns empty result",
			args:     nil,
			rewrites: [][2]string{{"a", "b"}},
			want:     []string{},
		},
		{
			name:     "rewrite that does not match leaves args unchanged",
			args:     []string{"-c:v", "libx264", "-i", "input.mp4"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"-c:v", "libx264", "-i", "input.mp4"},
		},
		{
			name:     "empty find token is skipped",
			args:     []string{"-c:v", "libx264"},
			rewrites: [][2]string{{"", "anything"}, {"   ", "anything"}},
			want:     []string{"-c:v", "libx264"},
		},

		// --- Single-token rewrites (whole-arg match) ---
		{
			name:     "single rewrite replaces matching element",
			args:     []string{"-c:v", "h264_nvenc", "-i", "input.mp4"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"-c:v", "h264_qsv", "-i", "input.mp4"},
		},
		{
			name:     "single rewrite applied to every matching element",
			args:     []string{"h264_nvenc", "foo", "h264_nvenc"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"h264_qsv", "foo", "h264_qsv"},
		},
		{
			name:     "single-token find requires whole-arg match (no substring rewrite)",
			args:     []string{"h264_nvenc_extra", "h264_nvenc"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"h264_nvenc_extra", "h264_qsv"},
		},
		{
			name:     "single-token rewrite with empty replacement removes element",
			args:     []string{"-nostdin", "-c:v", "libx264"},
			rewrites: [][2]string{{"-nostdin", ""}},
			want:     []string{"-c:v", "libx264"},
		},
		{
			name:     "whitespace-only replacement is equivalent to empty (removes element)",
			args:     []string{"-nostdin", "-c:v", "libx264"},
			rewrites: [][2]string{{"-nostdin", "   "}},
			want:     []string{"-c:v", "libx264"},
		},
		{
			name: "multiple single-token rewrites applied in order",
			args: []string{"-c:v", "h264_nvenc", "-preset", "fast"},
			rewrites: [][2]string{
				{"h264_nvenc", "h264_qsv"},
				{"fast", "medium"},
			},
			want: []string{"-c:v", "h264_qsv", "-preset", "medium"},
		},
		{
			name: "chained rewrites where first produces token matched by second",
			args: []string{"alpha"},
			rewrites: [][2]string{
				{"alpha", "beta"},
				{"beta", "gamma"},
			},
			want: []string{"gamma"},
		},
		{
			name: "chained rewrites across token-count changes (2 to 2 then 2 to 1)",
			args: []string{"a", "b"},
			rewrites: [][2]string{
				{"a b", "c d"},
				{"c d", "e"},
			},
			want: []string{"e"},
		},

		// --- Multi-token rewrites (consecutive argv run match) ---
		{
			name:     "multi-token 2 to 2 swap",
			args:     []string{"-hwaccel", "qsv", "-i", "in.mp4"},
			rewrites: [][2]string{{"-hwaccel qsv", "-hwaccel cuda"}},
			want:     []string{"-hwaccel", "cuda", "-i", "in.mp4"},
		},
		{
			name:     "multi-token 2 to 4 expansion",
			args:     []string{"-hwaccel", "qsv", "-i", "in.mp4"},
			rewrites: [][2]string{{"-hwaccel qsv", "-hwaccel cuda -hwaccel_output_format cuda"}},
			want:     []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda", "-i", "in.mp4"},
		},
		{
			name:     "multi-token 4 to 2 contraction",
			args:     []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda", "-i", "in.mp4"},
			rewrites: [][2]string{{"-hwaccel cuda -hwaccel_output_format cuda", "-hwaccel qsv"}},
			want:     []string{"-hwaccel", "qsv", "-i", "in.mp4"},
		},
		{
			name:     "multi-token with empty replacement deletes run",
			args:     []string{"-preset", "veryfast", "-i", "in.mp4"},
			rewrites: [][2]string{{"-preset veryfast", ""}},
			want:     []string{"-i", "in.mp4"},
		},
		{
			name:     "multi-token applied to every occurrence",
			args:     []string{"-c:v", "h264_qsv", "-c:a", "aac", "-c:v", "h264_qsv"},
			rewrites: [][2]string{{"-c:v h264_qsv", "-c:v h264_nvenc"}},
			want:     []string{"-c:v", "h264_nvenc", "-c:a", "aac", "-c:v", "h264_nvenc"},
		},
		{
			name:     "multi-token does not match when only the first token aligns",
			args:     []string{"-hwaccel", "vaapi", "-i", "in.mp4"},
			rewrites: [][2]string{{"-hwaccel qsv", "-hwaccel cuda"}},
			want:     []string{"-hwaccel", "vaapi", "-i", "in.mp4"},
		},
		{
			name:     "multi-token does not match when args slice is shorter than find",
			args:     []string{"-hwaccel"},
			rewrites: [][2]string{{"-hwaccel qsv", "-hwaccel cuda"}},
			want:     []string{"-hwaccel"},
		},
		{
			name:     "multi-token scan resumes past replacement, no self-loop",
			args:     []string{"a", "b"},
			rewrites: [][2]string{{"a b", "a b c"}},
			want:     []string{"a", "b", "c"},
		},
		{
			name:     "multi-token matches are non-overlapping",
			args:     []string{"a", "a", "a"},
			rewrites: [][2]string{{"a a", "x"}},
			want:     []string{"x", "a"},
		},
		{
			name:     "multi-token matches back-to-back occurrences",
			args:     []string{"a", "b", "a", "b"},
			rewrites: [][2]string{{"a b", "x"}},
			want:     []string{"x", "x"},
		},
		{
			name:     "whitespace in find is collapsed via strings.Fields",
			args:     []string{"-hwaccel", "qsv"},
			rewrites: [][2]string{{"  -hwaccel    qsv  ", "-hwaccel cuda"}},
			want:     []string{"-hwaccel", "cuda"},
		},

		// --- Mixed single- and multi-token rewrites ---
		{
			name: "mixed single-token and multi-token rewrites applied in order",
			args: []string{"-c:v", "h264_qsv", "-preset", "veryfast"},
			rewrites: [][2]string{
				{"-c:v h264_qsv", "-c:v h264_nvenc"},
				{"veryfast", "p1"},
			},
			want: []string{"-c:v", "h264_nvenc", "-preset", "p1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Apply(tt.args, tt.rewrites)

			if len(got) != len(tt.want) {
				t.Fatalf("length mismatch: got %d (%v), want %d (%v)", len(got), got, len(tt.want), tt.want)
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
