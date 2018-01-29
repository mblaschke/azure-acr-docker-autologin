# Azure Container Registry autologin (AAD refresh tokens)

[![Docker Automated build](https://img.shields.io/docker/automated/mblaschke/azure-acr-docker-autologin.svg)](https://hub.docker.com/r/mblaschke/azure-acr-docker-autologin/)

This daemon uses an service principal to create a `~/.docker/config.json`
with refresh tokens for all Azure Container Registries which are
available for this service prinicpal. These refresh tokens are normally
only valid for 1 hour so this daemon will refresh the file before the
timeout.

For Kubernetes there is already an integration but that only works from
the same subscription. If you want to have multiple clusters in different
subscription maybe this solution will fix this issue (until Azure fixes this).

## Standalone

```bash
export AZURE_TENANT=your_tenant_id
export AZURE_SUBSCRIPTION=your_subscription_id
export AZURE_CLIENT=your_service_principal_app_id
export AZURE_CLIENT_SECRET=your_service_principal_secret

cd src
go build -o main .
./main --daemon --docker-config=/home/youruser/.docker/config.json

```

## Kubernetes configuration

```bash
kubectl -n YOUR_NAMESPACE create serviceaccount docker-autologin
kubectl -n YOUR_NAMESPACE create rolebinding serviceaccount:docker-autologin --clusterrole=edit --serviceaccount=YOUR_NAMESPACE:docker-autologin
kubectl -n YOUR_NAMESPACE apply -f docker-autologin.yaml
````

docker-autologin.yaml:
```yaml
apiVersion: v1
kind: Secret
type: Opaque
metadata:
  name: docker
  annotations:
    helm.sh/resource-policy: keep
data:
  docker.conf: e30NCg==
---
apiVersion: v1
kind: Pod
metadata:
  name: master
spec:
  serviceAccount: docker-autologin
  containers:
  - name: docker-autologin
    image: mblaschke/azure-acr-autologin:latest
    args: ["/app/main", "--daemon"]
    env:
    - name: KUBERNETES_SECRET_NAMESPACE
      value: your-namespace
    - name: KUBERNETES_SECRET_NAME
      value: docker
    - name: KUBERNETES_SECRET_FILENAME
      value: docker.conf
    - name: AZURE_TENANT
      value: your tenant id
    - name: AZURE_SUBSCRIPTION
      value: your subscription id
    - name: AZURE_CLIENT
      value: your service principal app id
    - name: AZURE_CLIENT_SECRET
      value: your service principal secret
```
