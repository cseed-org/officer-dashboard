package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"
)

func BenchmarkMCPMultipart(b *testing.B) {
	raw := make([]byte, 10<<20)
	content, _ := json.Marshal(base64.StdEncoding.EncodeToString(raw))
	args := map[string]json.RawMessage{"id": json.RawMessage(`"req-0002"`), "filename": json.RawMessage(`"receipt.pdf"`), "mimeType": json.RawMessage(`"application/pdf"`), "contentBase64": content}
	tool := mcpToolByName["attach_receipt"]
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, _, err := mcpMultipart(tool, args)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, r); err != nil {
			b.Fatal(err)
		}
	}
}
