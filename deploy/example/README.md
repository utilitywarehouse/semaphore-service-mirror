Example deployment using kustomize base.

The example under this directory deploys:
- A service account under `kube-system` namespace, which should be used from
  remote service-mirror controllers to gain access to local cluster resources.
  The user will need to grab the token generated for the service account and
  feed it as a secret to remote deployments.
- The controller deployment under any namespace for a single controller watching
  a single remote cluster for mirrors.
