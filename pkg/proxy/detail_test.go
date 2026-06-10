package proxy

import (
	"testing"
)

func TestDiffBodiesIdentical(t *testing.T) {
	lines := DiffBodies("hello\nworld", "hello\nworld")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	for _, line := range lines {
		if line.Differs {
			t.Errorf("expected no differing lines, got %+v", line)
		}
	}
}

func TestDiffBodiesChangedLine(t *testing.T) {
	lines := DiffBodies("a\nb\nc", "a\nX\nc")

	var differing int
	for _, line := range lines {
		if line.Differs {
			differing++
		}
	}
	if differing != 2 {
		t.Errorf("expected 2 differing lines (removal of b, addition of X), got %d in %+v", differing, lines)
	}

	if lines[0].Differs || lines[0].Main != "a" {
		t.Errorf("expected first line to be an unchanged 'a', got %+v", lines[0])
	}
	last := lines[len(lines)-1]
	if last.Differs || last.Main != "c" {
		t.Errorf("expected last line to be an unchanged 'c', got %+v", last)
	}
}

func TestDiffBodiesJSONReformatted(t *testing.T) {
	// Semantically equal JSON with different formatting should produce no
	// differing lines because bodies are pretty-printed before diffing.
	lines := DiffBodies(`{"a":1,"b":2}`, "{\n  \"a\": 1,\n  \"b\": 2\n}")
	for _, line := range lines {
		if line.Differs {
			t.Errorf("expected reformatted JSON to diff clean, got differing line %+v", line)
		}
	}
}

func TestDiffBodiesJSONValueChange(t *testing.T) {
	lines := DiffBodies(`{"a":1}`, `{"a":2}`)

	var differing int
	for _, line := range lines {
		if line.Differs {
			differing++
		}
	}
	if differing == 0 {
		t.Error("expected differing lines for changed JSON value")
	}
}

func TestDiffBodiesAddedLines(t *testing.T) {
	lines := DiffBodies("a", "a\nb\nc")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %+v", len(lines), lines)
	}
	if lines[0].Differs {
		t.Errorf("expected 'a' to be unchanged, got %+v", lines[0])
	}
	if !lines[1].Differs || lines[1].New != "b" || lines[1].Main != "" {
		t.Errorf("expected added line 'b', got %+v", lines[1])
	}
}
