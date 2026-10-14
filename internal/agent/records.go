package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

// recordReader yields valid JSON lines. The current line is borrowed until next.
// Complete invalid lines count as bad; an unfinished EOF line does not.
// Long lines grow buffer rather than imposing a scanner token limit.
type recordReader struct {
	reader *bufio.Reader
	buffer []byte
	line   []byte
	bad    int
	err    error
	done   bool
}

func newRecordReader(reader io.Reader) *recordReader {
	return &recordReader{reader: bufio.NewReader(reader)}
}

func (r *recordReader) next() bool {
	for !r.done {
		r.buffer = r.buffer[:0]
		for {
			part, err := r.reader.ReadSlice('\n')
			if err == bufio.ErrBufferFull {
				r.buffer = append(r.buffer, part...)
				continue
			}
			if len(r.buffer) > 0 {
				r.buffer = append(r.buffer, part...)
				r.line = r.buffer
			} else {
				r.line = part
			}
			r.done = err != nil
			if err != nil && err != io.EOF {
				r.err = err
				return false
			}
			break
		}
		if len(bytes.TrimSpace(r.line)) == 0 {
			continue
		}
		if json.Valid(r.line) {
			return true
		}
		if !r.done {
			r.bad++
		}
	}
	return false
}

// borrowedJSON avoids copying nested payloads that are decoded within one record.
// It must never be retained past the next call to recordReader.next: pending calls
// must first decode their arguments into owned strings and values.
type borrowedJSON []byte

// UnmarshalJSON borrows data for the current record only.
func (r *borrowedJSON) UnmarshalJSON(data []byte) error {
	*r = data
	return nil
}

// mentions searches the whole stream, not just its cwd, using space bounded by
// the longest root and one read buffer. Overlap preserves cross-buffer matches.
func mentions(reader io.Reader, roots [][]byte) (bool, error) {
	longest := 0
	for _, root := range roots {
		if len(root) == 0 {
			return true, nil
		}
		longest = max(longest, len(root))
	}
	if longest == 0 {
		return false, nil
	}
	const chunkSize = 32 * 1024
	buffer := make([]byte, chunkSize+longest-1)
	kept := 0
	for {
		n, err := reader.Read(buffer[kept : kept+chunkSize])
		if err != nil && err != io.EOF {
			return false, err
		}
		data := buffer[:kept+n]
		for _, root := range roots {
			if bytes.Contains(data, root) {
				return true, nil
			}
		}
		if err == io.EOF {
			return false, nil
		}
		kept = min(len(data), longest-1)
		copy(buffer, data[len(data)-kept:])
	}
}
