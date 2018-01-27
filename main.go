package k8s_docker_acr_autologin

import (
	"os"
	"fmt"
	"encoding/json"
	"github.com/Azure/azure-sdk-for-go/arm/containerregistry"
	"k8s.io/kubernetes/pkg/credentialprovider"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	azureapi "github.com/Azure/go-autorest/autorest/azure"
)

func main() {
	azureTenant := os.Getenv("AZURE_TENANT")
	azureSubscription := os.Getenv("AZURE_SUBSCRIPTION")
	azureClient := os.Getenv("AZURE_CLIENT")
	azureClientSecret := os.Getenv("AZURE_CLIENT_SECRET")

	oauthConfig, err := adal.NewOAuthConfig(azureapi.PublicCloud.ActiveDirectoryEndpoint, azureTenant)
	if err != nil {
		panic(fmt.Sprintf("[ERROR] failed to get oauth config: %v", err))
	}

	servicePrincipalToken, err := adal.NewServicePrincipalToken(
		*oauthConfig,
		azureClient,
		azureClientSecret,
		azureapi.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		panic(fmt.Sprintf("[ERROR] failed to create service principal token: %v", err))
	}

	registryClient := containerregistry.NewRegistriesClient(azureSubscription)
	registryClient.BaseURI = azureapi.PublicCloud.ResourceManagerEndpoint
	registryClient.Authorizer = autorest.NewBearerAuthorizer(servicePrincipalToken)

	dockerConfig := credentialprovider.DockerConfigJson{}
	dockerConfig.Auths = credentialprovider.DockerConfig{}

	if resp, err := registryClient.List(); err == nil {
		if resp.Value != nil {
			for _, registry := range *(resp.Value) {
				entry := credentialprovider.DockerConfigEntry{}
				entry.Username = azureClient
				entry.Password = azureClientSecret

				dockerConfig.Auths[*(registry.LoginServer)] = entry
			}
		}
	} else {
		panic(fmt.Sprintf("[ERROR] failed to get acr list: %v", err))
	}

	if jsonData, err := json.Marshal(dockerConfig); err == nil {
		fmt.Println(string(jsonData))
	} else {
		panic(fmt.Sprintf("[ERROR] failed to create docker config: %v", err))
	}

}
