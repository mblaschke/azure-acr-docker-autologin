package main

import (
	"os"
	"fmt"
	"encoding/json"
	"time"
	"io/ioutil"
	"encoding/base64"
	flags "github.com/jessevdk/go-flags"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	azureapi "github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/azure-sdk-for-go/arm/containerregistry"
)

const (
	AutomaticRefreshPrematureSeconds = 600
)

var opts struct {
	DockerConfigPath    string   `           long:"docker-config"`
	Daemon              bool     `short:"d"  long:"daemon"`
	K8sNamespace        string   `           long:"k8s-secret-namespace"`
	K8sSecret           string   `           long:"k8s-secret-name"`
	K8sFilename         string   `           long:"k8s-secret-filename"`
	AutoRefresh         string   `           long:"refresh"`
	autoRefresh         time.Duration
	autoRefreshNextTime int64
}

var (
	argparser *flags.Parser
	args []string
	k8sService = Kubernetes{}

	azureTenant = os.Getenv("AZURE_TENANT")
	azureSubscription = os.Getenv("AZURE_SUBSCRIPTION")
	azureClient = os.Getenv("AZURE_CLIENT")
	azureClientSecret = os.Getenv("AZURE_CLIENT_SECRET")
)

type DockerConfigEntry struct {
	Auth string          `json:"auth"`
	Identitytoken string `json:"identitytoken"`
}

type DockerConfig struct {
	Auths map[string]DockerConfigEntry `json:"auths"`
}

func main() {
	var err error
	argparser = flags.NewParser(&opts, flags.Default)
	args, err = argparser.Parse()

	// check if there is an parse error
	if err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			fmt.Println()
			argparser.WriteHelp(os.Stdout)
			os.Exit(1)
		}
	}

	if val := os.Getenv("KUBERNETES_SECRET_NAMESPACE"); val != "" {
		opts.K8sNamespace = val
	}

	if val := os.Getenv("KUBERNETES_SECRET_NAME"); val != "" {
		opts.K8sSecret = val
	}

	if val := os.Getenv("KUBERNETES_SECRET_FILENAME"); val != "" {
		opts.K8sFilename = val
	}

	if opts.AutoRefresh != "" {
		if val, err := time.ParseDuration(opts.AutoRefresh); err == nil {
			opts.autoRefresh = val
		} else {
			FatalErrorMessage("unable to parse --refresh", err)
		}
	}

	if opts.Daemon {
		for {
			opts.autoRefreshNextTime = 0
			updateDockerConfig()

			nextUpdateUnix := opts.autoRefreshNextTime - AutomaticRefreshPrematureSeconds
			waitUntil := time.Unix(nextUpdateUnix, 0)
			duration := time.Until(waitUntil)
			fmt.Println(fmt.Sprintf("Sleeping for %.2f minutes (%v)", duration.Minutes(), waitUntil.String()))
			time.Sleep(duration)
		}
	} else {
		updateDockerConfig()
	}
}

func updateDockerConfig() {
	oauthConfig, err := adal.NewOAuthConfig(azureapi.PublicCloud.ActiveDirectoryEndpoint, azureTenant)
	if err != nil {
		FatalErrorMessage("failed to get oauth config", err)
	}

	servicePrincipalToken, err := adal.NewServicePrincipalToken(
		*oauthConfig,
		azureClient,
		azureClientSecret,
		azureapi.PublicCloud.ResourceManagerEndpoint,
	)
	if err != nil {
		FatalErrorMessage("failed to create service principal token", err)
	}

	servicePrincipalToken.SetRefreshWithin(time.Duration(1)*time.Hour)
	servicePrincipalToken.Refresh()

	registryClient := containerregistry.NewRegistriesClient(azureSubscription)
	registryClient.BaseURI = azureapi.PublicCloud.ResourceManagerEndpoint
	registryClient.Authorizer = autorest.NewBearerAuthorizer(servicePrincipalToken)

	dockerConfig := DockerConfig{}
	dockerConfig.Auths = map[string]DockerConfigEntry{}

	if resp, err := registryClient.List(); err == nil {
		if resp.Value != nil {
			for _, registry := range *(resp.Value) {
				acrServer := *(registry.LoginServer)

				fmt.Println(fmt.Sprintf("Request RefreshToken for %s", acrServer))

				acrToken, err := fetchAcrToken(acrServer, azureTenant, servicePrincipalToken.AccessToken)
				if err != nil {
					ErrorMessage("failed to fetch acr refresh token", err)
					continue
				}

				// auto calc autorefresh
				if opts.AutoRefresh == "" {
					acrParsedToken, _ := parseAcrToken(acrToken)
					nextUpdateTime := acrParsedToken.Expiration
					if opts.autoRefreshNextTime == 0 || nextUpdateTime <= opts.autoRefreshNextTime {
						opts.autoRefreshNextTime = nextUpdateTime
					}
				}

				// Add to docker
				entry := DockerConfigEntry{}
				entry.Auth = base64.StdEncoding.EncodeToString([]byte("00000000-0000-0000-0000-000000000000:"))
				entry.Identitytoken = acrToken

				dockerConfig.Auths[acrServer] = entry
			}
		}
	} else {
		panic(fmt.Sprintf("[ERROR] failed to get acr list: %v", err))
	}

	// Update docker config
	if jsonData, err := json.MarshalIndent(dockerConfig, "", "  "); err == nil {
		// update local file
		if opts.DockerConfigPath != "" {
			fmt.Println(fmt.Sprintf("Updating docker config %s", opts.DockerConfigPath))
			if err := ioutil.WriteFile(opts.DockerConfigPath, jsonData, 0600); err != nil {
				ErrorMessage("Unable to write docker file", err)
			}
		}

		// Update k8s secret
		if opts.K8sNamespace != "" && opts.K8sSecret != "" && opts.K8sFilename != "" {
			fmt.Println(fmt.Sprintf("Updating k8s secret %s:%s", opts.K8sNamespace, opts.K8sSecret))
			if err := k8sService.ApplySecret(opts.K8sNamespace, opts.K8sSecret, opts.K8sFilename, jsonData); err != nil {
				ErrorMessage("Unable to update k8 ssecret", err)
			}
		}
	} else {
		FatalErrorMessage("failed to create docker config", err)
	}
}

func FatalErrorMessage(msg string, err error) {
	panic(fmt.Sprintf("[FATAL] %v: %v\n", msg, err))
}

func ErrorMessage(msg string, err error) {
	fmt.Errorf("[ERROR] %v: %v\n", msg, err)
}
