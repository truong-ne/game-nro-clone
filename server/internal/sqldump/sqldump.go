// Package sqldump reads INSERT statements from a MySQL/MariaDB dump file
// (phpMyAdmin or mysqldump style) without needing a running database.
package sqldump

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Value is one cell of a dumped row. Null is true for SQL NULL.
type Value struct {
	Raw  string
	Null bool
}

func (v Value) String() string { return v.Raw }

func (v Value) Int() int64 {
	if v.Null || v.Raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v.Raw), 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(strings.TrimSpace(v.Raw), 64)
		if ferr != nil {
			return 0
		}
		return int64(f)
	}
	return n
}

// Table holds all rows inserted into one table, in dump order.
type Table struct {
	Name    string
	Columns []string
	Rows    [][]Value
}

// Col returns the index of a column (case-insensitive), or -1.
func (t *Table) Col(name string) int {
	for i, c := range t.Columns {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}

// Row is a convenience accessor over a row with column lookup by name.
type Row struct {
	t *Table
	v []Value
}

func (t *Table) Each(fn func(r Row) error) error {
	for _, v := range t.Rows {
		if err := fn(Row{t, v}); err != nil {
			return err
		}
	}
	return nil
}

func (r Row) Get(col string) Value {
	i := r.t.Col(col)
	if i < 0 || i >= len(r.v) {
		return Value{Null: true}
	}
	return r.v[i]
}

func (r Row) Str(col string) string { return r.Get(col).Raw }
func (r Row) Int(col string) int64  { return r.Get(col).Int() }

// ParseFile parses every INSERT INTO statement in the dump.
func ParseFile(path string) (map[string]*Table, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(b))
}

func Parse(src string) (map[string]*Table, error) {
	p := &parser{s: src}
	tables := map[string]*Table{}
	for {
		idx := strings.Index(p.s[p.i:], "INSERT INTO ")
		if idx < 0 {
			return tables, nil
		}
		p.i += idx + len("INSERT INTO ")
		name := p.ident()
		p.ws()
		var cols []string
		if p.peek() == '(' {
			p.i++
			for {
				p.ws()
				cols = append(cols, p.ident())
				p.ws()
				if p.peek() == ',' {
					p.i++
					continue
				}
				if p.peek() == ')' {
					p.i++
					break
				}
				return nil, p.errf("bad column list for %s", name)
			}
		}
		p.ws()
		if !strings.HasPrefix(p.s[p.i:], "VALUES") {
			return nil, p.errf("expected VALUES for %s", name)
		}
		p.i += len("VALUES")
		t := tables[name]
		if t == nil {
			t = &Table{Name: name, Columns: cols}
			tables[name] = t
		}
		for {
			p.ws()
			row, err := p.tuple()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			t.Rows = append(t.Rows, row)
			p.ws()
			c := p.peek()
			p.i++
			if c == ',' {
				continue
			}
			if c == ';' {
				break
			}
			return nil, p.errf("unexpected %q after tuple in %s", c, name)
		}
	}
}

type parser struct {
	s string
	i int
}

func (p *parser) peek() byte {
	if p.i >= len(p.s) {
		return 0
	}
	return p.s[p.i]
}

func (p *parser) ws() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\n', '\r', '\t':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) errf(format string, a ...any) error {
	end := min(p.i+60, len(p.s))
	return fmt.Errorf(format+" near %q", append(a, p.s[p.i:end])...)
}

func (p *parser) ident() string {
	if p.peek() == '`' {
		end := strings.IndexByte(p.s[p.i+1:], '`')
		id := p.s[p.i+1 : p.i+1+end]
		p.i += end + 2
		return id
	}
	start := p.i
	for p.i < len(p.s) && (isAlnum(p.s[p.i]) || p.s[p.i] == '_') {
		p.i++
	}
	return p.s[start:p.i]
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func (p *parser) tuple() ([]Value, error) {
	if p.peek() != '(' {
		return nil, p.errf("expected (")
	}
	p.i++
	var row []Value
	for {
		p.ws()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		row = append(row, v)
		p.ws()
		c := p.peek()
		p.i++
		if c == ',' {
			continue
		}
		if c == ')' {
			return row, nil
		}
		return nil, p.errf("unexpected %q in tuple", c)
	}
}

func (p *parser) value() (Value, error) {
	if p.peek() == '\'' {
		p.i++
		var sb strings.Builder
		for p.i < len(p.s) {
			c := p.s[p.i]
			switch {
			case c == '\\' && p.i+1 < len(p.s):
				p.i += 2
				switch n := p.s[p.i-1]; n {
				case 'n':
					sb.WriteByte('\n')
				case 'r':
					sb.WriteByte('\r')
				case 't':
					sb.WriteByte('\t')
				case '0':
					sb.WriteByte(0)
				case 'Z':
					sb.WriteByte(26)
				default:
					sb.WriteByte(n)
				}
			case c == '\'' && p.i+1 < len(p.s) && p.s[p.i+1] == '\'':
				sb.WriteByte('\'')
				p.i += 2
			case c == '\'':
				p.i++
				return Value{Raw: sb.String()}, nil
			default:
				sb.WriteByte(c)
				p.i++
			}
		}
		return Value{}, p.errf("unterminated string")
	}
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != ',' && p.s[p.i] != ')' {
		p.i++
	}
	raw := strings.TrimSpace(p.s[start:p.i])
	if strings.EqualFold(raw, "NULL") {
		return Value{Null: true}, nil
	}
	return Value{Raw: raw}, nil
}
