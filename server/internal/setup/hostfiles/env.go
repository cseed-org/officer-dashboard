package hostfiles

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// EnvFile is an env file kept line by line, so that writing back an untouched file
// reproduces its bytes exactly: comments, blank lines, order, unknown keys, CRLF line
// endings and a missing final newline all survive.
type EnvFile struct {
	lines []envLine
}

type envLine struct {
	text string // the line without its ending
	eol  string // "\n", "\r\n" or "" (last line without a newline)
}

// ParseEnv splits data into lines. It never fails: a line it cannot read as KEY=VALUE
// is kept as it is and ignored, which is also what Docker Compose does with comments.
func ParseEnv(data []byte) *EnvFile {
	f := &EnvFile{}
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			f.lines = append(f.lines, envLine{text: string(data)})
			break
		}
		text, eol := string(data[:i]), "\n"
		if strings.HasSuffix(text, "\r") {
			text, eol = text[:len(text)-1], "\r\n"
		}
		f.lines = append(f.lines, envLine{text: text, eol: eol})
		data = data[i+1:]
	}
	return f
}

// Bytes is the file's content.
func (f *EnvFile) Bytes() []byte {
	var b bytes.Buffer
	for _, l := range f.lines {
		b.WriteString(l.text)
		b.WriteString(l.eol)
	}
	return b.Bytes()
}

// Get returns the value of the last active KEY= line, read the way Docker Compose reads
// an env_file: surrounding blanks trimmed, " #" starting a comment in an unquoted value,
// single quotes literal, double quotes with backslash escapes. Unquoted $VAR references
// are returned as written; Compose would expand them, which is why FormatValue quotes $.
func (f *EnvFile) Get(key string) (string, bool) {
	i := f.active(key)
	if i < 0 {
		return "", false
	}
	_, raw, _ := splitAssignment(f.lines[i].text)
	return decodeValue(raw), true
}

// Has reports whether key has an active (not commented-out) line.
func (f *EnvFile) Has(key string) bool { return f.active(key) >= 0 }

// active is the index of the last active line for key, or -1.
func (f *EnvFile) active(key string) int {
	found := -1
	for i, l := range f.lines {
		if k, _, ok := splitAssignment(l.text); ok && k == key {
			found = i
		}
	}
	return found
}

// splitAssignment reads "[export ]KEY=raw" with optional blanks around the key. Comment
// lines and lines without a valid key are not assignments.
func splitAssignment(text string) (key, raw string, ok bool) {
	s := strings.TrimLeft(text, " \t")
	if strings.HasPrefix(s, "#") {
		return "", "", false
	}
	if rest, found := strings.CutPrefix(s, "export"); found && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		s = strings.TrimLeft(rest, " \t")
	}
	k, v, found := strings.Cut(s, "=")
	if !found {
		return "", "", false
	}
	k = strings.TrimRight(k, " \t")
	if !validKey(k) {
		return "", "", false
	}
	return k, v, true
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i, c := range k {
		switch {
		case c == '_', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func decodeValue(raw string) string {
	v := strings.Trim(raw, " \t")
	if v == "" {
		return ""
	}
	switch v[0] {
	case '\'':
		return quoted(v, '\'')
	case '"':
		return quoted(v, '"')
	}
	v, _, _ = strings.Cut(v, " #")
	return strings.TrimRight(v, " \t")
}

// quoted reads a quoted value as Compose does: a backslash before the quote character
// escapes it; other backslashes are kept, except that double quotes also expand \n, \t,
// \\ and \". Text after the closing quote is ignored.
func quoted(v string, q byte) string {
	var b strings.Builder
	for i := 1; i < len(v); i++ {
		c := v[i]
		if c == '\\' && i+1 < len(v) {
			n := v[i+1]
			if n == q {
				b.WriteByte(n)
				i++
				continue
			}
			if q == '"' {
				switch n {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case '\\':
					b.WriteByte('\\')
				default:
					b.WriteByte('\\')
					b.WriteByte(n)
				}
				i++
				continue
			}
		}
		if c == q {
			break
		}
		b.WriteByte(c)
	}
	return b.String()
}

// Set gives key the value. It changes the value on the key's active line; if the key
// exists only as a commented-out example ("#KEY=..." at the start of a line) it
// uncomments the first such line in place; otherwise it appends a line at the end. A key
// is never written twice: if the file already has two active lines for it, Set refuses.
func (f *EnvFile) Set(key, value string) error {
	if !validKey(key) {
		return fmt.Errorf("%q is not an env key", key)
	}
	formatted, err := FormatValue(value)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	active := 0
	for _, l := range f.lines {
		if k, _, ok := splitAssignment(l.text); ok && k == key {
			active++
		}
	}
	if active > 1 {
		return fmt.Errorf("%s is set on %d lines; remove the extra lines by hand first", key, active)
	}
	if i := f.active(key); i >= 0 {
		prefix, _, _ := strings.Cut(f.lines[i].text, "=")
		f.lines[i].text = prefix + "=" + formatted
		return nil
	}
	for i, l := range f.lines {
		if strings.HasPrefix(l.text, "#"+key+"=") {
			f.lines[i].text = key + "=" + formatted
			return nil
		}
	}
	eol := f.lineEnding()
	if n := len(f.lines); n > 0 && f.lines[n-1].eol == "" {
		f.lines[n-1].eol = eol
	}
	f.lines = append(f.lines, envLine{text: key + "=" + formatted, eol: eol})
	return nil
}

// Unset turns the key's active lines back into commented-out examples.
func (f *EnvFile) Unset(key string) {
	for i, l := range f.lines {
		if k, _, ok := splitAssignment(l.text); ok && k == key {
			f.lines[i].text = "#" + strings.TrimLeft(l.text, " \t")
		}
	}
}

// lineEnding is the ending of the file's first line, so appended lines match it.
func (f *EnvFile) lineEnding() string {
	for _, l := range f.lines {
		if l.eol != "" {
			return l.eol
		}
	}
	return "\n"
}

// FormatValue writes value so that Docker Compose (env_file and --env-file), systemd's
// EnvironmentFile and scripts/update's dotenv all read it back unchanged, or refuses.
//
// Measured with `docker compose config` (Compose v2): an unquoted value is trimmed, cut
// at " #", and has $VAR expanded; a single-quoted value is literal except that \' is an
// escaped quote, so it can neither contain ' nor end in \. systemd also unescapes \ in
// unquoted values. So: plain when nothing needs protecting, single quotes when they
// suffice, an error otherwise. Control characters, tabs and newlines are always refused:
// one line per key is the only shape all three readers agree on.
//
// The error never contains the value.
func FormatValue(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("value is not valid UTF-8")
	}
	for _, c := range value {
		if c < 0x20 || c == 0x7f {
			return "", errors.New("value contains a newline, tab or other control character, which an env file cannot hold safely")
		}
	}
	if value == "" {
		return "", nil
	}
	needsQuotes := value[0] == ' ' || value[len(value)-1] == ' ' || value[0] == '\'' || value[0] == '"' ||
		strings.ContainsAny(value, `#$\`)
	if !needsQuotes {
		return value, nil
	}
	if strings.Contains(value, "'") || strings.HasSuffix(value, `\`) {
		return "", errors.New("value needs quoting and contains a single quote or ends in a backslash, which Compose cannot read back; store it in a secret file instead")
	}
	return "'" + value + "'", nil
}
