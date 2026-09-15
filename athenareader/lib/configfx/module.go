// SPDX-License-Identifier: MIT

package configfx

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	drv "github.com/CorkCyber/athenadriver/v2/go"
	"gopkg.in/yaml.v3"
)

// ReaderOutputConfig is to represent the output section of configuration file
type ReaderOutputConfig struct {
	// Render is for the output format
	Render string `yaml:"render"`
	// Page is for the pagination
	Page int `yaml:"pagesize"`
	// Style is output style
	Style string `yaml:"style"`
	// Rowonly is for displaying header or not
	Rowonly bool `yaml:"rowonly"`
	// Moneywise is for displaying spending or not
	Moneywise bool `yaml:"moneywise"`
	// Fastfail is for multiple queries
	Fastfail bool `yaml:"fastfail"`
}

// ReaderInputConfig is to represent the input section of configuration file
type ReaderInputConfig struct {
	// Bucket is the output bucket
	Bucket string `yaml:"bucket"`
	// Region is AWS region
	Region string `yaml:"region"`
	// Database is the name of the DB
	Database string `yaml:"database"`
	// Admin is for write mode
	Admin bool `yaml:"admin"`
}

// AthenaDriverConfig is Athena Driver Configuration
type AthenaDriverConfig struct {
	// OutputConfig is for the output section of the config
	OutputConfig ReaderOutputConfig
	// InputConfig is for the input section of the config
	InputConfig ReaderInputConfig
	// QueryString is the query string
	QueryString []string
	// DrvConfig is the datastructure of Driver Config
	DrvConfig *drv.Config
}

// fileConfig mirrors the on-disk athenareader.config layout.
type fileConfig struct {
	Athenareader struct {
		Output ReaderOutputConfig `yaml:"output"`
		Input  ReaderInputConfig  `yaml:"input"`
	} `yaml:"athenareader"`
}

func init() {
	setUpFlagUsage()
}

// loadConfigFile parses an athenareader.config YAML file. A missing or
// malformed file is an error, never a silently zero-valued config.
func loadConfigFile(path string) (ReaderOutputConfig, ReaderInputConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ReaderOutputConfig{}, ReaderInputConfig{}, err
	}
	var fc fileConfig
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return ReaderOutputConfig{}, ReaderInputConfig{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return fc.Athenareader.Output, fc.Athenareader.Input, nil
}

// New parses the command line flags and the athenareader.config file into
// the CLI's configuration.
func New() (AthenaDriverConfig, error) {
	var mc = AthenaDriverConfig{
		QueryString: make([]string, 0),
	}

	var bucket = flag.String("b", defaultOutputBucket, "Athena resultset output bucket")
	var database = flag.String("d", "default", "The database you want to query")
	var query = flag.String("q", "select 1", "The SQL query string or a file containing SQL string")
	var rowOnly = flag.Bool("r", false, "Display rows only, don't show the first row as columninfo")
	var moneyWise = flag.Bool("m", false, "Enable moneywise mode to display the query cost as the first line of the output")
	var versionFlag = flag.Bool("v", false, "Print the current version and exit")
	var admin = flag.Bool("a", false, "Enable admin mode, so database write(create/drop) is allowed at athenadriver level")
	var style = flag.String("y", "default", "Output rendering style")
	var format = flag.String("o", "csv", "Output format(options: table, markdown, csv, html)")
	var fastFail = flag.Bool("f", true, "fast fail when where are multiple queries")

	flag.Parse()
	if *versionFlag {
		println("Current build version: v" + drv.DriverVersion())
		os.Exit(0)
	}

	cfgPath, err := resolveConfigFile()
	if err != nil {
		return mc, err
	}
	mc.OutputConfig, mc.InputConfig, err = loadConfigFile(cfgPath)
	if err != nil {
		return mc, err
	}

	filePath := expand(*query)
	if _, statErr := os.Stat(filePath); statErr == nil {
		b, err := os.ReadFile(filePath)
		if err != nil {
			return mc, err
		}
		mc.QueryString = strings.Split(string(b), "\n\n") // convert content to a '[]string'
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		// Stat failed for a reason other than "not there" (e.g. permissions):
		// don't silently reinterpret a file path as literal SQL.
		return mc, statErr
	} else {
		mc.QueryString = append(mc.QueryString, *query)
	}

	if isFlagPassed("b") {
		mc.InputConfig.Bucket = *bucket
	}
	if isFlagPassed("d") {
		mc.InputConfig.Database = *database
	}
	if isFlagPassed("r") {
		mc.OutputConfig.Rowonly = *rowOnly
	}
	if isFlagPassed("m") {
		mc.OutputConfig.Moneywise = *moneyWise
	}
	if isFlagPassed("f") {
		mc.OutputConfig.Fastfail = *fastFail
	}
	if isFlagPassed("a") {
		mc.InputConfig.Admin = *admin
	}
	if isFlagPassed("y") {
		mc.OutputConfig.Style = *style
	}
	if isFlagPassed("o") {
		mc.OutputConfig.Render = *format
	}

	// No credentials here: leaving AccessID empty makes the driver use the
	// default AWS credential chain (SSO, ~/.aws, IRSA, IMDS).
	mc.DrvConfig = drv.NewNoOpsConfig()
	if err := mc.DrvConfig.SetOutputBucket(mc.InputConfig.Bucket); err != nil {
		return mc, err
	}
	if mc.InputConfig.Region != "" {
		if err := mc.DrvConfig.SetRegion(mc.InputConfig.Region); err != nil {
			return mc, err
		}
	}
	if mc.OutputConfig.Moneywise {
		mc.DrvConfig.MoneyWise = true
	}
	mc.DrvConfig.DB = mc.InputConfig.Database
	if !mc.InputConfig.Admin {
		mc.DrvConfig.ReadOnly = true
	}
	return mc, nil
}

func expand(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path // better an unexpanded path than a silently wrong one
	}
	return filepath.Join(home, path[1:])
}
