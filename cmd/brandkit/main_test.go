package main

import (
	"bytes"
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

func TestMissingSourcesFail(t *testing.T) {
	if _, _, err := run("generate", "--mark", "/nonexistent.svg"); err == nil {
		t.Error("missing mark accepted")
	}
	if _, _, err := run("generate", "--mark", "../../assets/ccdps-mark-v3.svg", "--font", "/nonexistent.ttf"); err == nil {
		t.Error("missing font accepted")
	}
}
