package main

import (
	"os"
	"fmt"
	"time"
	"errors"
	"io/ioutil"
	"encoding/json"
	"encoding/base64"
	flags "github.com/jessevdk/go-flags"
)

const (
	AutomaticRefreshPrematureSeconds = 600
)

var opts struct {
	DockerConfigPath    string   `           long:"docker-config"`
	Daemon              bool     `short:"d"  long:"daemon"`
	AzureTenant         string   `           long:"azure-tenant"`
	AzureSubscription   string   `           long:"azure-subscription"`
	AzureClient         string   `           long:"azure-client"`
	azureClientSecret   string
	k8sEnabled          bool
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
)

func initOpts() (err error) {
	//#######################
	// Daemon
	//#######################
	if opts.AutoRefresh != "" {
		if val, err := time.ParseDuration(opts.AutoRefresh); err == nil {
			opts.autoRefresh = val
		} else {
			FatalErrorMessage("unable to parse --refresh", err)
		}
	}

	//#######################
	// Azure
	//#######################
	if val := os.Getenv("AZURE_TENANT"); opts.AzureTenant == "" && val != "" {
		opts.AzureTenant = val
	}

	if val := os.Getenv("AZURE_SUBSCRIPTION"); opts.AzureSubscription == "" && val != "" {
		opts.AzureSubscription = val
	}

	if val := os.Getenv("AZURE_CLIENT"); opts.AzureClient == "" && val != "" {
		opts.AzureClient = val
	}

	if val := os.Getenv("AZURE_CLIENT_SECRET"); opts.azureClientSecret == "" && val != "" {
		opts.azureClientSecret = val
	}

	//#######################
	// K8S
	//#######################
	if val := os.Getenv("KUBERNETES_SECRET_NAMESPACE"); opts.K8sNamespace == "" && val != "" {
		opts.K8sNamespace = val
	}

	if val := os.Getenv("KUBERNETES_SECRET_NAME"); opts.K8sSecret == "" && val != "" {
		opts.K8sSecret = val
	}

	if val := os.Getenv("KUBERNETES_SECRET_FILENAME"); opts.K8sFilename == "" && val != "" {
		opts.K8sFilename = val
	}

	return
}

func validateOpts() (err error) {

	//#######################
	// Azure
	//#######################
	if opts.AzureTenant == "" {
		return errors.New("Azure tenant id empty (use either --azure-tenant or env var AZURE_TENANT)")
	}

	if opts.AzureSubscription == "" {
		return errors.New("Azure subscription id empty (use either --azure-subscription or env var AZURE_SUBSCRIPTION)")
	}

	if opts.AzureClient == "" {
		return errors.New("Azure client id empty (use either --azure-client or env var AZURE_CLIENT)")
	}

	if opts.azureClientSecret == "" {
		return errors.New("Azure client secret empty (use env var AZURE_CLIENT_SECRET)")
	}

	//#######################
	// K8S
	//#######################
	if opts.K8sNamespace != "" && opts.K8sSecret != "" && opts.K8sFilename != "" {
		opts.k8sEnabled = true
		if opts.K8sNamespace == "" {
			return errors.New("K8S secret namespace empty (use either --k8s-secret-namespace or env var KUBERNETES_SECRET_NAMESPACE)")
		}

		if opts.K8sSecret == "" {
			return errors.New("K8S secret name empty (use either --k8s-secret-name or env var KUBERNETES_SECRET_NAME)")
		}

		if opts.K8sSecret == "" {
			return errors.New("K8S secret name empty (use either --k8s-secret-filename or env var KUBERNETES_SECRET_FILENAME)")
		}
	}

	return
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

	if err := initOpts(); err != nil {
		FatalErrorMessage("unable to process arguments/env vars", err)
	}

	if err := validateOpts(); err != nil {
		FatalErrorMessage("unable to validate config", err)
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
	// create azure service principal adal token
	azureService := AzureService{
		TenantId: opts.AzureTenant,
		ClientId: opts.AzureClient,
		ClientSecret: opts.azureClientSecret,
	}
	_, err := azureService.CreateServicePrincipalToken()
	if err != nil {
		FatalErrorMessage("failed to create service principal token", err)
	}

	// init docker configuration
	dockerConfig := CreateDockerConfig()

	if resp, err := azureService.GetContainerRegistryList(opts.AzureSubscription); err == nil {
		if resp.Value != nil {
			for _, registry := range *(resp.Value) {
				acr := azureService.CreateContainerRegistryClient(*(registry.LoginServer))

				fmt.Println(fmt.Sprintf("Request RefreshToken for %s", acr.GetName()))

				acrToken, err := acr.FetchAcrToken()
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

				dockerConfig.Auths[acr.GetName()] = entry
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
		if opts.k8sEnabled {
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
