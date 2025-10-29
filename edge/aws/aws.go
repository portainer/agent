package aws

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/portainer/api/edge"
	retry "github.com/portainer/portainer/pkg/retry"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	iamra "github.com/aws/rolesanywhere-credential-helper/aws_signing_helper"
	"github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
	"github.com/rs/zerolog/log"
)

var (
	ErrFailedToFetchECRCredentials = errors.New("failed to fetch ECR credentials")
	ErrNotPrivateECRRepo           = errors.New("repository url is not a private ECR repository")

	iamraSessionDurationSec  = 60 * 60         // 1 hour or profile duration (configured in IAM), absolute maximum is 12 hours
	iamraSessionExpiryBuffer = 5 * time.Minute // buffer expiry for simple safety
)

func DoAWSIAMRolesAnywhereAuthAndGetECRCredentials(serverURL string, awsConfig *agent.AWSConfig) (*edge.RegistryCredentials, error) {
	if serverURL == "" || awsConfig == nil {
		log.Info().
			Str("server_url", serverURL).
			Msg("incomplete information when using local AWS config for credential lookup")

		return nil, errors.New("invalid ecr configuration")
	}

	// use AWS lib to parse the registry URL and identify if it's an ECR registry
	registry, err := api.ExtractRegistry(serverURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse serverURL: %w, caused by: %w", ErrNotPrivateECRRepo, err)
	}

	if registry == nil {
		log.Warn().Str("server_url", serverURL).Msg("aws client failed to parse server url, no info about registry returned")
		return nil, ErrNotPrivateECRRepo
	}

	if registry.Service == api.ServiceECRPublic {
		log.Info().Str("server_url", serverURL).Msg("aws client recognized url as a public ECR registry")
		return nil, ErrNotPrivateECRRepo
	}

	log.Debug().
		Str("server_url", serverURL).
		Str("Service", string(registry.Service)).
		Str("ID", registry.ID).
		Bool("FIPS", registry.FIPS).
		Str("Region", registry.Region).
		Msg("successfully identified ECR registry")

	ecrClient, err := getOrRefreshGlobalClient(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to get or refresh global ECR client: %w", ErrFailedToFetchECRCredentials, err)
	}

	ecrCreds, err := ecrClient.GetCredentials(serverURL)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to get ECR credentials: %w", ErrFailedToFetchECRCredentials, err)
	}

	return &edge.RegistryCredentials{
		ServerURL: serverURL,
		Username:  ecrCreds.Username,
		Secret:    ecrCreds.Password,
	}, nil
}

func authenticateAgainstIAMRA(awsConfig *agent.AWSConfig) (*iamra.CredentialProcessOutput, error) {
	credentialsOptions := iamra.CredentialsOpts{
		PrivateKeyId:      awsConfig.ClientKeyPath,
		CertificateId:     awsConfig.ClientCertPath,
		RoleArn:           awsConfig.RoleARN,
		ProfileArnStr:     awsConfig.ProfileARN,
		TrustAnchorArnStr: awsConfig.TrustAnchorARN,
		SessionDuration:   iamraSessionDurationSec, // note actual duration is min(profileDurationSeconds, createSessionDurationSeconds) - profileDurationSeconds is default 1h but configurable
		NoVerifySSL:       false,
		WithProxy:         false,
	}

	if awsConfig.ClientBundlePath != "" {
		credentialsOptions.CertificateBundleId = awsConfig.ClientBundlePath
	}

	credentialProcessOutput, err := iamra.GenerateCredentials(&credentialsOptions)
	if err != nil {
		return nil, err
	}

	return &credentialProcessOutput, nil
}

func ExtractAwsConfig(options *agent.Options) *agent.AWSConfig {
	if isValidAWSConfig(options) {
		log.Info().Msg("AWS configuration detected")
		return &agent.AWSConfig{
			ClientCertPath: options.AWSClientCert,
			ClientKeyPath:  options.AWSClientKey,
			RoleARN:        options.AWSRoleARN,
			TrustAnchorARN: options.AWSTrustAnchorARN,
			ProfileARN:     options.AWSProfileARN,
			Region:         options.AWSRegion,
		}
	}

	return nil
}

