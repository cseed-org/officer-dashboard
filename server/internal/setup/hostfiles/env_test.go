package hostfiles

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var templates = []string{".env.example", "dashboard.env.example", "backup.env.example"}

func templatePath(name string) string { return filepath.Join("..", "..", "..", "deploy", name) }

func TestRoundTripKeepsBytes(t *testing.T) {
	cases := map[string][]byte{
		"crlf":            []byte("# c\r\nA=1\r\n\r\n#B=2\r\n"),
		"no final eol":    []byte("A=1\n# tail"),
		"mixed endings":   []byte("A=1\r\nB=2\nC=3"),
		"empty":           {},
		"only newline":    []byte("\n"),
		"odd spacing":     []byte("  export   A = 'x y' # note\n\tB=2\n"),
		"no equals lines": []byte("just words\n=\n"),
	}
	for _, name := range templates {
		raw, err := os.ReadFile(templatePath(name))
		if err != nil {
			t.Fatal(err)
		}
		cases[name] = raw
		cases[name+" as crlf"] = bytes.ReplaceAll(raw, []byte("\n"), []byte("\r\n"))
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			f := ParseEnv(raw)
			if got := f.Bytes(); !bytes.Equal(got, raw) {
				t.Fatalf("round trip changed the file:\n got %q\nwant %q", got, raw)
			}
		})
	}
}

func TestGetReadsValuesLikeCompose(t *testing.T) {
	// Expected values are what `docker compose config` printed for the same lines
	// (Docker Compose v2, checked by hand when this was written).
	f := ParseEnv([]byte(strings.Join([]string{
		"PLAIN=abc",
		"SPACEIN=Jane Doe",
		"HASHNOSPACE=a#b",
		"HASHSPACE=a #b",
		"SQ='a$b #c d'",
		`SQBS='a\b'`,
		`DQ="a\"b"`,
		"TRAIL=abc   ",
		"LEAD=  x",
		"EMPTY=",
		"SQEMPTY=''",
		"export EXP=1",
		"#COMMENTED=no",
		"",
	}, "\n")))
	want := map[string]string{
		"PLAIN": "abc", "SPACEIN": "Jane Doe", "HASHNOSPACE": "a#b", "HASHSPACE": "a",
		"SQ": "a$b #c d", "SQBS": `a\b`, "DQ": `a"b`, "TRAIL": "abc", "LEAD": "x",
		"EMPTY": "", "SQEMPTY": "", "EXP": "1",
	}
	for k, v := range want {
		got, ok := f.Get(k)
		if !ok || got != v {
			t.Errorf("Get(%s) = %q, %v; want %q", k, got, ok, v)
		}
	}
	if f.Has("COMMENTED") {
		t.Error("a commented-out example is not a value")
	}
	if _, ok := f.Get("MISSING"); ok {
		t.Error("Get of a missing key reported a value")
	}
}

func TestSetEditsInPlace(t *testing.T) {
	cases := []struct {
		name, in, key, value, want string
	}{
		{"active line keeps its place and comment lines", "# top\nA=1\nB=2\n", "A", "9", "# top\nA=9\nB=2\n"},
		{"commented example is uncommented in place", "A=1\n#TUNNEL_NET=10.250.89\nB=2\n", "TUNNEL_NET", "10.250.90", "A=1\nTUNNEL_NET=10.250.90\nB=2\n"},
		{"active line wins over a commented one", "#A=old\nA=1\n", "A", "2", "#A=old\nA=2\n"},
		{"prose that mentions the key is not an example", "# A=see docs\n", "A", "1", "# A=see docs\nA=1\n"},
		{"missing key is appended", "A=1\n", "B", "2", "A=1\nB=2\n"},
		{"append keeps CRLF", "A=1\r\n", "B", "2", "A=1\r\nB=2\r\n"},
		{"append after a missing final newline", "A=1", "B", "2", "A=1\nB=2\n"},
		{"append to an empty file", "", "A", "1", "A=1\n"},
		{"export prefix is kept", "export A=1\n", "A", "2", "export A=2\n"},
		{"value that needs quotes", "A=1\n", "A", "Jane $HOME", "A='Jane $HOME'\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := ParseEnv([]byte(c.in))
			if err := f.Set(c.key, c.value); err != nil {
				t.Fatal(err)
			}
			if got := string(f.Bytes()); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			if got, _ := f.Get(c.key); got != c.value {
				t.Fatalf("Get after Set = %q, want %q", got, c.value)
			}
		})
	}
}

