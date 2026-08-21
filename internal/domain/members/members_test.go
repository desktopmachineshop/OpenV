package members

import "testing"

func TestRoleAtLeast(t *testing.T) {
	cases := []struct {
		have, want string
		expect     bool
	}{
		{RoleOwner, RoleViewer, true},
		{RoleOwner, RoleOwner, true},
		{RoleEditor, RoleViewer, true},
		{RoleEditor, RoleOwner, false},
		{RoleViewer, RoleEditor, false},
		{"", RoleViewer, false},
		{"bogus", RoleViewer, false},
	}
	for _, c := range cases {
		if got := RoleAtLeast(c.have, c.want); got != c.expect {
			t.Errorf("RoleAtLeast(%q, %q) = %v, want %v", c.have, c.want, got, c.expect)
		}
	}
}

type fakeMemberRepo struct {
	Repository
	roles []string
}

func (f *fakeMemberRepo) RolesFor(projectID, userID string) ([]string, error) {
	return f.roles, nil
}

func TestEffectiveRolePicksHighest(t *testing.T) {
	cases := []struct {
		roles  []string
		expect string
	}{
		{nil, ""},
		{[]string{RoleViewer}, RoleViewer},
		{[]string{RoleViewer, RoleEditor}, RoleEditor},
		{[]string{RoleEditor, RoleOwner, RoleViewer}, RoleOwner},
		{[]string{RoleViewer, RoleViewer}, RoleViewer},
	}
	for _, c := range cases {
		s := NewDefaultService(&fakeMemberRepo{roles: c.roles})
		got, err := s.EffectiveRole("p", "u")
		if err != nil {
			t.Fatalf("EffectiveRole error: %v", err)
		}
		if got != c.expect {
			t.Errorf("EffectiveRole(%v) = %q, want %q", c.roles, got, c.expect)
		}
	}
}
