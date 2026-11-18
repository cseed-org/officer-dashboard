package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

// A JSON base64 string normally contains no escapes. Decode that slice directly;
// only escaped JSON needs a temporary unquoted string. Whitespace is skipped as it
// streams, without a second full-size copy of an attachment's encoding.
func decodeContentBase64(raw json.RawMessage) ([]byte, error) {
	var source []byte
	if bytes.ContainsRune(raw, '\\') {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		source = []byte(s)
	} else {
		source = raw[1 : len(raw)-1]
	}
	source = bytes.TrimRight(source, "= \t\r\n")
	enc := base64.RawStdEncoding
	if bytes.ContainsAny(source, "-_") {
		enc = base64.RawURLEncoding
	}
	decoder := base64.NewDecoder(enc, &base64WhitespaceReader{data: source})
	data := make([]byte, enc.DecodedLen(len(source)))
	n, err := io.ReadFull(decoder, data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	if err == nil {
		var extra [1]byte
		if count, tailErr := decoder.Read(extra[:]); count != 0 || tailErr != io.EOF {
			return nil, errors.New("invalid base64 tail")
		}
	}
	return data[:n], nil
}

type base64WhitespaceReader struct{ data []byte }

func (r *base64WhitespaceReader) Read(p []byte) (int, error) {
	n := 0
	for len(r.data) > 0 && n < len(p) {
		c := r.data[0]
		r.data = r.data[1:]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			continue
		}
		p[n] = c
		n++
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}
