package configfx

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// defaultConfig is the athenareader.config shipped in this repository. It is
// baked into the binary at build time so a first run never has to fetch
// configuration (bucket, region, admin/read-only flag) over the network.
//
//go:embed athenareader.config
var defaultConfig []byte

func setUpFlagUsage() {
	flag.Usage = func() {
		preBody := "NAME\n\tathenareader - read athena data from command line\n\n"
		desc := "\nEXAMPLES\n\n" +
			"\t$ athenareader -d sampledb -q \"select request_timestamp,elb_name from elb_logs limit 2\"\n" +
			"\trequest_timestamp,elb_name\n" +
			"\t2015-01-03T00:00:00.516940Z,elb_demo_004\n" +
			"\t2015-01-03T00:00:00.902953Z,elb_demo_004\n\n" +
			"\t$ athenareader -d sampledb -q \"select request_timestamp,elb_name from elb_logs limit 2\" -r\n" +
			"\t2015-01-05T20:00:01.206255Z,elb_demo_002\n" +
			"\t2015-01-05T20:00:01.612598Z,elb_demo_008\n\n" +
			"\t$ athenareader -d sampledb -b s3://example-athena-query-result -q tools/query.sql\n" +
			"\trequest_timestamp,elb_name\n" +
			"\t2015-01-06T00:00:00.516940Z,elb_demo_009\n\n" +
			"\n\tAdd '-m' to enable moneywise mode. The first line will display query cost under moneywise mode.\n\n" +
			"\t$ athenareader -b s3://athena-query-result -q 'select count(*) as cnt from sampledb.elb_logs' -m\n" +
			"\tquery cost: 0.00184898369752772851 USD\n" +
			"\tcnt\n" +
			"\t1356206\n\n" +
			"\n\tAdd '-a' to enable admin mode. Database write is enabled at driver level under admin mode.\n\n" +
			"\t$ athenareader -b s3://athena-query-result -q 'DROP TABLE IF EXISTS depreacted_table' -a\n" +
			"\t\n" +
			"AUTHORS\n\tCreated by Henry Fuheng Wu at Uber Technologies. Maintained by Cork Cyber.\n\n" +
			"REPORTING BUGS\n\thttps://github.com/CorkCyber/athenadriver/issues\n"
		fmt.Fprint(flag.CommandLine.Output(), preBody)
		fmt.Fprintf(flag.CommandLine.Output(),
			"SYNOPSIS\n\n\t%s [-v] [-b OUTPUT_BUCKET] [-d DATABASE_NAME] [-q QUERY_STRING_OR_FILE] [-r] [-a] [-m] [-y STYLE_NAME] [-o OUTPUT_FORMAT]\n\nDESCRIPTION\n\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprint(flag.CommandLine.Output(), desc)
	}
}

// resolveConfigFile returns the path of the athenareader.config to use,
// looking in $HOME then the working directory. If neither exists, the
// embedded default is written to $HOME and used. No network access.
func resolveConfigFile() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	home := filepath.Join(h, "athenareader.config")
	for _, p := range []string{home, "athenareader.config"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if err := os.WriteFile(home, defaultConfig, 0o600); err != nil {
		return "", fmt.Errorf("no athenareader.config found and could not write default to %s: %w", home, err)
	}
	return home, nil
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
