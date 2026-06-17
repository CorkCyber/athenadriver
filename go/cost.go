// SPDX-License-Identifier: MIT

package athenadriver

// Athena Standard query pricing in USD per terabyte of data scanned, by
// AWS region. Used by the "moneywise" cost estimator. Pricing as of 2024;
// default for unknown regions is $5/TB.
//
// Reference: https://aws.amazon.com/athena/pricing/
var athenaUSDPerTB = map[string]float64{
	// $5.00/TB regions (the majority)
	"us-east-1":      5.00,
	"us-east-2":      5.00,
	"us-west-2":      5.00,
	"af-south-1":     5.00,
	"ap-northeast-1": 5.00,
	"ap-northeast-2": 5.00,
	"ap-northeast-3": 5.00,
	"ap-south-1":     5.00,
	"ap-southeast-1": 5.00,
	"ap-southeast-2": 5.00,
	"eu-central-1":   5.00,
	"eu-north-1":     5.00,
	"eu-south-1":     5.00,
	"eu-west-1":      5.00,
	"eu-west-2":      5.00,
	"us-gov-east-1":  5.00,
	"us-gov-west-1":  5.00,

	// More expensive regions
	"ap-east-1":   5.50, // Hong Kong
	"ca-central-1": 5.50,
	"me-south-1":  6.50, // Bahrain
	"us-west-1":   6.75, // N. California
	"eu-west-3":   7.00, // Paris
	"sa-east-1":   9.00, // São Paulo
}

const (
	// defaultUSDPerTB is the price used when the configured AWS region is
	// not in athenaUSDPerTB. Matches the price for the most common
	// regions (us-east-1, us-east-2, us-west-2, ...).
	defaultUSDPerTB = 5.00

	// bytesPerTB is exactly 2^40, the divisor Athena uses to compute scan
	// charges (TB is a tebibyte for this purpose).
	bytesPerTB = 1024 * 1024 * 1024 * 1024

	// minScanBytes is the Athena per-query scan-charge floor: any query
	// scanning fewer than 10 MB is billed as if it scanned 10 MB.
	minScanBytes int64 = 10 * 1024 * 1024
)

// usdPerByte returns the USD price per byte scanned in the given AWS region.
// Unknown regions fall back to defaultUSDPerTB.
func usdPerByte(region string) float64 {
	rate, ok := athenaUSDPerTB[region]
	if !ok {
		rate = defaultUSDPerTB
	}
	return rate / bytesPerTB
}

// estimateScanCost returns the estimated USD cost for scanning `bytes` of
// data in the given region. Athena bills with a 10 MB minimum, so any
// non-zero scan smaller than 10 MB is charged as 10 MB.
func estimateScanCost(region string, bytes int64) float64 {
	switch {
	case bytes <= 0:
		return 0
	case bytes < minScanBytes:
		bytes = minScanBytes
	}
	return float64(bytes) * usdPerByte(region)
}
