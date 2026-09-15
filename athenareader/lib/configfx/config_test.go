// SPDX-License-Identifier: MIT

package configfx

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigFile parses the shipped config without go.uber.org/config.
func TestLoadConfigFile(t *testing.T) {
	out, in, err := loadConfigFile("athenareader.config")
	if err != nil {
		t.Fatal(err)
	}
	if out.Render != "csv" || out.Page != 1024 || out.Style != "StyleColoredYellowWhiteOnBlack" {
		t.Errorf("output config = %+v", out)
	}
	if in.Bucket != "s3://athena-query-result-bucket/" || in.Region != "us-east-1" ||
		in.Database != "sampledb" || in.Admin {
		t.Errorf("input config = %+v", in)
	}
}

func TestLoadConfigFileErrors(t *testing.T) {
	if _, _, err := loadConfigFile(filepath.Join(t.TempDir(), "nope.config")); err == nil {
		t.Error("missing file: want error, got nil")
	}

	bad := filepath.Join(t.TempDir(), "bad.config")
	if err := os.WriteFile(bad, []byte("athenareader: [not, a, map\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfigFile(bad); err == nil {
		t.Error("malformed YAML: want error, got nil")
	}

	// Valid YAML of the wrong shape must not yield a silently zero config.
	wrong := filepath.Join(t.TempDir(), "wrong.config")
	if err := os.WriteFile(wrong, []byte("athenareader: mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfigFile(wrong); err == nil {
		t.Error("wrong shape: want error, got nil")
	}
}
