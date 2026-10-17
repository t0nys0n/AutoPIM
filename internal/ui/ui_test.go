package ui

import "testing"

func paths(ps ...[]string) []Choice {
	var out []Choice
	for _, p := range ps {
		out = append(out, Choice{Path: p})
	}
	return out
}

func TestBuildRowsNested(t *testing.T) {
	rows := buildRows(paths(
		[]string{"s", "d", "a"}, []string{"s", "d", "b"},
		[]string{"s", "e", "a"}, []string{"s", "e", "b"},
		[]string{"plain"}))
	want := []struct {
		name, guides string
		head         bool
	}{
		{"s", "", true},
		{"d", "  \u251c ", true},
		{"a", "  \u2502 \u251c ", false},
		{"b", "  \u2502 \u2514 ", false},
		{"e", "  \u2514 ", true},
		{"a", "    \u251c ", false},
		{"b", "    \u2514 ", false},
		{"plain", "", false},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows", len(rows))
	}
	for i, w := range want {
		r := rows[i]
		if r.name != w.name || r.guides != w.guides || r.head != w.head {
			t.Fatalf("row %d: %+v, want %+v", i, r, w)
		}
	}
}

func TestBuildRowsCompressesSingleChains(t *testing.T) {
	rows := buildRows(paths([]string{"x", "y", "a"}, []string{"x", "y", "b"}, []string{"p", "q"}))
	if len(rows) != 4 || rows[0].name != "x-y" || !rows[0].head || rows[3].name != "p-q" || rows[3].head {
		t.Fatalf("%+v", rows)
	}
}

func TestHeaderCoversSubtree(t *testing.T) {
	rows := buildRows(paths([]string{"s", "d", "a"}, []string{"s", "e", "b"}, []string{"s", "e", "c"}))
	if got := len(rows[0].node.all); got != 3 {
		t.Fatalf("top header covers %d items", got)
	}
}
