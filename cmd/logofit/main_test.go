package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpListsCommands(t *testing.T) {
	var out bytes.Buffer
	c := RootCmd()
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs([]string{"--help"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"fetch", "run", "trace", "measure"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("help does not mention %q", w)
		}
	}
}

func TestRunRequiresTheOriginals(t *testing.T) {
	for _, args := range [][]string{{"run"}, {"trace"}, {"measure"}, {"run", "--lockup", "/nonexistent.png", "--mark", "/nonexistent.png"}} {
		c := RootCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}
