// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"os"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

// To use athenadriver's Config for authentication
func useAthenaDriverConfigForAuth() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucketDev, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		return
	}
	// 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// 3. Query and print results
	var i int
	_ = db.QueryRow("SELECT 123").Scan(&i)
	println("with AthenaDriver Config:", i)
}

// To use the AWS SDK's default credential chain for authentication — shared
// config (~/.aws/config), shared credentials (~/.aws/credentials), SSO,
// container/IRSA credentials, or IMDS. Leaving Config.AccessID unset is what
// selects this path: the driver resolves everything via
// config.LoadDefaultConfig, exactly like any other AWS SDK v2 client.
func useAWSCLIConfigForAuth() {
	// 1. Leave credentials unset in Driver Config.
	conf := drv.NewNoOpsConfig()
	if err := conf.SetOutputBucket(secret.OutputBucketProd); err != nil {
		println(err.Error())
		return
	}
	// 2. Open Connection.
	db, err := sql.Open(drv.DriverName, conf.Stringify())
	if err != nil {
		println(err.Error())
		return
	}
	// 3. Query and print results
	var i int
	err = db.QueryRow("SELECT 456").Scan(&i)
	if err != nil {
		println(err.Error())
	}
	println("with AWS CLI Config:", i)
}

// To use the AWS SDK's default credential chain with a non-default profile
// selected via the AWS_PROFILE environment variable.
// Refer: https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configuring-sdk.html
func useAWSCLIConfigForAuthProfileByEnv(profile string) {
	os.Setenv("AWS_PROFILE", profile)
	// 1. Leave credentials unset in Driver Config; AWS_PROFILE (just set
	// above) selects the shared-config profile.
	conf := drv.NewNoOpsConfig()
	if err := conf.SetOutputBucket(secret.OutputBucketDev); err != nil {
		return
	}
	// 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// 3. Query and print results
	var i int
	_ = db.QueryRow("SELECT 789").Scan(&i)
	println("with AWS CLI Config With Profile:", i)
	os.Unsetenv("AWS_PROFILE")
}

// To use the AWS SDK's default credential chain with a profile set
// explicitly on the Config rather than via the environment.
func useAWSCLIConfigForAuthProfileByManualSetup(profile string) {
	// 1. Leave credentials unset in Driver Config; AWSProfile selects the
	// shared-config profile explicitly.
	conf := drv.NewNoOpsConfig()
	if err := conf.SetOutputBucket(secret.OutputBucketDev); err != nil {
		return
	}
	conf.AWSProfile = profile
	// 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// 3. Query and print results
	var i int
	_ = db.QueryRow("SELECT 789").Scan(&i)
	println("with AWS CLI Config With Profile:", i)
}

func main() {
	useAthenaDriverConfigForAuth()
	useAWSCLIConfigForAuth()
	useAWSCLIConfigForAuthProfileByEnv("henry")
	useAWSCLIConfigForAuthProfileByManualSetup("profile-development")
}

/*
Sample Output:
with AthenaDriver Config: 123
with AWS CLI Config: 456
with AWS CLI Config With Profile: 789
*/
