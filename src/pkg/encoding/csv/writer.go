// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package csv

import (
	"bufio"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A Writer writes records to a CSV encoded file.
//
// As returned by NewWriter, a Writer writes records terminated by a
// newline and uses ',' as the field delimiter.  The exported fields can be
// changed to customize the details before the first call to Write or WriteAll.
//
// Comma is the field delimiter.
//
// If UseCRLF is true, the Writer ends each record with \r\n instead of \n.
type Writer struct {
	Comma   rune // Field delimiter (set to ',' by NewWriter)
	UseCRLF bool // True to use \r\n as the line terminator
	w       *bufio.Writer
}

// NewWriter returns a new Writer that writes to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{
		Comma: ',',
		w:     bufio.NewWriter(w),
	}
}

// Writer writes a single CSV record to w along with any necessary quoting.
// A record is a slice of strings with each string being one field.
func (w *Writer) Write(record []string) (err error) {
	for n, field := range record {
		if n > 0 {
			if _, err = w.w.WriteRune(w.Comma); err != nil {
				return
			}
		}

		// If we don't have to have a quoted field then just
		// write out the field and continue to the next field.
		if !w.fieldNeedsQuotes(field) {
			if _, err = w.w.WriteString(field); err != nil {
				return
			}
			continue
		}
		if err = w.w.WriteByte('"'); err != nil {
			return
		}

		for _, r1 := range field {
			switch r1 {
			case '"':
				_, err = w.w.WriteString(`""`)
			case '\r':
				if !w.UseCRLF {
					err = w.w.WriteByte('\r')
				}
			case '\n':
				if w.UseCRLF {
					_, err = w.w.WriteString("\r\n")
				} else {
					err = w.w.WriteByte('\n')
				}
			default:
				_, err = w.w.WriteRune(r1)
			}
			if err != nil {
				return
			}
		}

		if err = w.w.WriteByte('"'); err != nil {
			return
		}
	}
	if w.UseCRLF {
		_, err = w.w.WriteString("\r\n")
	} else {
		err = w.w.WriteByte('\n')
	}
	return
}

// Flush writes any buffered data to the underlying io.Writer.
func (w *Writer) Flush() {
	w.w.Flush()
}

// WriteAll writes multiple CSV records to w using Write and then calls Flush.
func (w *Writer) WriteAll(records [][]string) (err error) {
	for _, record := range records {
		err = w.Write(record)
		if err != nil {
			return err
		}
	}
	return w.w.Flush()
}

// fieldNeedsQuotes returns true if our field must be enclosed in quotes.
// Empty fields, files with a Comma, fields with a quote or newline, and
// fields which start with a space must be enclosed in quotes.
func (w *Writer) fieldNeedsQuotes(field string) bool {
	if len(field) == 0 || strings.IndexRune(field, w.Comma) >= 0 || strings.IndexAny(field, "\"\r\n") >= 0 {
		return true
	}

	r1, _ := utf8.DecodeRuneInString(field)
	return unicode.IsSpace(r1)
}