func isValidAWSConfig(opts *agent.Options) bool {
	return opts != nil && opts.AWSRoleARN != "" && opts.AWSTrustAnchorARN != "" && opts.AWSProfileARN != "" && opts.AWSRegion != ""
}

var (
	mu                 sync.Mutex
	globalClient       api.Client
	globalClientExpiry time.Time
	retrySettings      = retry.Default
)

// Important expiry notes:
// The IAMRA login has a *different* duration than the ECR credentials, but also the ECR client includes a credential
// cache. The AWS client with static credentials will stop working before cached ECR credentials which is hardcoded by
// AWS to 12 hours.
//
// Consider the following scenario:
// - IAMRA login done and expires in 1 hour (as per configured profile duration), AWS client is created with static credentials
// - ECR client is created with static credentials
// - ECR client calls GetCredentials for registry 123 and caches the credentials for 12 hours (hardcoded by AWS)
// - 2 hours pass
// - ECR client calls GetCredentials for registry 123, cached credentials are returned and is valid for 10 hours
// - ECR client calls GetCredentials for registry 456, no cache so an API call is made with expired credentials, ERROR
// - 11 hours pass
// - ECR client calls GetCredentials for registry 123, cache is expired so makes an API call with expired credentials, ERROR
//
// Given this, the ECR client is only kept until the IAMRA login expires (minus the buffer). Note that no matter what is
// requested when doing the IAMRA CreateSession it is the minimum of what is requested and the configured profile
// duration. Also, the GetCredentials api returns an expiry, but it is not exposed by the API library, so we have to
// guess at the expiry time. Although it appears to be somewhat hardcoded by AWS, it could change. In any case, it will
// be much more than the IAMRA temporary credentials so that is our real limit.
//
// The AWS library caches AWS credentials in a file (~/.ecr/cache.json), but it includes the access key as part of the
// cache key. It would be temping to try and use this directly, but because of the temporary IAMRA login, the cache key
// is unpredictable and always changing.

// getOrRefreshGlobalClient check for a stored and still valid ECR client, otherwise authenticate against IAMRA and
// create a new client
func getOrRefreshGlobalClient(awsConfig *agent.AWSConfig) (api.Client, error) {
	mu.Lock()
	defer mu.Unlock()

	if globalClient != nil && time.Now().Before(globalClientExpiry) {
		// global client is still valid (temporary credentials inside via static provider have not expired), reuse it
		log.Debug().Str("expires_in", time.Until(globalClientExpiry).String()).Msg("global ECR client is valid, reusing it")
		return globalClient, nil
	}

	// the iamra client does not contain a retry mechanism like the regular client, so simple retry is used here
	iamraCreds, err := retry.RetryWithWarnings("IAM Roles Anywhere authentication", retrySettings, func() (*iamra.CredentialProcessOutput, error) {
		return authenticateAgainstIAMRA(awsConfig)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: login to IAM Roles Anywhere failed: %w", ErrFailedToFetchECRCredentials, err)
	}

	// set expiry to reported expiry minus buffer
	expiry, err := time.Parse(time.RFC3339, iamraCreds.Expiration)
	if err != nil {
		// extremely unlikely to happen, but add a fallback rather than failing the process
		log.Warn().Err(err).
			Str("expiration_string", iamraCreds.Expiration).
			Msg("unable to parse IAM Roles Anywhere expiration, falling back to immediate expiry")
		globalClientExpiry = time.Time{}
	} else {
		globalClientExpiry = expiry.Add(-iamraSessionExpiryBuffer)
	}

	factory := api.DefaultClientFactory{}

	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(awsConfig.Region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider( // static credential provider, the AWS client can't do anything when it expires
				iamraCreds.AccessKeyId,
				iamraCreds.SecretAccessKey,
				iamraCreds.SessionToken,
			),
		),
		config.WithRetryMode(awssdk.RetryModeStandard), // use standard aws sdk retry for the next step
	)
	if err != nil {
		log.Err(err).Msg("unable to build AWS client config")
		return nil, fmt.Errorf("unable to build AWS client config: %w", err)
	}

	globalClient = factory.NewClient(cfg)
	log.Debug().
		Str("expires_in", time.Until(globalClientExpiry).String()). // string duration reads better in logs
		Msg("IAMRA authenticated and global ECR client created")

	return globalClient, nil
}
