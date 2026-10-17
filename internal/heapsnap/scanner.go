package heapsnap

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const readBufferSize = 256 << 10

type scanner struct {
	r         *bufio.Reader
	offset    int64
	recording bool
	record    []byte
	text      []byte
}

func newScanner(r io.Reader) *scanner {
	return &scanner{r: bufio.NewReaderSize(r, readBufferSize)}
}

func (s *scanner) readByte() (byte, error) {
	c, err := s.r.ReadByte()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, io.ErrUnexpectedEOF
		}
		return 0, err
	}
	s.offset++
	if s.recording {
		s.record = append(s.record, c)
	}
	return c, nil
}

func (s *scanner) unreadByte() {
	_ = s.r.UnreadByte()
	s.offset--
	if s.recording && len(s.record) > 0 {
		s.record = s.record[:len(s.record)-1]
	}
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

func (s *scanner) next() (byte, error) {
	for {
		c, err := s.readByte()
		if err != nil {
			return 0, err
		}
		if !isSpace(c) {
			return c, nil
		}
	}
}

func (s *scanner) peek() (byte, error) {
	c, err := s.next()
	if err != nil {
		return 0, err
	}
	s.unreadByte()
	return c, nil
}

func (s *scanner) expect(want byte) error {
	c, err := s.next()
	if err != nil {
		return err
	}
	if c != want {
		return fmt.Errorf("offset %d: want %q, got %q", s.offset-1, want, c)
	}
	return nil
}

func (s *scanner) readString(keep bool) (string, error) {
	if err := s.expect('"'); err != nil {
		return "", err
	}
	s.text = s.text[:0]
	escaped := false
	for {
		c, err := s.readByte()
		if err != nil {
			return "", err
		}
		if c == '"' {
			break
		}
		if keep {
			s.text = append(s.text, c)
		}
		if c == '\\' {
			escaped = true
			n, err := s.readByte()
			if err != nil {
				return "", err
			}
			if keep {
				s.text = append(s.text, n)
			}
		}
	}
	if !keep {
		return "", nil
	}
	if !escaped {
		return string(s.text), nil
	}
	quoted := make([]byte, 0, len(s.text)+2)
	quoted = append(quoted, '"')
	quoted = append(quoted, s.text...)
	quoted = append(quoted, '"')
	var out string
	if err := json.Unmarshal(quoted, &out); err != nil {
		return "", fmt.Errorf("offset %d: decode string: %w", s.offset, err)
	}
	return out, nil
}

func (s *scanner) readInt() (int64, error) {
	c, err := s.next()
	if err != nil {
		return 0, err
	}
	negative := false
	if c == '-' {
		negative = true
		if c, err = s.readByte(); err != nil {
			return 0, err
		}
	}
	if c < '0' || c > '9' {
		return 0, fmt.Errorf("offset %d: want an integer, got %q", s.offset-1, c)
	}
	var v int64
	for {
		v = v*10 + int64(c-'0')
		c, err = s.readByte()
		if err != nil {
			return 0, err
		}
		if c < '0' || c > '9' {
			s.unreadByte()
			break
		}
	}
	if negative {
		v = -v
	}
	return v, nil
}

func (s *scanner) intArray(fn func(int64) error) (int, error) {
	if err := s.expect('['); err != nil {
		return 0, err
	}
	c, err := s.peek()
	if err != nil {
		return 0, err
	}
	if c == ']' {
		_, _ = s.next()
		return 0, nil
	}
	count := 0
	for {
		v, err := s.readInt()
		if err != nil {
			return count, err
		}
		if err := fn(v); err != nil {
			return count, err
		}
		count++
		c, err := s.next()
		if err != nil {
			return count, err
		}
		switch c {
		case ',':
		case ']':
			return count, nil
		default:
			return count, fmt.Errorf("offset %d: want ',' or ']', got %q", s.offset-1, c)
		}
	}
}

func (s *scanner) stringArray(fn func(index int) (keep bool), got func(index int, value string)) (int, error) {
	if err := s.expect('['); err != nil {
		return 0, err
	}
	c, err := s.peek()
	if err != nil {
		return 0, err
	}
	if c == ']' {
		_, _ = s.next()
		return 0, nil
	}
	index := 0
	for {
		keep := fn(index)
		v, err := s.readString(keep)
		if err != nil {
			return index, err
		}
		if keep {
			got(index, v)
		}
		index++
		c, err := s.next()
		if err != nil {
			return index, err
		}
		switch c {
		case ',':
		case ']':
			return index, nil
		default:
			return index, fmt.Errorf("offset %d: want ',' or ']', got %q", s.offset-1, c)
		}
	}
}

func (s *scanner) skipValue() error {
	c, err := s.peek()
	if err != nil {
		return err
	}
	switch c {
	case '"':
		_, err := s.readString(false)
		return err
	case '{':
		return s.skipContainer('{', '}', true)
	case '[':
		return s.skipContainer('[', ']', false)
	default:
		return s.skipLiteral()
	}
}

func (s *scanner) skipContainer(open, closing byte, object bool) error {
	if err := s.expect(open); err != nil {
		return err
	}
	c, err := s.peek()
	if err != nil {
		return err
	}
	if c == closing {
		_, _ = s.next()
		return nil
	}
	for {
		if object {
			if _, err := s.readString(false); err != nil {
				return err
			}
			if err := s.expect(':'); err != nil {
				return err
			}
		}
		if err := s.skipValue(); err != nil {
			return err
		}
		c, err := s.next()
		if err != nil {
			return err
		}
		if c == closing {
			return nil
		}
		if c != ',' {
			return fmt.Errorf("offset %d: want ',' or %q, got %q", s.offset-1, closing, c)
		}
	}
}

func (s *scanner) skipLiteral() error {
	read := 0
	for {
		c, err := s.readByte()
		if err != nil {
			return err
		}
		if isSpace(c) || c == ',' || c == ']' || c == '}' {
			s.unreadByte()
			break
		}
		read++
	}
	if read == 0 {
		return fmt.Errorf("offset %d: want a value", s.offset)
	}
	return nil
}

func (s *scanner) captureValue() ([]byte, error) {
	if _, err := s.peek(); err != nil {
		return nil, err
	}
	s.recording = true
	s.record = s.record[:0]
	err := s.skipValue()
	s.recording = false
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), s.record...), nil
}
