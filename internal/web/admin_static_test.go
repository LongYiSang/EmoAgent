package web

import "testing"

func TestAdminStaticIsViteEntrypoint(t *testing.T) {
	requireEmbeddedDist(t)
	assertViteEntrypoint(t, "static/dist/admin.html", "/assets/admin-")
}
