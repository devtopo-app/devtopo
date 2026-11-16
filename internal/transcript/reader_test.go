package transcript_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devtopo-app/devtopo/internal/transcript"
)

func TestReadNewIncremental(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	a := `{"type":"user","sessionId":"s","timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":"a"}}` + "\n"
	if err := os.WriteFile(p, []byte(a), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, off, _, err := transcript.ReadNew(p, 0)
	if err != nil || len(recs) != 1 {
		t.Fatalf("first read: %v %d", err, len(recs))
	}
	// Append one complete line plus a partial (no trailing newline) line.
	b := `{"type":"assistant","sessionId":"s","timestamp":"2026-10-21T09:01:00Z","message":{"role":"assistant","content":[]}}` + "\n" + `{"type":"assist`
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(b)
	f.Close()
	recs2, off2, _, err := transcript.ReadNew(p, off)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs2) != 1 {
		t.Fatalf("want 1 complete new record, got %d", len(recs2))
	}
	if off2 <= off {
		t.Fatalf("offset did not advance: %d -> %d", off, off2)
	}
	// The partial line must not decode until completed.
	recs3, _, _, _ := transcript.ReadNew(p, off2)
	if len(recs3) != 0 {
		t.Fatalf("partial line should not decode, got %d", len(recs3))
	}
}

func TestReadNewOffsetPastEOF(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, off, _, err := transcript.ReadNew(p, 9999)
	if err != nil {
		t.Fatalf("past-EOF must not error: %v", err)
	}
	if len(recs) != 0 || off != 9999 {
		t.Fatalf("want no records and unchanged offset, got %d recs off %d", len(recs), off)
	}
}

func TestReadNewCountsDroppedLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	good := `{"type":"user","sessionId":"s","timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":"a"}}`
	bad := `{not valid json`
	if err := os.WriteFile(p, []byte(good+"\n"+bad+"\n"+good+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, _, dropped, err := transcript.ReadNew(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 decoded records, got %d", len(recs))
	}
	if dropped != 1 {
		t.Fatalf("want 1 dropped line, got %d", dropped)
	}
}
