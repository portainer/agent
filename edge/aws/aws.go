package aws

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	iamra "github.com/aws/rolesanywhere-credential-helper/aws_signing_helper"
	"github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
	"github.com/portainer/agent"
	"github.com/portainer/portainer/api/edge"
	"github.com/rs/zerolog/log"
)

var ErrNoCredentials = errors.New("No credentials found")

func DoAWSIAMRolesAnywhereAuthAndGetECRCredentials(serverURL string, awsConfig *agent.AWSConfig) (*edge.RegistryCredentials, error) {
	if serverURL == "" || awsConfig == nil {
		log.Info().
			Str("server_url", serverURL).
			Str("aws configuration region", awsConfig.Region).
			Msg("incomplete information when using local AWS config for credential lookup")

		return nil, errors.New("invalid ecr configuration")
	}

	iamraCreds, err := authenticateAgainstIAMRA(awsConfig)
	if err != nil {
		return nil, err
	}

	factory := api.DefaultClientFactory{}

	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(awsConfig.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(iamraCreds.AccessKeyId, iamraCreds.SecretAccessKey, iamraCreds.SessionToken)),
	)
	if err != nil {
		log.Err(err).Msg("unable to build AWS client config")

		return nil, err
	}

	client := factory.NewClient(cfg)

	creds, err := client.GetCredentials(serverURL)
	if err != nil {
		// This might not be an ECR registry
		// Therefore we deliberately not return an error here so that the upstream logic can fallback to other credential providers
		log.Warn().Str("server_url", serverURL).Err(err).Msg("unable to retrieve credentials from server")

		return nil, ErrNoCredentials
	}

	return &edge.RegistryCredentials{
		ServerURL: serverURL,
		Username:  creds.Username,
		Secret:    creds.Password,
	}, nil
}

func authenticateAgainstIAMRA(awsConfig *agent.AWSConfig) (*iamra.CredentialProcessOutput, error) {
	credentialsOptions := iamra.CredentialsOpts{
		PrivateKeyId:      awsConfig.ClientKeyPath,
		CertificateId:     awsConfig.ClientCertPath,
		RoleArn:           awsConfig.RoleARN,
		ProfileArnStr:     awsConfig.ProfileARN,
		TrustAnchorArnStr: awsConfig.TrustAnchorARN,
		SessionDuration:   3600 * 6,
		NoVerifySSL:       false,
		WithProxy:         false,
		Debug:             false,
	}

	if awsConfig.ClientBundlePath != "" {
		credentialsOptions.CertificateBundleId = awsConfig.ClientBundlePath
	}

	credentialProcessOutput, err := iamra.GenerateCredentials(&credentialsOptions)
	if err != nil {
		log.Err(err).Msg("unable to authenticate against AWS IAM Roles Anywhere")
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
	return opts.AWSRoleARN != "" && opts.AWSTrustAnchorARN != "" && opts.AWSProfileARN != "" && opts.AWSRegion != ""
}
