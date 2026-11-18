package main

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// A byte-order mark is the only reliable way to identify UTF-16 without
// asking the user to guess an encoding. Preserve it when writing the file.
type notebookEncoding uint8

const (
	noteUTF8 notebookEncoding = iota
	noteUTF8BOM
	noteUTF16LE
	noteUTF16BE
)

func decodeNotebookText(data []byte) (string, notebookEncoding, error) {
	enc := noteUTF8
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		enc = noteUTF16LE
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		enc = noteUTF16BE
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		enc = noteUTF8BOM
		data = data[3:]
	}
	if enc == noteUTF16LE || enc == noteUTF16BE {
		if len(data)%2 != 0 {
			return "", enc, errNotebookBinary
		}
		units := make([]uint16, (len(data)-2)/2)
		for i := range units {
			units[i] = enc.order().Uint16(data[2+i*2:])
		}
		text := string(utf16.Decode(units))
		if strings.ContainsRune(text, 0) || !bytes.Equal(enc.encode(text), data) {
			return "", enc, errNotebookBinary
		}
		return text, enc, nil
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", enc, errNotebookBinary
	}
	return string(data), enc, nil
}
func (e notebookEncoding) order() binary.ByteOrder {
	if e == noteUTF16BE {
		return binary.BigEndian
	}
	return binary.LittleEndian
}
func (e notebookEncoding) encode(text string) []byte {
	if e == noteUTF8 {
		return []byte(text)
	}
	if e == noteUTF8BOM {
		return []byte("\ufeff" + text)
	}
	units := utf16.Encode([]rune(text))
	b := make([]byte, 2+len(units)*2)
	order := e.order()
	order.PutUint16(b, 0xfeff)
	for i, u := range units {
		order.PutUint16(b[2+i*2:], u)
	}
	return b
}
