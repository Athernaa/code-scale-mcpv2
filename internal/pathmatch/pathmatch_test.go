package pathmatch

import "testing"

func TestMatchSupportsRecursiveDoublestar(t *testing.T) {
	for _, name := range []string{"src/a.ts", "src/components/a.ts", "src/components/deep/a.ts"} {
		if !Match("src/**/*.ts", name) {
			t.Fatalf("expected pattern to match %q", name)
		}
	}
	if Match("src/**/*.ts", "server/a.ts") {
		t.Fatal("pattern matched a path outside src")
	}
}
