// SPDX-License-Identifier: MIT

package athenadriver

import "runtime/debug"

// TContextKey is a type for key in context.
type TContextKey string

const (
	// DriverName is the Name of this DB driver.
	DriverName = "awsathena"

	// DefaultBytesScannedCutoffPerQuery is 1G for every user.
	DefaultBytesScannedCutoffPerQuery = 1024 * 1024 * 1024

	// DefaultDBName is the default database name in Athena.
	DefaultDBName = "default"

	// DefaultWGName is the default workgroup name in Athena
	DefaultWGName = "primary"

	// DefaultRegion is the default region in Athena.
	DefaultRegion = "us-east-1"

	// TimestampUniXFormat is from https://docs.aws.amazon.com/athena/latest/ug/data-types.html.
	// https://stackoverflow.com/questions/20530327/origin-of-mon-jan-2-150405-mst-2006-in-golang
	// RFC3339 is not supported by AWS Athena. It uses session timezone!.
	TimestampUniXFormat = "2006-01-02 15:04:05.000"

	// ZeroDateTimeString is the invalid or zero result for a time.Time
	ZeroDateTimeString = "0001-01-01 00:00:00 +0000 UTC"

	// DateUniXFormat comes along the same way as TimestampUniXFormat.
	DateUniXFormat = "2006-01-02"

	// MetricsKey is the key for Metrics in context
	MetricsKey = TContextKey("MetricsKey")

	// LoggerKey is the key for Logger in context
	LoggerKey = TContextKey("LoggerKey")

	// CatalogKey overrides the Athena data catalog used for a single query.
	// Value must be a non-empty string. When unset, the driver falls back to
	// Config.GetCatalog(), which itself defaults to AWS's default catalog.
	CatalogKey = TContextKey("CatalogKey")

	// ResultEncryptionKey overrides the ResultConfiguration.EncryptionConfiguration
	// sent on StartQueryExecution for a single query. Value must be a
	// *athenatypes.EncryptionConfiguration. Use WithResultEncryption for a
	// typed helper. When unset, the driver falls back to
	// Config.GetResultEncryption().
	ResultEncryptionKey = TContextKey("ResultEncryptionKey")

	// ExpectedBucketOwnerKey overrides ResultConfiguration.ExpectedBucketOwner
	// for a single query. Value must be a non-empty string (12-digit AWS
	// account ID). When unset, the driver falls back to
	// Config.GetExpectedBucketOwner().
	ExpectedBucketOwnerKey = TContextKey("ExpectedBucketOwnerKey")

	// ResultReuseMaxAgeKey opts a single query into Athena's result-reuse
	// cache (engine v3). Value must be a time.Duration in (0, 60min].
	// Athena will reuse a previous successful result for the same query
	// text if it is no older than this duration, saving the scan cost.
	// Use WithResultReuse for a typed helper.
	ResultReuseMaxAgeKey = TContextKey("ResultReuseMaxAgeKey")

	// ClientRequestTokenKey overrides the ClientRequestToken sent on
	// StartQueryExecution for a single query. Value must be a non-empty
	// string (32–128 ASCII chars per Athena's API). When unset, the driver
	// generates a fresh UUID per query so SDK-level retries do not re-charge
	// scan cost.
	ClientRequestTokenKey = TContextKey("ClientRequestTokenKey")

	// DefaultCatalog is the AWS-managed default Athena data catalog.
	DefaultCatalog = "AwsDataCatalog"

	// DummyRegion is a sentinel value used by the auth/lambda examples when
	// they rely on AWS_SDK_LOAD_CONFIG / IRSA / instance profile and want
	// the driver's DSN-driven static credentials path to no-op.
	DummyRegion = "dummy"

	// DummyAccessID — see DummyRegion.
	DummyAccessID = "dummy"

	// DummySecretAccessKey — see DummyRegion.
	DummySecretAccessKey = "dummy"
)

// https://docs.aws.amazon.com/athena/latest/ug/service-limits.html
const (
	// DDLQueryTimeout is DDL query timeout 600 minutes(unit second).
	DDLQueryTimeout = 600 * 60

	// DMLQueryTimeout is DML query timeout 30 minutes(unit second).
	DMLQueryTimeout = 30 * 60

	// PoolInterval is the interval between two status checks(unit second).
	PoolInterval = 3

	// The maximum allowed query string length is 262144 bytes,
	// where the strings are encoded in UTF-8.
	// This is not an adjustable quota. (unit bytes)
	MAXQueryStringLength = 262144
)

const digits01 = "0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789"
const digits10 = "0000000000111111111122222222223333333333444444444455555555556666666666777777777788888888889999999999"

// AthenaColumnTypes is a fixed array of Athena Column Types. An array isn't immutable by nature; you can't make it constant.
var AthenaColumnTypes = [...]string{"tinyint", "smallint", "integer", "bigint", "float", "real", "double",
	"json", "char", "varchar", "varbinary", "row", "string", "binary",
	"struct", "interval year to month", "interval day to second", "decimal",
	"ipaddress", "array", "map", "unknown", "boolean", "date", "time", "time with time zone",
	"timestamp with time zone", "timestamp", "weird_type"}

// pseudo commands all start with `PC_`

// PCGetQID is the pseudo command of getting query execution id of an SQL
const PCGetQID = "get_query_id"

// PCGetQIDStatus is the pseudo command of getting status of a query execution id
const PCGetQIDStatus = "get_query_id_status"

// PCStopQID is the pseudo command to stop a query execution id
const PCStopQID = "stop_query_id"

// PCGetDriverVersion is the pseudo command to get the version of athenadriver
const PCGetDriverVersion = "get_driver_version"

// version is set at build time via ldflags:
//
//	go build -ldflags "-X github.com/CorkCyber/athenadriver/go.version=v2.0.0"
//
// Leave empty for non-release builds; DriverVersion falls back to the VCS
// revision (or "dev") in that case.
var version string

// DriverVersion returns the driver version string surfaced by the
// `pc:get_driver_version` pseudo-command and any caller that wants a
// human-readable build tag. Resolution order:
//  1. -ldflags-injected `version`
//  2. VCS revision (short SHA) recorded in the binary's BuildInfo
//  3. "dev"
func DriverVersion() string { return cachedVersion }

var cachedVersion = func() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				if len(s.Value) > 7 {
					return s.Value[:7]
				}
				return s.Value
			}
		}
	}
	return "dev"
}()
