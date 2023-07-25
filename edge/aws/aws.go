package aws

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	iamra "github.com/aws/rolesanywhere-credential-helper/aws_signing_helper"
	"github.com/portainer/agent"
	"github.com/rs/zerolog/log"
)

var ErrNoCredentials = errors.New("no credentials found")

func DoAWSIAMRolesAnywhereAuthAndGetECRCredentials(serverURL string, awsConfig *agent.AWSConfig) (*agent.RegistryCredentials, error) {
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

	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(awsConfig.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(iamraCreds.AccessKeyId, iamraCreds.SecretAccessKey, iamraCreds.SessionToken)),
	)
	if err != nil {
		log.Err(err).Msg("unable to build AWS client config")
		return nil, err
	}

	client := ecr.NewFromConfig(cfg)

	output, err := client.GetAuthorizationToken(context.TODO(), &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		log.Err(err).Msg("unable to get ECR authorization token")
		return nil, err
	}

	if len(output.AuthorizationData) == 0 {
		log.Err(err).Msg("unable to find ECR authorization token associated with the AWS account")
		return nil, errors.New("no ECR authorization token associated with AWS account")
	}

	data := output.AuthorizationData[0]

	token, err := base64.StdEncoding.DecodeString(*data.AuthorizationToken)
	if err != nil {
		log.Err(err).Msg("unable to decode ECR authorization token")
		return nil, err
	}

	tokenParts := strings.Split(string(token), ":")
	username := tokenParts[0]
	password := tokenParts[1]

	return &agent.RegistryCredentials{
		ServerURL: serverURL,
		Username:  username,
		Secret:    password,
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
