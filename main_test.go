package main

import (
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		environ []string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "simple key=value",
			input: "FOO=bar",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:  "multiple vars",
			input: "A=1\nB=2\nC=3",
			want:  map[string]string{"A": "1", "B": "2", "C": "3"},
		},
		{
			name:  "comments and blank lines",
			input: "# comment\n\nFOO=bar\n  # indented comment\nBAZ=qux",
			want:  map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:  "export prefix",
			input: "export FOO=bar\nexport BAZ=qux",
			want:  map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:  "double-quoted value",
			input: `FOO="hello world"`,
			want:  map[string]string{"FOO": "hello world"},
		},
		{
			name:  "single-quoted value",
			input: `FOO='hello $WORLD'`,
			want:  map[string]string{"FOO": "hello $WORLD"},
		},
		{
			name:  "escape sequences in double quotes",
			input: `FOO="line1\nline2\ttab\\backslash\""`,
			want:  map[string]string{"FOO": "line1\nline2\ttab\\backslash\""},
		},
		{
			name:  "dollar escape in double quotes",
			input: `FOO="price is \$5"`,
			want:  map[string]string{"FOO": "price is $5"},
		},
		{
			name:  "variable substitution $VAR",
			input: "BASE=/opt\nAPP=$BASE/myapp",
			want:  map[string]string{"BASE": "/opt", "APP": "/opt/myapp"},
		},
		{
			name:  "variable substitution ${VAR}",
			input: "BASE=/opt\nAPP=${BASE}/myapp",
			want:  map[string]string{"BASE": "/opt", "APP": "/opt/myapp"},
		},
		{
			name:  "substitution with default ${VAR:-default}",
			input: "FOO=${MISSING:-fallback}",
			want:  map[string]string{"FOO": "fallback"},
		},
		{
			name:    "default used when var is empty",
			input:   "EMPTY=\nFOO=${EMPTY:-fallback}",
			want:    map[string]string{"EMPTY": "", "FOO": "fallback"},
		},
		{
			name:    "default NOT used when var is set",
			input:   "X=hello\nFOO=${X:-fallback}",
			want:    map[string]string{"X": "hello", "FOO": "hello"},
		},
		{
			name:    "inherited env used in substitution",
			input:   "FOO=$INHERITED",
			environ: []string{"INHERITED=from_env"},
			want:    map[string]string{"FOO": "from_env"},
		},
		{
			name:    "file var overrides inherited in substitution",
			input:   "X=from_file\nFOO=$X",
			environ: []string{"X=from_env"},
			want:    map[string]string{"X": "from_file", "FOO": "from_file"},
		},
		{
			name:  "substitution in double-quoted value",
			input: "BASE=/opt\nAPP=\"${BASE}/myapp\"",
			want:  map[string]string{"BASE": "/opt", "APP": "/opt/myapp"},
		},
		{
			name:  "value with equals sign",
			input: "FOO=a=b=c",
			want:  map[string]string{"FOO": "a=b=c"},
		},
		{
			name:  "empty value",
			input: "FOO=",
			want:  map[string]string{"FOO": ""},
		},
		{
			name:  "inline comment on unquoted value",
			input: "FOO=bar # this is a comment",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:    "missing equals",
			input:   "INVALID_LINE",
			wantErr: true,
		},
		{
			name:    "unterminated double quote",
			input:   `FOO="unterminated`,
			wantErr: true,
		},
		{
			name:    "unterminated single quote",
			input:   `FOO='unterminated`,
			wantErr: true,
		},
		{
			name:  "spaces around key",
			input: "  FOO  =bar",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:  "multiple substitutions in one value",
			input: "A=hello\nB=world\nC=$A-$B",
			want:  map[string]string{"A": "hello", "B": "world", "C": "hello-world"},
		},
		{
			name:  "undefined var expands to empty",
			input: "FOO=pre${NOPE}post",
			want:  map[string]string{"FOO": "prepost"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEnvFile(strings.NewReader(tt.input), tt.environ)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d vars, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for k, wantV := range tt.want {
				if gotV, ok := got[k]; !ok {
					t.Errorf("missing key %q", k)
				} else if gotV != wantV {
					t.Errorf("key %q: got %q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}

func TestParseVarRef(t *testing.T) {
	tests := []struct {
		input       string
		wantName    string
		wantDefault string
		wantAdv     int
	}{
		{"$FOO", "FOO", "", 4},
		{"${FOO}", "FOO", "", 6},
		{"${FOO:-bar}", "FOO", "bar", 11},
		{"$FOO_BAR", "FOO_BAR", "", 8},
		{"$123", "123", "", 4},
		{"$", "", "", 0},
		{"${}", "", "", 3},
		{"${:-val}", "", "val", 8},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, def, adv := parseVarRef(tt.input)
			if name != tt.wantName || def != tt.wantDefault || adv != tt.wantAdv {
				t.Errorf("parseVarRef(%q) = (%q, %q, %d), want (%q, %q, %d)",
					tt.input, name, def, adv, tt.wantName, tt.wantDefault, tt.wantAdv)
			}
		})
	}
}
