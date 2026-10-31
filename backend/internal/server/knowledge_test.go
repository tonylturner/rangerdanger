package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

func TestKnowledgeActivePackageWithoutHealthyRange(t *testing.T) {
	s := routedServer(t)
	catalog, err := labs.NewLoader(filepath.Join(repoRoot(), "lab-definitions"), ValidatorRequirements()).Load()
	if err != nil {
		t.Fatal(err)
	}
	s.setCatalog(catalog)
	rng := s.rng.(*fakeRange)
	rng.gen = nil
	rng.status.Phase = lifecycle.PhaseFailed
	for _, id := range []string{usPackageID, euPackageID, usPackageID} {
		rng.active = id
		rec := serve(s, http.MethodGet, "/api/knowledge")
		if rec.Code != http.StatusOK {
			t.Fatalf("knowledge: %d %s", rec.Code, rec.Body)
		}
		var got struct {
			PackageID string                  `json:"package_id"`
			Revision  int                     `json:"revision"`
			Sections  []labs.KnowledgeSection `json:"sections"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		var expected labs.Package
		for _, pkg := range catalog.Packages {
			if pkg.ID == id {
				expected = pkg
			}
		}
		// File is intentionally excluded from JSON; every other ordered value
		// must be identical to the loaded snapshot.
		data, err := json.Marshal(expected.Knowledge)
		if err != nil {
			t.Fatal(err)
		}
		var want []labs.KnowledgeSection
		if err := json.Unmarshal(data, &want); err != nil {
			t.Fatal(err)
		}
		if got.PackageID != id || got.Revision != expected.Revision || !reflect.DeepEqual(got.Sections, want) {
			t.Fatal("response differs from active snapshot")
		}
		count := 0
		for _, section := range got.Sections {
			count += len(section.Articles)
		}
		// Both packages teach the same seven labs. EU mirrors every US topic
		// (IEC 104 for DNP3) and also carries IEC104/substation deep-dive
		// articles salvaged from its retired scenarios, so the counts differ.
		wantArticles := map[string]int{usPackageID: 41, euPackageID: 61}
		if count != wantArticles[id] {
			t.Fatalf("%s articles = %d, want %d", id, count, wantArticles[id])
		}
	}
}

func TestKnowledgeMissingIdentity(t *testing.T) {
	for _, id := range []string{"", "not-loaded"} {
		t.Run(id, func(t *testing.T) {
			s := routedServer(t)
			s.rng.(*fakeRange).active = id
			rec := serve(s, http.MethodGet, "/api/knowledge")
			if rec.Code != http.StatusConflict {
				t.Fatalf("got %d %s", rec.Code, rec.Body)
			}
		})
	}
	s := routedServer(t)
	s.setCatalog(nil)
	if rec := serve(s, http.MethodGet, "/api/knowledge"); rec.Code != http.StatusConflict {
		t.Fatalf("nil catalog = %d", rec.Code)
	}
}

func TestKnowledgeSeedFailureKeepsSnapshot(t *testing.T) {
	s := routedServer(t)
	// The EU fixture is self-contained, allowing a bad article on disk to
	// exercise the actual admin reseed path without touching shipped files.
	definitions := t.TempDir()
	source := os.DirFS(filepath.Join(repoRoot(), "backend/internal/labs/testdata/eu-definitions"))
	if err := os.CopyFS(definitions, source); err != nil {
		t.Fatal(err)
	}
	s.loader = labs.NewLoader(definitions, ValidatorRequirements())
	s.rng.(*fakeRange).active = euPackageID
	first := serve(s, http.MethodPost, "/api/admin/seed")
	if first.Code != http.StatusOK {
		t.Fatalf("seed = %d %s", first.Code, first.Body)
	}
	before := serve(s, http.MethodGet, "/api/knowledge").Body.String()
	if err := os.WriteFile(filepath.Join(definitions, "packages", euPackageID, "knowledge/baseline.md"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	failed := serve(s, http.MethodPost, "/api/admin/seed")
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("bad knowledge seed = %d %s", failed.Code, failed.Body)
	}
	after := serve(s, http.MethodGet, "/api/knowledge")
	if after.Code != http.StatusOK || after.Body.String() != before {
		t.Fatal("bad seed replaced the knowledge snapshot")
	}
}
