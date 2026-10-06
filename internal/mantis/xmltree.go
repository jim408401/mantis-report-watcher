package mantis

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"time"
)

// node is a minimal namespace-agnostic XML tree used to read SOAP responses.
type node struct {
	Name     string
	Attrs    map[string]string
	Text     string
	Children []*node
	Nil      bool
}

func parseXML(b []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	root := &node{Name: "#root"}
	stack := []*node{root}
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{Name: t.Name.Local, Attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.Attrs[a.Name.Local] = a.Value
				if a.Name.Local == "nil" && (a.Value == "true" || a.Value == "1") {
					n.Nil = true
				}
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, n)
			stack = append(stack, n)
			text.Reset()
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			n := stack[len(stack)-1]
			if len(n.Children) == 0 {
				n.Text = text.String()
			}
			text.Reset()
			stack = stack[:len(stack)-1]
		}
	}
	return root, nil
}

// child returns the first direct child with the given local name.
func (n *node) child(name string) *node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// find returns the first descendant (depth first) with the given name.
func (n *node) find(name string) *node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
		if f := c.find(name); f != nil {
			return f
		}
	}
	return nil
}

func (n *node) str(name string) string {
	c := n.child(name)
	if c == nil || c.Nil {
		return ""
	}
	return c.Text
}

func (n *node) int(name string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(n.str(name)))
	return v
}

func (n *node) boolean(name string) bool {
	v := strings.TrimSpace(strings.ToLower(n.str(name)))
	return v == "true" || v == "1"
}

func (n *node) time(name string) time.Time {
	return parseTime(n.str(name))
}

func (n *node) items() []*node {
	if n == nil || n.Nil {
		return nil
	}
	return n.Children
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0)
	}
	return time.Time{}
}
