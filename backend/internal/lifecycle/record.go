package lifecycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// RecordSchema is the runtime record schema this backend reads and writes.
const RecordSchema = 1

// RecordFile is the runtime record's file name in the backend data directory.
const RecordFile = "range.json"

// Phase is where the range lifecycle is.
type Phase string

const (
	PhaseNone        Phase = "none"        // no range was ever started
	PhasePreflight   Phase = "preflight"   // checking a target; the old range still serves
	PhaseStopping    Phase = "stopping"    // draining and removing the old range
	PhaseStarting    Phase = "starting"    // compose up of the new range
	PhaseConfiguring Phase = "configuring" // routes, policy, workers
	PhaseReady       Phase = "ready"
	PhaseFailed      Phase = "failed"
)

// persisted reports whether the record may hold the phase.
func (p Phase) persisted() bool {
	switch p {
	case PhaseStopping, PhaseStarting, PhaseConfiguring, PhaseReady, PhaseFailed:
		return true
	}
	return false
}

// Record is the runtime record: which range the backend last started, and
// how far it got. It survives backend restarts.
type Record struct {
	Schema     int           `json:"schema"`
	Generation uint64        `json:"generation"`
	Phase      Phase         `json:"phase"`
	Package    string        `json:"package"`
	Revision   int           `json:"revision"`
	Mode       manifest.Mode `json:"mode"`
	Root       string        `json:"root"`
	Version    string        `json:"version"` // backend build that wrote the record
	Error      string        `json:"error"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

// RecordStore reads and atomically replaces the record file.
type RecordStore struct {
	Path string
}

// Load returns the record, or nil when none was ever written.
func (s RecordStore) Load() (*Record, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read runtime record: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var rec Record
	if err := decoder.Decode(&rec); err != nil {
		return nil, fmt.Errorf("decode runtime record %s: %w", s.Path, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode runtime record %s: trailing data", s.Path)
	}
	if rec.Schema != RecordSchema {
		return nil, fmt.Errorf("runtime record %s: schema %d, want %d", s.Path, rec.Schema, RecordSchema)
	}
	if !rec.Phase.persisted() {
		return nil, fmt.Errorf("runtime record %s: phase %q is not a persisted phase", s.Path, rec.Phase)
	}
	return &rec, nil
}

// Save writes the record to a temporary file and renames it over the old
// one, so a crash leaves either the old or the new record.
func (s RecordStore) Save(rec Record) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime record: %w", err)
	}
	tmp := filepath.Join(filepath.Dir(s.Path), "."+filepath.Base(s.Path)+".tmp")
	if err := writeSynced(tmp, append(data, '\n')); err != nil {
		return fmt.Errorf("write runtime record: %w", err)
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		return fmt.Errorf("replace runtime record: %w", err)
	}
	return nil
}

// writeSynced writes data to path and flushes it to disk before returning.
func writeSynced(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