func TestSetNeverDuplicates(t *testing.T) {
	f := ParseEnv([]byte("#A=x\n"))
	for _, v := range []string{"1", "2", "3"} {
		if err := f.Set("A", v); err != nil {
			t.Fatal(err)
		}
	}
	if got := string(f.Bytes()); got != "A=3\n" {
		t.Fatalf("got %q", got)
	}
	dup := ParseEnv([]byte("A=1\nA=2\n"))
	if err := dup.Set("A", "3"); err == nil {
		t.Fatal("Set on a key that is already duplicated should refuse")
	}
	if err := f.Set("not a key", "1"); err == nil {
		t.Fatal("Set accepted an invalid key")
	}
}

func TestUnsetCommentsTheLine(t *testing.T) {
	f := ParseEnv([]byte("A=1\r\nB=2\r\n"))
	f.Unset("A")
	f.Unset("MISSING")
	if got := string(f.Bytes()); got != "#A=1\r\nB=2\r\n" {
		t.Fatalf("got %q", got)
	}
	if f.Has("A") {
		t.Fatal("A still active")
	}
	if err := f.Set("A", "1"); err != nil || string(f.Bytes()) != "A=1\r\nB=2\r\n" {
		t.Fatalf("Set after Unset should uncomment in place: %q %v", f.Bytes(), err)
	}
}

// Each case is a value as Compose's env_file reader and scripts/update's dotenv would see
// it; FormatValue must write it so both read it back unchanged, or refuse.
func TestFormatValue(t *testing.T) {
	cases := []struct {
		value, want string
		refuse      bool
	}{
		{value: "abc", want: "abc"},
		{value: "", want: ""},
		{value: "https://officers.example.org", want: "https://officers.example.org"},
		{value: "Jane Doe", want: "Jane Doe"},       // inner blanks survive unquoted
		{value: "O'Brien", want: "O'Brien"},         // a quote inside is literal unquoted
		{value: "a#b", want: "'a#b'"},               // Compose keeps it, but quote any #
		{value: "a #b", want: "'a #b'"},             // unquoted, " #" starts a comment
		{value: "#x", want: "'#x'"},                 // leading # quoted
		{value: "a$HOME", want: "'a$HOME'"},         // unquoted, Compose expands $
		{value: `a\b`, want: `'a\b'`},               // systemd unescapes \ unquoted
		{value: " x", want: "' x'"},                 // leading blank trimmed unquoted
		{value: "x ", want: "'x '"},                 // trailing blank trimmed unquoted
		{value: `"x"`, want: `'"x"'`},               // leading double quote
		{value: "'x", refuse: true},                 // needs quotes and contains '
		{value: "it's $1", refuse: true},            // needs quotes and contains '
		{value: `ab\`, refuse: true},                // '\'' closes nothing in Compose
		{value: "a\nb", refuse: true},               // newline
		{value: "a\rb", refuse: true},               // carriage return
		{value: "a\tb", refuse: true},               // tab: Compose and dotenv disagree
		{value: "a\x00b", refuse: true},             // NUL
		{value: "a\x7fb", refuse: true},             // DEL
		{value: "caf\xc3\xa9", want: "caf\xc3\xa9"}, // UTF-8 is fine
		{value: "bad\xffutf8", refuse: true},        // invalid UTF-8
	}
	for _, c := range cases {
		got, err := FormatValue(c.value)
		if c.refuse {
			if err == nil {
				t.Errorf("FormatValue(%q) = %q, want an error", c.value, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("FormatValue(%q) = %q, %v; want %q", c.value, got, err, c.want)
			continue
		}
		f := ParseEnv([]byte("K=" + got + "\n"))
		if back, _ := f.Get("K"); back != c.value {
			t.Errorf("value %q written as %q reads back as %q", c.value, got, back)
		}
	}
}

func TestErrorsNeverCarryTheValue(t *testing.T) {
	const sentinel = "SENTINEL-value-'with\nnewline"
	_, err := FormatValue(sentinel)
	if err == nil || strings.Contains(err.Error(), "SENTINEL") {
		t.Fatalf("error %v", err)
	}
	f := ParseEnv(nil)
	if err := f.Set("K", sentinel); err == nil || strings.Contains(err.Error(), "SENTINEL") {
		t.Fatalf("error %v", err)
	}
}
