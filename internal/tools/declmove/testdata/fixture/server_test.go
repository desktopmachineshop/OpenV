package fixture

import "testing"

func TestHandle(t *testing.T) {
	s := &Server{name: "a", mode: ModeLoud}
	if got, _ := s.Handle("<b>"); got != "&LT;B&GT;" {
		t.Fatal(got)
	}
	if helper() == "" || len(Sorted()) != 2 {
		t.Fatal("registry")
	}
}
