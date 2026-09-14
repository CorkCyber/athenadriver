// SPDX-License-Identifier: MIT

package configfx

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
)

// TestResolveConfigFileNoNetwork verifies that with no config in $HOME or the
// working directory, resolveConfigFile writes the *embedded* default instead
// of fetching one over the network.
func TestResolveConfigFileNoNetwork(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	chdirTemp(t)

	// Any DNS lookup (i.e. an outbound fetch) fails the test.
	old := net.DefaultResolver.Dial
	t.Cleanup(func() { net.DefaultResolver.Dial = old })
	net.DefaultResolver.Dial = func(context.Context, string, string) (net.Conn, error) {
		t.Error("unexpected network access")
		return nil, errors.New("network disabled")
	}

	path, err := resolveConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if want := home + "/athenareader.config"; path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(defaultConfig) {
		t.Error("written config does not match embedded default")
	}
}

// TestEmbeddedConfigMatchesRepo keeps the embedded copy in sync with the
// athenareader.config shipped at the module root.
func TestEmbeddedConfigMatchesRepo(t *testing.T) {
	repo, err := os.ReadFile("../../athenareader.config")
	if err != nil {
		t.Fatal(err)
	}
	if string(repo) != string(defaultConfig) {
		t.Error("lib/configfx/athenareader.config has drifted from athenareader/athenareader.config")
	}
}

// TestResolveConfigFilePrefersExisting checks an existing $HOME config wins.
func TestResolveConfigFilePrefersExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	chdirTemp(t)

	path := home + "/athenareader.config"
	if err := os.WriteFile(path, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveConfigFile(); err != nil || got != path {
		t.Fatalf("resolveConfigFile() = %q, %v", got, err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "mine\n" {
		t.Error("existing config was overwritten")
	}
}

func chdirTemp(t *testing.T) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}
