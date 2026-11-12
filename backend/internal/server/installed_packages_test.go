package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

func TestListInstalledPackagesAndReload(t *testing.T) {
	s := routedServer(t)
	file := filepath.Join(s.cfg.Root, "data", "installed-packages")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("rangerdanger-installed-packages-v1\ntest-package\n")
	rec := serve(s, http.MethodGet, "/api/packages")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "other-package") || !strings.Contains(rec.Body.String(), "test-package") {
		t.Fatalf("installed list = %d %s", rec.Code, rec.Body)
	}
	write("rangerdanger-installed-packages-v1\nother-package\ntest-package\n")
	rec = serve(s, http.MethodGet, "/api/packages")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "other-package") {
		t.Fatalf("host add not visible = %d %s", rec.Code, rec.Body)
	}
	write("broken\n")
	if rec = serve(s, http.MethodGet, "/api/packages"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt receipt = %d %s", rec.Code, rec.Body)
	}
}

func TestPostRangePackageNotInstalled(t *testing.T) {
	s := routedServer(t)
	s.rng.(*fakeRange).err = &lifecycle.PackageNotInstalledError{ID: "other-package"}
	rec, body := rangeRequest(s, http.MethodPost, "/api/range", `{"package":"other-package"}`)
	if rec.Code != http.StatusUnprocessableEntity || body["code"] != "package_not_installed" ||
		body["error"] != (&lifecycle.PackageNotInstalledError{ID: "other-package"}).Error() {
		t.Fatalf("uninstalled = %d %s", rec.Code, rec.Body)
	}
}
