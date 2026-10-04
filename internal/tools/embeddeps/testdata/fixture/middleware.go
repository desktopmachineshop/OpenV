package api

// AuthMiddleware has fields named like Handler's, of another type: the
// rewrite leaves them, and every selector of them, as they are.
type AuthMiddleware struct {
	userService Users
	store       Store
}

// NewAuthMiddleware builds one.
func NewAuthMiddleware(u Users, s Store) *AuthMiddleware {
	return &AuthMiddleware{userService: u, store: s}
}

// Who names the user.
func (m *AuthMiddleware) Who(id string) string {
	return m.userService.Name(id) + m.store.Get(id)
}
