package stack

import (
	"os"
	"os/exec"
	"runtime"

	iamra "github.com/aws/rolesanywhere-credential-helper/aws_signing_helper"
	"github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
	"github.com/docker/cli/cli/compose/loader"
	"github.com/docker/cli/cli/compose/types"
	"github.com/docker/distribution/reference"
	"github.com/portainer/agent"
	"github.com/rs/zerolog/log"
)

func doAWSLogout() error {
	cmd := exec.Command("docker", "logout")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("docker.exe", "logout")
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		log.Err(err).Msg("unable to execute docker logout command")
		return err
	}

	return nil
}

func doAWSAuthentication(composeFilePath string, awsConfig *agent.AWSConfig) error {
	// First, we retrieve the list of images used in the Compose file
	images, err := extractImagesFromComposeFile(composeFilePath)
	if err != nil {
		return err
	}

	// Then, for each image we retrieve a list of unique server URLs
	servers := extractServerURLsFromImage(images)
	if len(servers) == 0 {
		log.Info().Msg("no server URLs found in Compose file")
		return nil
	}

	// After that, we authenticate against the AWS IAM Roles Anywhere service
	err = authenticateAgainstIAMRA(awsConfig)
	if err != nil {
		return err
	}

	// Finally, we authenticate against each server URL

	factory := api.DefaultClientFactory{}
	client := factory.NewClientFromRegion(awsConfig.Region)

	for _, server := range servers {
		// TODO AWS-IAM-ECR
		// We should probably find a way to filter non ECR registry URLs from that list
		err = authenticateAgainstServer(server, client, awsConfig)
		if err != nil {
			// In case an error happens, we continue and try other servers
			// Can be revised after filtering the list of servers to ECR registries only
			continue
		}

	}

	return nil
}

func authenticateAgainstServer(serverURL string, client api.Client, awsConfig *agent.AWSConfig) error {
	creds, err := client.GetCredentials(serverURL)
	if err != nil {
		// This might not be an ECR registry
		// Therefore we just warn and exit
		log.Warn().Str("server_url", serverURL).Err(err).Msg("unable to retrieve credentials from server")
		return err
	}

	cmd := exec.Command("docker", "login", "-u", creds.Username, "-p", creds.Password, serverURL)
	if runtime.GOOS == "windows" {
		cmd = exec.Command("docker.exe", "login", "-u", creds.Username, "-p", creds.Password, serverURL)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		log.Err(err).Msg("unable to execute docker login command")
		return err
	}

	return nil
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

func extractServerURLsFromImage(images []string) []string {
	uniqServers := map[string]bool{}

	for _, image := range images {
		ref, err := reference.ParseNamed(image)
		if err != nil {
			log.Warn().Str("image", image).Err(err).Msg("unable to parse image name")
			continue
		}

		uniqServers[reference.Domain(ref)] = true
	}

	servers := []string{}
	for server := range uniqServers {
		servers = append(servers, server)
	}

	return servers
}

func extractImagesFromComposeFile(composeFilePath string) ([]string, error) {
	images := []string{}

	composeData, err := os.ReadFile(composeFilePath)
	if err != nil {
		log.Error().Str("compose_file_path", composeFilePath).Err(err).Msg("unable to read Compose file from path")
		return images, err
	}

	composeConfigYAML, err := loader.ParseYAML(composeData)
	if err != nil {
		log.Error().Err(err).Msg("unable to parse YML from Compose file data")
		return images, err
	}

	composeConfigFile := types.ConfigFile{
		Config: composeConfigYAML,
	}

	composeConfigDetails := types.ConfigDetails{
		ConfigFiles: []types.ConfigFile{composeConfigFile},
		Environment: map[string]string{},
	}

	composeConfig, err := loader.Load(composeConfigDetails, func(options *loader.Options) {
		options.SkipValidation = true
		options.SkipInterpolation = true
	})
	if err != nil {
		log.Error().Err(err).Msg("unable to create Compose configuration from YAML")
		return images, err
	}

	for key := range composeConfig.Services {
		service := composeConfig.Services[key]
		images = append(images, service.Image)
	}

	return images, nil
}
