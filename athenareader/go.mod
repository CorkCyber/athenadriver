module github.com/CorkCyber/athenadriver/athenareader

go 1.22

// Use the in-repo driver during development. Consumers building the
// CLI against a tagged release should drop or override this.
replace github.com/CorkCyber/athenadriver/v2 => ../

require (
	github.com/CorkCyber/athenadriver/v2 v2.0.0
	github.com/jedib0t/go-pretty/v6 v6.2.7
	go.uber.org/config v1.4.0
	go.uber.org/fx v1.12.0
)

require (
	github.com/aws/aws-sdk-go-v2 v1.32.7 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.28.8 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.49 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.22 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.26 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.26 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/athena v1.49.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.24.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.28.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.4 // indirect
	github.com/aws/smithy-go v1.22.1 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/dig v1.9.0 // indirect
	go.uber.org/multierr v1.5.0 // indirect
	golang.org/x/lint v0.0.0-20190930215403-16217165b5de // indirect
	golang.org/x/sys v0.27.0 // indirect
	golang.org/x/text v0.3.3 // indirect
	golang.org/x/tools v0.1.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
