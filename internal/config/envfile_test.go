package config_test

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/brettinternet/noon/internal/config"
)

// Prevent silent Compose-divergent values and diagnostics that expose secrets.
func TestEnvFileParse(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"\n  # ignored\n\texport KEY \t= \tvalue\t\r\n", "value"},
		{"KEY=", ""},
		{"KEY=word # comment $ignored", "word"},
		{"KEY=word#literal", "word#literal"},
		{"KEY=word\t#literal", "word\t#literal"},
		{`KEY=literal\path`, `literal\path`},
		{`KEY='$NOT_EXPANDED' # comment`, "$NOT_EXPANDED"},
		{`KEY='it\'s\literal'`, `it's\literal`},
		{`KEY='two\\'`, `two\\`},
		{`KEY="a\nb\rc\td\"e\\f" # comment`, "a\nb\rc\td\"e\\f"},
		{"KEY=first\nKEY=last", "last"},
		{"KEY=\"\"", ""},
		{"KEY=''	# comment", ""},
	} {
		got, err := config.ParseEnvFile([]byte(tc.input))
		if err != nil || !reflect.DeepEqual(got, map[string]string{"KEY": tc.want}) {
			t.Errorf("input %q: got %q, err=%v, want %q", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{
		"KEY=hunter2 $X", `KEY="hunter2$X"`, `KEY="hunter2`, `KEY="hunter2\q"`,
		`KEY='hunter2'junk`, `KEY:hunter2`, `KEY hunter2`, `NOON_X=hunter2`, `1KEY=hunter2`,
		"KEY=hunter2\x00", "KEY=hunter2\xff", "KEY='hunter2\ncontinued'", "KEY=\"hunter2\ncontinued\"",
		"KEY=\vhunter2", "KEY=\u00a0hunter2", "KEY", "export KEY", "KEY.NAME=hunter2", "KEY-NAME=hunter2", `KEY="hunter2\$"`,
		`KEY='hunter2`, "KEY=hunter2\rhidden", `KEY="hunter2\0"`,
	} {
		_, err := config.ParseEnvFile([]byte("# first line\n" + input))
		if err == nil || !strings.Contains(err.Error(), "line 2:") || strings.Contains(err.Error(), "hunter2") {
			t.Errorf("input %q: unsafe or missing error: %v", input, err)
		}
	}
}

func FuzzEnvFileParse(f *testing.F) {
	for _, seed := range []string{"", "KEY=value\n", "export KEY='literal$'", `KEY="a\tb"`, "KEY=bad\x00", `KEY='two\\'`} {
		f.Add(seed)
	}
	name := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	f.Fuzz(func(t *testing.T, input string) {
		values, err := config.ParseEnvFile([]byte(input))
		if err != nil {
			return
		}
		for key, value := range values {
			if !name.MatchString(key) || strings.HasPrefix(key, "NOON_") || strings.ContainsRune(value, 0) {
				t.Fatalf("invalid accepted assignment: %q=%q", key, value)
			}
		}
	})
}
