// Package ui implements the interactive checkbox tree.
package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// Choice is one selectable item. Path is its name split into segments; items
// sharing leading segments are nested under a common header.
type Choice struct {
	Label    string
	Path     []string
	Disabled bool
	Note     string
}

func runeLen(s string) int { return len([]rune(s)) }

func pad(s string, w int) string {
	if n := runeLen(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

type node struct {
	name     string
	items    []int
	children []*node
	all      []int // every item index in this subtree
}

// row is one display line: a group header (node set, head true) or an item.
type row struct {
	head   bool
	node   *node // header node, or the node an item row belongs to
	item   int
	name   string
	guides string  // tree-drawing prefix
	anc    []*node // enclosing headers, outermost first
}

func newTree(items []Choice) *node {
	root := &node{}
	for i, c := range items {
		path := c.Path
		if len(path) == 0 {
			path = []string{c.Label}
		}
		cur := root
		for _, seg := range path {
			var next *node
			for _, ch := range cur.children {
				if ch.name == seg {
					next = ch
					break
				}
			}
			if next == nil {
				next = &node{name: seg}
				cur.children = append(cur.children, next)
			}
			cur = next
		}
		cur.items = append(cur.items, i)
	}
	compress(root)
	collect(root)
	return root
}

// compress merges a node that only has one child (and no items) into that child,
// so a shared prefix such as "sec-azure-sub" becomes a single header.
func compress(n *node) {
	for i, c := range n.children {
		for len(c.children) == 1 && len(c.items) == 0 {
			g := c.children[0]
			g.name = c.name + "-" + g.name
			c = g
		}
		n.children[i] = c
		compress(c)
	}
}

func collect(n *node) []int {
	n.all = append([]int(nil), n.items...)
	for _, c := range n.children {
		n.all = append(n.all, collect(c)...)
	}
	return n.all
}

// buildRows flattens the tree into display rows.
func buildRows(items []Choice) []row {
	var rows []row
	var emit func(n *node, guides string, anc []*node, childBase string)
	emit = func(n *node, guides string, anc []*node, childBase string) {
		if len(n.children) == 0 {
			for _, i := range n.items {
				rows = append(rows, row{node: n, item: i, name: n.name, guides: guides, anc: anc})
			}
			return
		}
		rows = append(rows, row{head: true, node: n, item: -1, name: n.name, guides: guides, anc: anc})
		inner := append(append([]*node(nil), anc...), n)
		total := len(n.items) + len(n.children)
		for k := 0; k < total; k++ {
			conn, cont := "\u251c ", "\u2502 "
			if k == total-1 {
				conn, cont = "\u2514 ", "  "
			}
			if k < len(n.items) {
				rows = append(rows, row{node: n, item: n.items[k], name: n.name, guides: childBase + conn, anc: inner})
				continue
			}
			emit(n.children[k-len(n.items)], childBase+conn, inner, childBase+cont)
		}
	}
	for _, c := range newTree(items).children {
		emit(c, "", nil, "  ")
	}
	return rows
}

// helpLine renders key hints with highlighted keys and dim descriptions.
func helpLine() string {
	hint := func(key, desc string) string {
		return "\x1b[1m\x1b[33m" + key + "\x1b[0m \x1b[90m" + desc + "\x1b[0m"
	}
	sep := " \x1b[90m\u2502\x1b[0m "
	return "  " + strings.Join([]string{
		hint("\u2191/\u2193", "move"),
		hint("\u2190/\u2192", "(un)fold"),
		hint("space", "select"),
		hint("a", "all"),
		"\x1b[1m\x1b[32menter\x1b[0m \x1b[32mactivate\x1b[0m",
		"\x1b[1m\x1b[31mq\x1b[0m \x1b[31mquit\x1b[0m",
	}, sep)
}

// Select: arrows/jk move, left/right (or h/l) fold and unfold, space toggles
// (a header toggles everything under it), a toggles all, enter confirms,
// q/esc/ctrl-c cancels. Disabled items are dimmed and never selected. It
// returns the selected item indices.
func Select(title string, items []Choice) ([]int, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("Interactive selection requires a TTY")
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	defer term.Restore(fd, old)
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")

	rows := buildRows(items)
	var enabled []int
	for i, c := range items {
		if !c.Disabled {
			enabled = append(enabled, i)
		}
	}
	enabledIn := func(idx []int) []int {
		var out []int
		for _, i := range idx {
			if !items[i].Disabled {
				out = append(out, i)
			}
		}
		return out
	}
	w := 5
	headRow := map[*node]int{}
	collapsed := map[*node]bool{}
	for i, r := range rows {
		if l := runeLen(r.guides) + runeLen(r.name); l > w {
			w = l
		}
		if r.head {
			headRow[r.node] = i
			collapsed[r.node] = len(enabledIn(r.node.all)) == 0
		}
	}
	navigable := func(r row) bool { return r.head || !items[r.item].Disabled }

	cols, termRows, _ := term.GetSize(int(os.Stdout.Fd()))
	if cols <= 0 {
		cols = 80
	}
	if termRows <= 0 {
		termRows = 24
	}
	pageSize := max(5, termRows-10)
	selected := map[int]bool{}
	cursor, top, drawn := 0, 0, 0 // cursor indexes rows; top is a position in visible()
	visible := func() []int {
		var v []int
	rowLoop:
		for i, r := range rows {
			for _, a := range r.anc {
				if collapsed[a] {
					continue rowLoop
				}
			}
			v = append(v, i)
		}
		return v
	}
	for _, i := range visible() {
		r := rows[i]
		if (r.head && len(enabledIn(r.node.all)) > 0) || (!r.head && !items[r.item].Disabled) {
			cursor = i
			break
		}
	}

	move := func(dir int) {
		v := visible()
		pos := 0
		for p, i := range v {
			if i == cursor {
				pos = p
			}
		}
		for k := 1; k <= len(v); k++ {
			i := v[((pos+dir*k)%len(v)+len(v))%len(v)]
			if navigable(rows[i]) {
				cursor = i
				return
			}
		}
	}

	fold := func() {
		r := rows[cursor]
		switch {
		case r.head && !collapsed[r.node]:
			collapsed[r.node] = true
		case len(r.anc) > 0:
			p := r.anc[len(r.anc)-1]
			if !r.head {
				collapsed[p] = true
			}
			cursor = headRow[p]
		}
	}
	unfold := func() {
		if r := rows[cursor]; r.head {
			collapsed[r.node] = false
		}
	}

	clip := func(s string) string {
		var b strings.Builder
		n, esc := 0, false
		for _, r := range s {
			switch {
			case esc:
				b.WriteRune(r)
				if r == 'm' {
					esc = false
				}
			case r == '\x1b':
				esc = true
				b.WriteRune(r)
			case n < cols-1:
				b.WriteRune(r)
				n++
			}
		}
		return b.String() + "\x1b[0m"
	}

	headMark := func(r row) string {
		em := enabledIn(r.node.all)
		n := 0
		for _, i := range em {
			if selected[i] {
				n++
			}
		}
		switch {
		case len(em) == 0:
			return "\x1b[90m[\u2713]\x1b[0m"
		case n == len(em):
			return "\x1b[32m[x]\x1b[0m"
		case n > 0:
			return "\x1b[33m[-]\x1b[0m"
		}
		return "[ ]"
	}

	render := func() {
		v := visible()
		pos := 0
		for p, i := range v {
			if i == cursor {
				pos = p
			}
		}
		size := min(pageSize, len(v))
		if pos < top {
			top = pos
		}
		if pos >= top+size {
			top = pos - size + 1
		}
		top = max(0, min(top, len(v)-size))

		lines := strings.Split(title, "\n")
		lines = append(lines,
			helpLine(),
			"      \x1b[1m"+pad("Group", w)+"  Status\x1b[0m",
			"      "+strings.Repeat("\u2500", w)+"  "+strings.Repeat("\u2500", 40))
		for p := top; p < top+size && p < len(v); p++ {
			i := v[p]
			r := rows[i]
			ptr := " "
			if i == cursor {
				ptr = "\x1b[36m>\x1b[0m"
			}
			guides := "\x1b[90m" + r.guides + "\x1b[0m"
			label := pad(r.name, w-runeLen(r.guides))
			if r.head {
				arrow := "\u25bc"
				if collapsed[r.node] {
					arrow = "\u25b6"
				}
				name := "\x1b[1m" + label + "\x1b[0m"
				if len(enabledIn(r.node.all)) == 0 {
					name = "\x1b[90m" + label + "\x1b[0m"
				}
				lines = append(lines, ptr+" "+guides+headMark(r)+" "+name+"  \x1b[90m"+arrow+"\x1b[0m")
				continue
			}
			c := items[r.item]
			if c.Disabled {
				lines = append(lines, "  "+guides+"\x1b[90m[\u2713] "+label+"  "+c.Note+"\x1b[0m")
				continue
			}
			mark, note := "[ ]", "\x1b[90m-\x1b[0m"
			if selected[r.item] {
				mark = "\x1b[32m[x]\x1b[0m"
			}
			if c.Note != "" {
				note = "\x1b[31m" + c.Note + "\x1b[0m"
			}
			lines = append(lines, ptr+" "+guides+mark+" "+label+"  "+note)
		}
		foot := fmt.Sprintf("  %d selected", len(selected))
		if len(v) > size {
			foot += fmt.Sprintf("  (%d-%d of %d)", top+1, min(top+size, len(v)), len(v))
		}
		lines = append(lines, foot)
		if drawn > 0 {
			fmt.Printf("\x1b[%dA", drawn)
		}
		for _, l := range lines {
			fmt.Print("\r\x1b[2K" + clip(l) + "\r\n")
		}
		// Erase leftovers from a previous, taller frame (e.g. after folding a group).
		fmt.Print("\x1b[J")
		drawn = len(lines)
	}

	toggle := func(idx []int) {
		all := true
		for _, i := range idx {
			if !selected[i] {
				all = false
			}
		}
		for _, i := range idx {
			if all {
				delete(selected, i)
			} else {
				selected[i] = true
			}
		}
	}

	// setFold folds or unfolds root and every group below it (all groups if
	// root is nil). Groups with nothing left to select stay folded.
	setFold := func(root *node, fold bool) {
		for _, r := range rows {
			if !r.head {
				continue
			}
			under := root == nil || r.node == root
			for _, a := range r.anc {
				if a == root {
					under = true
				}
			}
			if under {
				collapsed[r.node] = fold || len(enabledIn(r.node.all)) == 0
			}
		}
	}

	buf := make([]byte, 16)
	render()
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return nil, err
		}
		key := string(buf[:n])
		switch key {
		case "\x03", "q", "\x1b":
			return nil, nil
		case "\r", "\n":
			var out []int
			for i := range items {
				if selected[i] {
					out = append(out, i)
				}
			}
			return out, nil
		case "\x1b[A", "k":
			move(-1)
		case "\x1b[B", "j":
			move(1)
		case "\x1b[D", "h":
			fold()
		case "\x1b[C", "l":
			unfold()
		case " ":
			if len(rows) > 0 && navigable(rows[cursor]) {
				if r := rows[cursor]; r.head {
					idx := enabledIn(r.node.all)
					toggle(idx)
					if len(idx) > 0 {
						setFold(r.node, !selected[idx[0]])
					}
				} else {
					toggle([]int{r.item})
					// Fold only the item's own group when nothing in it is selected.
					if len(r.anc) > 0 {
						p := r.anc[len(r.anc)-1]
						none := true
						for _, i := range p.all {
							if selected[i] {
								none = false
								break
							}
						}
						if none {
							collapsed[p] = true
							cursor = headRow[p]
						}
					}
				}
			}
		case "a":
			toggle(enabled)
			if len(enabled) > 0 {
				setFold(nil, !selected[enabled[0]])
			}
		}
		render()
	}
}
