package main

import (
	"os"
	"fmt"
	"time"
	"sync"
	"errors"
	"io/ioutil"
	"encoding/json"
	"encoding/base64"
	flags "github.com/jessevdk/go-flags"
)

var opts struct {
	DockerConfigPath    string   `           long:"docker-config"                        env:"DOCKER_CONFIG_DESTINATION"`
	DaemonMode          bool     `short:"d"  long:"daemon"                               env:"DAEMON_MODE"`
	AzureTenant         string   `           long:"azure-tenant"                         env:"AZURE_TENANT"                       required:"true"`
	AzureSubscription   []string `           long:"azure-subscription"                   env:"AZURE_SUBSCRIPTION"                 required:"true"`
	AzureClient         string   `           long:"azure-client"                         env:"AZURE_CLIENT"                       required:"true"`
	AzureClientSecret   string   `           long:"azure-client-secret"                  env:"AZURE_CLIENT_SECRET" env-delim:" "  required:"true"`
	k8sEnabled          bool
	K8sNamespace        string   `           long:"k8s-secret-namespace"                 env:"KUBERNETES_SECRET_NAMESPACE"`
	K8sSecret           string   `           long:"k8s-secret-name"                      env:"KUBERNETES_SECRET_NAME"`
	K8sFilename         string   `           long:"k8s-secret-filename"                  env:"KUBERNETES_SECRET_FILENAME"`
	AutoRefresh         string   `           long:"refresh"                              env:"AUTO_REFRESH"`
	AutoRefreshAdvance  int64    `           long:"refresh-advance"        default:"10"  env:"AUTO_REFRESH_ADVANCE"`
	autoRefresh         time.Duration
	autoRefreshNextTime int64
}

var (
	argparser *flags.Parser
	args []string
	k8sService = Kubernetes{}
	Logger *DaemonLogger
	ErrorLogger *DaemonLogger
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

	return
}

func validateOpts() (err error) {
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

	// Init logger
	Logger = CreateDaemonLogger(0)
	ErrorLogger = CreateDaemonErrorLogger(0)

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

	if opts.DaemonMode {
		for {
			opts.autoRefreshNextTime = 0
			updateDockerConfig()

			if opts.autoRefreshNextTime <= time.Now().Unix() {
				opts.autoRefreshNextTime = time.Now().Unix() + 10 * 60
			}

			nextUpdateUnix := opts.autoRefreshNextTime - (opts.AutoRefreshAdvance * 60)
			waitUntil := time.Unix(nextUpdateUnix, 0)
			duration := time.Until(waitUntil)
			Logger.Println(fmt.Sprintf("Sleeping for %.2f minutes (%v)", duration.Minutes(), waitUntil.String()))
			time.Sleep(duration)
		}
	} else {
		updateDockerConfig()
	}
}

func updateDockerConfig() {
	var wg sync.WaitGroup
	var wgMain sync.WaitGroup

	// create azure service principal adal token
	azureService := AzureService{
		TenantId: opts.AzureTenant,
		ClientId: opts.AzureClient,
		ClientSecret: opts.AzureClientSecret,
	}

	Logger.Println(fmt.Sprintf("Request ServicePrincipal token"))
	_, err := azureService.CreateServicePrincipalToken()
	if err != nil {
		FatalErrorMessage("failed to create service principal token", err)
	}

	channel := make(chan DockerConfigEntry)

	for _, azureSubscription := range opts.AzureSubscription {
		if resp, err := azureService.GetContainerRegistryList(azureSubscription); err == nil {
			if resp.Value != nil {
				for _, registry := range *(resp.Value) {
					acr := azureService.CreateContainerRegistryClient(*(registry.LoginServer))
					wg.Add(1)

					go func(acr *azureAcr) {
						defer wg.Done()

						Logger.Println(fmt.Sprintf("Requesting RefreshToken for %s", acr.GetName()))

						acrToken, err := acr.FetchAcrToken()
						if err != nil {
							ErrorLogger.Error("failed to fetch acr refresh token", err)
							return
						}

						// calc valid time
						acrParsedToken, _ := parseAcrToken(acrToken)
						validUntil := acrParsedToken.Expiration

						// build entry
						entry := DockerConfigEntry{}
						entry.Server = acr.GetName()
						entry.Auth = base64.StdEncoding.EncodeToString([]byte("00000000-0000-0000-0000-000000000000:"))
						entry.Identitytoken = acrToken
						entry.ValidUntil = validUntil

						channel <- entry
						return
					}(acr)
				}
			}
		} else {
			FatalErrorMessage("failed to get acr list", err)
		}
	}

	wgMain.Add(1)
	go func() {
		defer wgMain.Done()

		// init docker configuration
		dockerConfig := CreateDockerConfig()

		for item := range channel {
			serverName := item.Server
			dockerConfig.Auths[serverName] = item
		}

		// Update docker config
		Logger.Println("Building configuration")
		if jsonData, err := json.MarshalIndent(dockerConfig, "", "  "); err == nil {
			// update local file
			if opts.DockerConfigPath != "" {
				fmt.Println(fmt.Sprintf("Updating docker config %s", opts.DockerConfigPath))
				if err := ioutil.WriteFile(opts.DockerConfigPath, jsonData, 0600); err != nil {
					ErrorLogger.Error("Unable to write docker file", err)
				}
			}

			// Update k8s secret
			if opts.k8sEnabled {
				fmt.Println(fmt.Sprintf("Updating k8s secret %s:%s", opts.K8sNamespace, opts.K8sSecret))
				if err := k8sService.ApplySecret(opts.K8sNamespace, opts.K8sSecret, opts.K8sFilename, jsonData); err != nil {
					ErrorLogger.Error("Unable to update k8s secret", err)
				}
			}
		} else {
			FatalErrorMessage("failed to create docker config", err)
		}

		// Calc auto refresh next time
		opts.autoRefreshNextTime = 0
		for _, entry := range dockerConfig.Auths {
			if opts.autoRefreshNextTime == 0 || entry.ValidUntil < opts.autoRefreshNextTime {
				opts.autoRefreshNextTime = entry.ValidUntil
			}
		}
	}()

	wg.Wait()
	close(channel)
	wgMain.Wait()
}

func FatalErrorMessage(msg string, err error) {
	panic(fmt.Sprintf("[FATAL] %v: %v\n", msg, err))
}
