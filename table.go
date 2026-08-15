package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// table aligns rows of cells into columns. Unlike a bare tabwriter it keeps
// columns aligned when trailing cells are empty and trims trailing blanks.
type table struct {
	out io.Writer
	buf bytes.Buffer
	tw  *tabwriter.Writer
}

func newTable(out io.Writer) *table {
	t := &table{out: out}
	t.tw = tabwriter.NewWriter(&t.buf, 0, 0, 2, ' ', 0)
	return t
}

func (t *table) row(cells ...string) {
	fmt.Fprintln(t.tw, strings.Join(cells, "\t")+"\t")
}

func (t *table) flush() error {
	if err := t.tw.Flush(); err != nil {
		return err
	}
	var out strings.Builder
	for _, line := range strings.SplitAfter(t.buf.String(), "\n") {
		if line == "" {
			continue
		}
		out.WriteString(strings.TrimRight(line, " \n"))
		out.WriteByte('\n')
	}
	t.buf.Reset()
	_, err := io.WriteString(t.out, out.String())
	return err
}
