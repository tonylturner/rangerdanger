package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordStoreRoundTrip(t *testing.T) {
	store := RecordStore{Path: filepath.Join(t.TempDir(), RecordFile)}
	if rec, err := store.Load(); rec != nil || err != nil {
		t.Fatalf("Load of a missing record = %v, %v; want nil, nil", rec, err)
	}
	want := Record{Schema: RecordSchema, Generation: 7, Phase: PhaseReady, Package: "pkg-a", Revision: 2,
		Mode: "source", Root: "/H", Version: "v1", UpdatedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || *got != want {
		t.Fatalf("Load = %+v, %v; want %+v", got, err, want)
	}
	entries, err := os.ReadDir(filepath.Dir(store.Path))
	if err != nil || len(entries) != 1 {
		t.Errorf("directory holds %v, want only the record", entries)
	}
}

func TestRecordStoreRejects(t *testing.T) {
	tests := map[string]string{
		"unknown field": `{"schema":1,"phase":"ready","extra":true}`,
		"wrong schema":  `{"schema":2,"phase":"ready"}`,
		"preflight":     `{"schema":1,"phase":"preflight"}`,
		"none":          `{"schema":1,"phase":"none"}`,
		"trailing data": `{"schema":1,"phase":"ready"}{}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			store := RecordStore{Path: filepath.Join(t.TempDir(), RecordFile)}
			if err := os.WriteFile(store.Path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if rec, err := store.Load(); err == nil || !strings.Contains(err.Error(), "runtime record") {
				t.Errorf("Load = %+v, %v; want an error", rec, err)
			}
		})
	}
}
