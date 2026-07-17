// SPDX-License-Identifier: MIT

package athenadriver

// ServiceLimitOverride overrides the driver-side query timeouts. Values
// are seconds; zero means "no override, use the DDLQueryTimeout /
// DMLQueryTimeout package constants". Assumes the underlying AWS
// account has raised the corresponding Athena service limits.
// https://docs.aws.amazon.com/athena/latest/ug/service-limits.html
type ServiceLimitOverride struct {
	DDLQueryTimeout int
	DMLQueryTimeout int
}
