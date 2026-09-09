package main

import (
	"path/filepath"
	"testing"

	job "github.com/openabstractions/abstraction-job/go"
)

func TestCheckFolderRefusesWhatTheStoreOwnsAndWhatEscapesIt(t *testing.T) {
	for _, p := range []string{
		"", "..", "../elsewhere", "files/../..", "/volume1/abstraction", `\\nas\share`,
		"jobs", "jobs/deeper", "work", "files\nx",
	} {
		if err := CheckFolder(p); err == nil {
			t.Errorf("CheckFolder(%q) allowed it", p)
		}
	}
}

func TestCheckFolderAllowsAnOrdinaryFolder(t *testing.T) {
	for _, p := range []string{"files", "models", "downloads/isos", "a.b-c_d"} {
		if err := CheckFolder(p); err != nil {
			t.Errorf("CheckFolder(%q) = %v", p, err)
		}
	}
}

func TestSettingsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := settingsAt(path).write("models", "14"); err != nil {
		t.Fatal(err)
	}
	got, err := settingsAt(path).read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Folder != "models" || got.KeepFilesDays != 14 {
		t.Fatalf("read back %+v", got)
	}
}

func TestSettingsRefuseADestinationTheBrowserShouldNotName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := settingsAt(path).write("../../etc", "0"); err == nil {
		t.Fatal("wrote an escaping folder")
	}
	if _, err := settingsAt(path).write("files", "-1"); err == nil {
		t.Fatal("wrote a negative retention")
	}
	got, _ := settingsAt(path).read()
	if got.Folder != fallback.Folder {
		t.Fatalf("a refused write changed the policy: %+v", got)
	}
}

func TestFilenameKeepsOneElement(t *testing.T) {
	for in, want := range map[string]string{
		"a.bin": "a.bin", "../../a.bin": "a.bin", `x\y\a.bin`: "a.bin",
		"dir/": "", "..": "", "  b.gguf  ": "b.gguf",
	} {
		if got := filename(in); got != want {
			t.Errorf("filename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAJobNobodyHasTakenYetIsQueuedNotStopped(t *testing.T) {
	waiting := &job.Record{State: job.StatePending}
	if dropped(waiting, true) {
		t.Error("a record with no lease owner reads as abandoned")
	}
	if state, _ := label(waiting, dropped(waiting, true), false); state != "queued" {
		t.Errorf("a record nobody has taken shows as %q", state)
	}

	letGo := &job.Record{State: job.StateRunning}
	letGo.Lease.Owner = "jobd@nas:1"
	if !dropped(letGo, true) {
		t.Error("a claimable running record with an owner is not read as abandoned")
	}
	if state, _ := label(letGo, dropped(letGo, true), false); state != "stopped" {
		t.Errorf("a job whose worker vanished shows as %q", state)
	}
}
