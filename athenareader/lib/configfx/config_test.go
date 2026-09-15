// SPDX-License-Identifier: MIT

package configfx

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// newWithArgs runs New() with a clean flag set, a $HOME holding the given
// config, and the given command line.
func newWithArgs(t *testing.T, config string, args ...string) (AthenaDriverConfig, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(home, "athenareader.config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	oldArgs, oldFlags := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = oldArgs, oldFlags })
	os.Args = append([]string{"athenareader"}, args...)
	flag.CommandLine = flag.NewFlagSet("athenareader", flag.ContinueOnError)
	return New()
}

const fastfailFalseConfig = `athenareader:
  output:
    render: csv
    pagesize: 1024
    style: default
    fastfail: false
  input:
    bucket: s3://bucket/
    region: us-east-1
    database: sampledb
`

// TestNewRespectsConfigFastfail: with no -f passed, the config file value wins.
func TestNewRespectsConfigFastfail(t *testing.T) {
	mc, err := newWithArgs(t, fastfailFalseConfig)
	if err != nil {
		t.Fatal(err)
	}
	if mc.OutputConfig.Fastfail {
		t.Error("Fastfail = true, want false from config file")
	}
}

// TestNewPropagatesQueryFileReadError: -q names something Stat accepts but
// ReadFile cannot read (a directory), which must surface as an error rather
// than an empty query set.
func TestNewPropagatesQueryFileReadError(t *testing.T) {
	if _, err := newWithArgs(t, fastfailFalseConfig, "-q", t.TempDir()); err == nil {
		t.Error("want error for unreadable query file, got nil")
	}
}

// TestNewUsesDefaultCredentialChain: New() has no -accessKey/-secretKey flag
// at all, so DrvConfig must come back with empty credential fields, letting
// the driver fall through to the AWS SDK's default credential chain (SSO,
// ~/.aws, IRSA, IMDS) rather than ever hard-coding a credential path here.
func TestNewUsesDefaultCredentialChain(t *testing.T) {
	mc, err := newWithArgs(t, fastfailFalseConfig)
	if err != nil {
		t.Fatal(err)
	}
	if mc.DrvConfig.AccessID != "" || mc.DrvConfig.SecretAccessKey != "" {
		t.Errorf("DrvConfig credentials = %q/%q, want empty (default credential chain)",
			mc.DrvConfig.AccessID, mc.DrvConfig.SecretAccessKey)
	}
}

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
