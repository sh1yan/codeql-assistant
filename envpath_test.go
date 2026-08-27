package main

import (
	"strings"
	"testing"
)

func TestDedupePath(t *testing.T) {
	got := dedupePath([]string{"/a", " /a ", "", "/b", "/a"})
	want := []string{"/a", "/b"}
	if len(got) != len(want) {
		t.Fatalf("dedupePath() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dedupePath() = %v, want %v", got, want)
		}
	}
}

func TestParseEnvSnapshotWithNUL(t *testing.T) {
	out := []byte("noise\n" + envMarkerBegin + "PATH=/a:/b\x00JAVA_HOME=/x\x00" + envMarkerEnd + "\ntail")
	env := parseEnvSnapshot(out)
	if env == nil {
		t.Fatal("parseEnvSnapshot() returned nil")
	}
	if env["PATH"] != "/a:/b" {
		t.Fatalf("PATH = %q, want /a:/b", env["PATH"])
	}
	if env["JAVA_HOME"] != "/x" {
		t.Fatalf("JAVA_HOME = %q, want /x", env["JAVA_HOME"])
	}
}

func TestParseEnvSnapshotLineFallback(t *testing.T) {
	out := []byte(envMarkerBegin + "PATH=/a:/b\nJAVA_HOME=/x\n" + envMarkerEnd)
	env := parseEnvSnapshot(out)
	if env == nil {
		t.Fatal("parseEnvSnapshot() returned nil")
	}
	if env["PATH"] != "/a:/b" || env["JAVA_HOME"] != "/x" {
		t.Fatalf("unexpected env: %#v", env)
	}
}

func TestBuildPathPrefersShellPath(t *testing.T) {
	got := buildPath("/shell/bin:/shared", "/current/bin:/shared")
	parts := strings.Split(got, ":")
	if parts[0] != "/shell/bin" {
		t.Fatalf("first PATH entry = %q, want /shell/bin", parts[0])
	}
	count := 0
	for _, part := range parts {
		if part == "/shared" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("/shared appears %d times, want once", count)
	}
}

func TestCompareNodeVersion(t *testing.T) {
	if compareNodeVersion("v10.0.0", "v9.0.0") <= 0 {
		t.Fatal("v10.0.0 should sort after v9.0.0")
	}
	if compareNodeVersion("v24.19.0", "v24.19.1") >= 0 {
		t.Fatal("v24.19.1 should sort after v24.19.0")
	}
}
