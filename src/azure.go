package main

import (
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	azureapi "github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/azure-sdk-for-go/arm/containerregistry"
)

type AzureService struct {
	TenantId string
	ClientId string
	ClientSecret string
	servicePrincipalToken *adal.ServicePrincipalToken
}

func (a *AzureService) CreateServicePrincipalToken() (servicePrincipalToken *adal.ServicePrincipalToken, err error) {
	oauthConfig, err := adal.NewOAuthConfig(azureapi.PublicCloud.ActiveDirectoryEndpoint, a.TenantId)
	if err != nil {
		return
	}

	servicePrincipalToken, err = adal.NewServicePrincipalToken(
		*oauthConfig,
		a.ClientId,
		a.ClientSecret,
		azureapi.PublicCloud.ResourceManagerEndpoint,
	)

	a.servicePrincipalToken = servicePrincipalToken

	return
}

func (a *AzureService) GetContainerRegistryList(subscription string) (result containerregistry.RegistryListResult, err error) {
	registryClient := containerregistry.NewRegistriesClient(subscription)
	registryClient.BaseURI = azureapi.PublicCloud.ResourceManagerEndpoint
	registryClient.Authorizer = autorest.NewBearerAuthorizer(a.servicePrincipalToken)

	return registryClient.List()
}

func (a *AzureService) CreateContainerRegistryClient(acrName string) (acr *azureAcr) {
	acr = &azureAcr{}
	acr.loginServer = acrName
	acr.servicePrincipalToken = a.servicePrincipalToken
	acr.azureTenantId = a.TenantId
	return
}

