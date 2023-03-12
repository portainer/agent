package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	iamra "github.com/aws/rolesanywhere-credential-helper/aws_signing_helper"
	"github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
	"github.com/portainer/agent"
	"github.com/portainer/agent/os"
	"github.com/rs/zerolog/log"
)

func DoAWSIAMRolesAnywhereAuthAndGetECRCredentials(serverURL string, awsConfig *agent.AWSConfig) (*agent.RegistryCredentials, error) {
	if serverURL == "" || awsConfig == nil {
		return nil, nil
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
		return nil, nil
	}

	return &agent.RegistryCredentials{
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
		SessionDuration:   3600,
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
	if os.IsValidAWSConfig(options) {
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
