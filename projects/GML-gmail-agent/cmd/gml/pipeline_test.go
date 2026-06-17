package main

import (
	"os"
	"testing"
)

func TestStringFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		flag string
		def  string
		want string
	}{
		{"found", []string{"--model", "claude"}, "--model", "gemini", "claude"},
		{"missing", []string{"--days", "3"}, "--model", "gemini", "gemini"},
		{"no value after flag", []string{"--model"}, "--model", "gemini", "gemini"},
		{"empty args", nil, "--model", "gemini", "gemini"},
		{"multiple flags", []string{"--days", "3", "--model", "claude"}, "--model", "gemini", "claude"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stringFlag(c.args, c.flag, c.def)
			if got != c.want {
				t.Errorf("stringFlag(%v, %q, %q) = %q, want %q", c.args, c.flag, c.def, got, c.want)
			}
		})
	}
}

func TestIntFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		flag string
		def  int
		want int
	}{
		{"found", []string{"--interval", "15"}, "--interval", 0, 15},
		{"missing", []string{"--model", "x"}, "--interval", 30, 30},
		{"no value", []string{"--interval"}, "--interval", 5, 5},
		{"non-numeric", []string{"--interval", "abc"}, "--interval", 5, 5},
		{"empty args", nil, "--interval", 10, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := intFlag(c.args, c.flag, c.def)
			if got != c.want {
				t.Errorf("intFlag(%v, %q, %d) = %d, want %d", c.args, c.flag, c.def, got, c.want)
			}
		})
	}
}

func TestHasFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		flag string
		want bool
	}{
		{"present", []string{"--no-llm", "--json"}, "--no-llm", true},
		{"absent", []string{"--model", "gemini"}, "--no-llm", false},
		{"empty", nil, "--no-llm", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := hasFlag(c.args, c.flag)
			if got != c.want {
				t.Errorf("hasFlag(%v, %q) = %v, want %v", c.args, c.flag, got, c.want)
			}
		})
	}
}

func TestRulesYAMLPath(t *testing.T) {
	os.Unsetenv("GML_RULES")
	if got := rulesYAMLPath(); got != "data/rules.yaml" {
		t.Errorf("default rulesYAMLPath = %q, want data/rules.yaml", got)
	}

	os.Setenv("GML_RULES", "/custom/rules.yaml")
	defer os.Unsetenv("GML_RULES")
	if got := rulesYAMLPath(); got != "/custom/rules.yaml" {
		t.Errorf("custom rulesYAMLPath = %q, want /custom/rules.yaml", got)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"hi", 2, "hi"},
		{"abcdef", 5, "ab..."},
	}
	for _, c := range cases {
		got := truncate(c.s, c.n)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

func TestPipelineDedup_EmptyInput(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", "[]"},
		{"empty array", "[]", "[]"},
		{"whitespace only", "  \n  ", "[]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := pipelineDedup(nil, nil, "", c.input, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("pipelineDedup(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}
