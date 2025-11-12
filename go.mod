module securityhub-exporter

go 1.25.2

require (
	github.com/aws/aws-sdk-go-v2 v1.39.6
	github.com/aws/aws-sdk-go-v2/config v1.31.19
	github.com/aws/aws-sdk-go-v2/credentials v1.18.23
	github.com/aws/aws-sdk-go-v2/service/iam v1.50.1
	github.com/aws/aws-sdk-go-v2/service/securityhub v1.65.3
	github.com/aws/aws-sdk-go-v2/service/sts v1.40.1
	github.com/google/go-github/v63 v63.0.0
	github.com/joho/godotenv v1.5.1
	golang.org/x/oauth2 v0.33.0
)

require (
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.6 // indirect
	github.com/aws/smithy-go v1.23.2 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
)
