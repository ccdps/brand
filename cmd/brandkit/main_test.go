package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(args ...string) (string, string, error) {
	var out, errb bytes.Buffer
	c := RootCmd()
	c.SetOut(&out)
	c.SetErr(&errb)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), errb.String(), err
}

func TestGenerateThenCheck(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kit")
	src := []string{"--mark", "../../assets/ccdps-mark-v3.svg", "--font", "../../fonts/Figtree-700.ttf"}
	out, _, err := run(append([]string{"generate", "--out", dir}, src...)...)
	if err != nil || !strings.Contains(out, "wrote") {
		t.Fatalf("generate: %v %q", err, out)
	}
	out, _, err = run(append([]string{"check", "--dir", dir, "--against", dir}, src...)...)
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("check: %v %q", err, out)
	}
	if _, msg, err := run(append([]string{"check", "--dir", filepath.Join(dir, "nope")}, src...)...); err == nil || msg == "" {
		t.Errorf("check on a missing dir passed: %v %q", err, msg)
	}
}

func TestGenerateReportsWhatItWrote(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kit")
	out, _, err := run("generate", "--out", dir, "--mark", "../../assets/ccdps-mark-v3.svg", "--font", "../../fonts/Figtree-700.ttf")
	if err != nil || !strings.Contains(out, "into "+dir) || !strings.HasPrefix(out, "wrote 2") {
		t.Errorf("generate: %v %q", err, out)
	}
}

func TestGenerateFailsWhenItCannotWrite(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := run("generate", "--out", filepath.Join(f, "kit"), "--mark", "../../assets/ccdps-mark-v3.svg", "--font", "../../fonts/Figtree-700.ttf")
	if err == nil {
		t.Error("generate under a file succeeded")
	}
}

// noPanic, deferred, turns a panic into a failed assertion, so that a guard
// whose removal crashes the code is held by a test that says so.
func noPanic(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Errorf("panicked: %v", r)
	}
}

func TestBadSourcesFail(t *testing.T) {
	defer noPanic(t)
	bad := filepath.Join(t.TempDir(), "bad.svg")
	if err := os.WriteFile(bad, []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"generate", "check"} {
		if _, _, err := run(sub, "--mark", bad, "--font", "../../fonts/Figtree-700.ttf"); err == nil {
			t.Errorf("%s accepted a mark with no stroke", sub)
		}
		// the error names the file, not what the kit made of an empty one
		if _, _, err := run(sub, "--mark", "/nonexistent.svg"); err == nil || !strings.Contains(err.Error(), "/nonexistent.svg") {
			t.Errorf("%s on a missing mark: %v", sub, err)
		}
		if _, _, err := run(sub, "--mark", "../../assets/ccdps-mark-v3.svg", "--font", "/nonexistent.ttf"); err == nil || !strings.Contains(err.Error(), "/nonexistent.ttf") {
			t.Errorf("%s on a missing font: %v", sub, err)
		}
	}
}

func TestCheckNamesEveryProblem(t *testing.T) {
	dir := t.TempDir() // empty: every one of the 29 files is missing
	out, msg, err := run("check", "--dir", dir, "--mark", "../../assets/ccdps-mark-v3.svg", "--font", "../../fonts/Figtree-700.ttf")
	if err == nil || !strings.Contains(err.Error(), "problem(s) in "+dir) {
		t.Errorf("err = %v", err)
	}
	if out != "" || strings.Count(msg, "\n") < 25 || !strings.Contains(msg, "mark-16.png") {
		t.Errorf("stdout %q, stderr %q", out, msg)
	}
}

func TestMissingSourcesFail(t *testing.T) {
	if _, _, err := run("generate", "--mark", "/nonexistent.svg"); err == nil {
		t.Error("missing mark accepted")
	}
	if _, _, err := run("generate", "--mark", "../../assets/ccdps-mark-v3.svg", "--font", "/nonexistent.ttf"); err == nil {
		t.Error("missing font accepted")
	}
}
