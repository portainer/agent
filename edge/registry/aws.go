package registry

import (
	"os"

	iamra "github.com/aws/rolesanywhere-credential-helper/aws_signing_helper"
	"github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
	"github.com/portainer/agent"
	"github.com/rs/zerolog/log"
)

func doAWSAuthAndRetrieveCredentials(serverURL string, awsConfig *agent.AWSConfig) (*agent.RegistryCredentials, error) {
	err := authenticateAgainstIAMRA(awsConfig)
	if err != nil {
		return nil, err
	}

	factory := api.DefaultClientFactory{}
	client := factory.NewClientFromRegion(awsConfig.Region)

	creds, err := client.GetCredentials(serverURL)
	if err != nil {
		// This might not be an ECR registry
		// Therefore we deliberately not return an error here so that the logic can fallback to other credential providers
		log.Warn().Str("server_url", serverURL).Err(err).Msg("unable to retrieve credentials from server")
		return nil, nil
	}

	return &agent.RegistryCredentials{
		ServerURL: serverURL,
		Username:  creds.Username,
		Secret:    creds.Password,
	}, nil
}

func authenticateAgainstIAMRA(awsConfig *agent.AWSConfig) error {
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
		return err
	}

	// TODO AWS-IAM-ECR
	// We should investigate another approach to store these credentials
	// Maybe a custom credentials provider implementing the CredentialsProvider interface
	// see https://github.com/awslabs/amazon-ecr-credential-helper/blob/4177265fa425cca37d9c112d7c65024537e504e3/ecr-login/vendor/github.com/aws/aws-sdk-go-v2/aws/credentials.go#L119
	os.Setenv("AWS_ACCESS_KEY_ID", credentialProcessOutput.AccessKeyId)
	os.Setenv("AWS_SECRET_ACCESS_KEY", credentialProcessOutput.SecretAccessKey)
	os.Setenv("AWS_SESSION_TOKEN", credentialProcessOutput.SessionToken)

	return nil
}
