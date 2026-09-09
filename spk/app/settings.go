package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	download "github.com/openabstractions/abstraction-download/go"
)

// Policy is what this NAS decides for itself, and the reason it is here rather
// than on a record: a record crosses machines, and a machine's answer to "where
// do files go" and "how long do they stay" is different on a laptop, on this
// box and in a container. A retention rule written into a record would be a
// durable instruction to delete somebody else's bytes, executed by whichever
// machine happened to adopt it — which is the shape we closed in the record
// once already.
type Policy struct {
	Folder        string `json:"folder"`
	KeepFilesDays int    `json:"keepFilesDays"`
}

var fallback = Policy{Folder: "files", KeepFilesDays: 0}

type settings struct {
	path string
	mu   sync.Mutex
}

func settingsAt(p string) *settings { return &settings{path: p} }

func (s *settings) read() (Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *settings) load() (Policy, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	p := fallback
	if err := json.Unmarshal(raw, &p); err != nil {
		return fallback, fmt.Errorf("%s is not readable as settings: %w", s.path, err)
	}
	if CheckFolder(p.Folder) != nil {
		p.Folder = fallback.Folder
	}
	return p, nil
}

func (s *settings) write(folder, keepDays string) (Policy, error) {
	folder = strings.Trim(strings.TrimSpace(folder), "/\\")
	if err := CheckFolder(folder); err != nil {
		return fallback, err
	}
	days, err := strconv.Atoi(strings.TrimSpace(keepDays))
	if err != nil || days < 0 {
		return fallback, errors.New("keep finished files for a whole number of days, or 0 for as long as there is room")
	}
	if days > 3650 {
		return fallback, errors.New("that is longer than this NAS will outlive; 0 means forever")
	}
	p := Policy{Folder: folder, KeepFilesDays: days}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fallback, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fallback, err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return fallback, err
	}
	return p, os.Rename(tmp, s.path)
}

// CheckFolder asks the download layer's own refusals rather than inventing a
// second opinion about paths. Everything typed into the settings page arrives
// here, and this is the field the confused-deputy work was about: a folder that
// climbs out of the share, names another platform's filesystem, or aims at the
// store's own records is refused in the words the layer uses everywhere else.
func CheckFolder(p string) error {
	if p == "" {
		return errors.New("name a folder inside the share — files, say")
	}
	if strings.ContainsAny(p, "\x00\n\r") {
		return errors.New("a folder name cannot contain a line break")
	}
	if err := download.ForeignPath(p); err != nil {
		return err
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return errors.New("files land inside the share, so name a folder relative to it: files, not /volume1/files")
	}
	if err := download.EscapesRoot(p); err != nil {
		return err
	}
	return download.ReservedSink("", path.Join(p, "a-file"))
}
