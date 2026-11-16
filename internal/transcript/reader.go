package transcript

import (
	"bufio"
	"bytes"
	"io"
	"os"
)

// ReadNew opens path read-only, seeks to fromOffset, and returns one Record per
// complete (newline-terminated) line from there to the end of the file.
// newOffset is the byte position after the last complete line; a trailing line
// with no newline is neither decoded nor consumed, so it is re-read once it is
// completed. An offset at or past EOF returns no records and the offset
// unchanged (a rotated or truncated file is not an error). A complete line that
// fails to decode is counted in dropped and skipped; it does not abort the read
// or drop later lines, but the count lets a caller surface an unparseable line.
func ReadNew(path string, fromOffset int64) (records []Record, newOffset int64, dropped int, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fromOffset, 0, err
	}
	if fromOffset >= fi.Size() {
		return nil, fromOffset, 0, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fromOffset, 0, err
	}
	defer f.Close()
	if _, err := f.Seek(fromOffset, io.SeekStart); err != nil {
		return nil, fromOffset, 0, err
	}

	off := fromOffset
	br := bufio.NewReader(f)
	for {
		line, rerr := br.ReadBytes('\n')
		if rerr == nil {
			// A complete line ending in '\n'. Consume it, decode it.
			off += int64(len(line))
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(bytes.TrimSpace(trimmed)) == 0 {
				continue // blank line, not a dropped record
			}
			if rec, derr := Decode(trimmed); derr == nil {
				records = append(records, rec)
			} else {
				dropped++
			}
			continue
		}
		if rerr == io.EOF {
			// line holds any trailing bytes with no newline: a partial line.
			// Leave it unconsumed so it is re-read when completed.
			break
		}
		return records, off, dropped, rerr
	}
	return records, off, dropped, nil
}
