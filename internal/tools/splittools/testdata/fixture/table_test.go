package fixture

import (
	"fmt"
	"strings"
	"testing"
)

// TestTable is the fixture's behaviour: each tool's answer, in order. It
// passes before and after the split.
func TestTable(t *testing.T) {
	var got []string
	for _, tool := range Tools() {
		out, err := tool.Handler(map[string]string{"x": "a", "id": "7", "n": "3", "k": "z"})
		got = append(got, fmt.Sprintf("%s=%s %v", tool.Name, out, err))
	}
	want := "alpha=tool:A <nil>\nbeta=id=7 <nil>\ngamma=#3 <nil>\ndelta=z <nil>\nepsilon=id,k,n,x <nil>"
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("Tools() answers\n%s\nwant\n%s", g, want)
	}
	if n := Names(); n != "[alpha beta gamma delta epsilon]" {
		t.Errorf("Names() = %s", n)
	}
}

// helperFromTest is declared by a test file of the package, so no
// constructor may take its name.
func helperFromTest() {}
