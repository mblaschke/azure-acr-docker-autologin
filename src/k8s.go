package main

import (
	"os"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Kubernetes struct {
	clientset *kubernetes.Clientset
}

// Create cached kubernetes client
func (k *Kubernetes) Client() (clientset *kubernetes.Clientset) {
	var err error
	var config *rest.Config

	if k.clientset == nil {
		if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
			// KUBECONFIG
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				panic(err.Error())
			}
		} else {
			// K8S in cluster
			config, err = rest.InClusterConfig()
			if err != nil {
				panic(err.Error())
			}
		}

		k.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}
	}

	return k.clientset
}


func (k *Kubernetes) ApplySecret(namespace, secretName, filename string, content []byte) error {

	var secret v1.Secret
	secretExists := false

	option := v12.GetOptions{}
	if val, err := k.Client().CoreV1().Secrets(namespace).Get(secretName, option); err != nil {
		if val != nil {
			secret = val
			secretExists = true
		}
	} else {
		return err
	}

	secret.APIVersion = "v1"
	secret.Namespace = namespace
	secret.Name = secretName
	secret.Type = "Opaque"
	secret.Data = map[string][]byte{}
	secret.Data[filename] = content


	var err error
	if secretExists {
		_, err = k.Client().CoreV1().Secrets(namespace).Update(&secret)
	} else {
		_, err = k.Client().CoreV1().Secrets(namespace).Create(&secret)
	}

	return err
}
