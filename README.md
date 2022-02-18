# semaphore-service-mirror

Small app that watches for kubernetes services and endpoints in a target cluster
and mirrors them in a local namespace. Can be used in conjunction with coredns
in cases where pod networks are reachable between clusters and one needs to be
able to route virtual services for remote pods.

## Usage
```
Usage of ./semaphore-service-mirror:
  -config string
        (required)Path to the json config file
  -kube-config string
        Path of a kube config file, if not provided the app will try to get in cluster config
  -label-selector string
        Label of services and endpoints to watch and mirror
  -log-level string
        Log level (default "info")
  -mirror-ns string
        The namespace to create dummy mirror services in
```

You can set most flags via envvars instead, format: "SSM_FLAG_NAME". Example:
`-config` can be set as `SSM_CONFIG` and `-kube-config` can be set as 
`SSM_KUBE_CONFIG`.

If both are present, flags take precedence over envvars.

The only mandatory flag is `-config` to point to a json formatted config file.
Label selector and mirror namespace must also be set, but there is the option to
do this via the json config (more details in the next section below). Flags will
take precedence over static configuration from the file.

## Configuration file

The operator expects a configuration file in json format. Here is a description
of the configuration keys by scope:

### Global
Contains configuration globally shared by all runners.

* `globalSvcLabelSelector`: Labels used to select global services
* `globalSvcRoutingStrategyLabel`: Labels used to instruct controller to try
   utilising Kubernetes topology aware hints to select local cluster targets
   first when routing global services.
* `mirrorSvcLabelSelector`: Label used to select services to mirror
* `mirrorNamespace`: Namespace used to locate/place mirrored objects
* `serviceSync`: Whether to sync services on startup and delete records that
  cannot be located based on the label selector. Defaults to false

### Local Cluster
Contains configuration needed to manage resources in the local cluster, where
this operator runs.

* `name`: A name for the local cluster
* `zones`: A list of the availability zones for the local cluster. This will be
  used to allow topology aware routing for global services and the values should
  derive from kuberenetes nodes' `topology.kubernetes.io/zone` label.
* `kubeConfigPath`: Path to a kube config file to access the local cluster. If
  not specified the operator will try to use in-cluster configuration with the
  pod's service account.

### Remote clusters
Contains a list of keys to configure access to all remote cluster. Each list can
include the following:

* `name`: A name for the remote cluster
* `kubeConfigPath`: Path to a kube config file to access the remote cluster.
* `remoteAPIURL`: Address of the remote cluster API server
* `remoteCAURL`: Address from where to fetch the public CA certificate to talk
  to the remote API server.
* `remoteSATokenPiath`: Path to a service account token that will be used to
  access remote cluster resources.
* `resyncPeriod`: Will trigger an `onUpdate` event for everything that is stored
   in the respective watchers cache. Defaults to 0 which equals disabled. 
* `servicePrefix`: How to prefix service names mirrored from that remote 
  locally.

Either `kubeConfigPath` or `remoteAPIURL`,`remoteCAURL` and `remoteSATokenPiath`
should be set to be able to successfully create a client to talk to the remote
cluster.

### Example
```
{
  "global": {
    "globalSvcLabelSelector": "mirror.semaphore.uw.io/global-service=true",
    "globalSvcRoutingStrategyLabel": "mirror.semaphore.uw.io/global-service-routing-strategy=local-first",
    "mirrorSvcLabelSelector": "mirror.semaphore.uw.io/mirror-service=true",
    "mirrorNamespace": "semaphore",
    "serviceSync": true
  },
  "localCluster": {
    "name": "local",
    "kubeConfigPath": "/path/to/local/kubeconfig"
  },
  "remoteClusters": [
    {
      "name": "clusterA",
      "remoteCAURL": "remote_ca_url",
      "remoteAPIURL": "remote_api_url",
      "remoteSATokenPath": "/path/to/token",
      "resyncPeriod": "10s",
      "servicePrefix": "cluster-A"
    }
  ]
}
```

## Generating mirrored service names

In order to make regex matching easier for dns rewrite purposes (see coredns
example bellow) we use a hardcoded separator between service names and
namespaces on the generated name for mirrored service: `73736d`.

The format of the generated name is: `<prefix>-<namespace>-73736d-<name>`.

It's possible for this name to exceed the 63 character limit imposed by Kubernetes.
To guard against this, a Gatekeeper constraint template is provided in
[`gatekeeper/semaphore-mirror-name-length`](gatekeeper/semaphore-mirror-name-length)
which can be used to validate that remote services won't produce mirror names longer
than the limit.

Refer to the [example](gatekeeper/semaphore-mirror-name-length/example.yaml).

## Coredns config example

To create a smoother experience when accessing a service coredns can be
configured using the `rewrite` functionality:
```
cluster.example {
    errors
    health
    rewrite continue {
      name regex ([a-zA-Z0-9-_]*\.)?([a-zA-Z0-9-_]*)\.([a-zv0-9-_]*)\.svc\.cluster\.example {1}example-{3}-73736d-{2}.<namespace>.svc.cluster.local
      answer name ([a-zA-Z0-9-_]*\.)?example-([a-zA-Z0-9-_]*)-73736d-([a-zA-Z0-9-_]*)\.<namespace>\.svc\.cluster\.local {1}{3}.{2}.svc.cluster.example
    }
    kubernetes cluster.local in-addr.arpa ip6.arpa {
      pods insecure
      endpoint_pod_names
      upstream
      fallthrough in-addr.arpa ip6.arpa
    }
    forward . /etc/resolv.conf
    cache 30
    loop
    reload
    loadbalance
}
.:53 {
    errors
    health
    kubernetes cluster.local in-addr.arpa ip6.arpa {
      pods insecure
      endpoint_pod_names
      upstream
      fallthrough in-addr.arpa ip6.arpa
    }

    prometheus :9153
    forward . /etc/resolv.conf
    cache 30
    loop
    reload
    loadbalance
}
```
that way all queries for services under domain cluster.target will be rewritten
to match services on the local namespace that the services are mirrored.

* note that `<target>` and `<namespace>` should be replaced with a name for the
target cluster and the local namespace that contains the mirrored services.
* note that the above example assumes that you are running the mirroring service
with a prefix flag that matches the target cluster name.

## Global Services

The operator is also watching services based on a separate label, in order to
create global services. A global service will gather endpoints from multiple
remote clusters that live under the "same" namespace and name, into a single 
ocal service with endpoints in multiple clusters. For that purpose, it will
create a single ClusterIP (or headless) service and mirror endpointslices from
remote clusters to target the new "global" service.

The format of the name used for the global service is:
`gl-<namespace>-73736d-<name>`.

For example, if we have the following services:
- cluster: cA, namespace: example-ns, name: my-svc, endpoints: [eA]
- cluster: cB, namespace: example-ns, name: my-svc, endpoints: [eB1, eB2]
The operator will create a global service under the local "semaphore" namespace
with a corresponding list of endpoints: [eA, eB1, eB2].

* Global services will include endpoints from the local cluster as well,
  provided they are using the mirror label.
* Global services will try to utilise Kubernetes topology aware hints to route
  to local endpoints first.

### CoreDNS config for Global services

In order to be able to resolve the global services under `cluster.global`
domain, the following CoreDNS block is needed:
```
cluster.global {
    cache 30
    errors
    forward . /etc/resolv.conf
    kubernetes cluster.local {
        pods insecure
        endpoint_pod_names
    }
    loadbalance
    loop
    prometheus
    reload
    rewrite continue {
        name regex ([\w-]*\.)?([\w-]*)\.([\w-]*)\.svc\.cluster\.global {1}gl-{3}-73736d-{2}.sys-semaphore.svc.cluster.local
        answer name ([\w-]*\.)?gl-([\w-]*)-73736d-([\w-]*)\.sys-semaphore\.svc\.cluster\.local {1}{4}.{3}.svc.cluster.global
    }
}
```

### Topology routing

In some cases, it is preferable to route to endpoints which live closer to the
caller when addressing global services (first hit available endpoints in the
same cluster). For that purpose, one can use a label to instruct the controller
to set `service.kubernetes.io/topology-aware-hints=auto` label in the generated
global service and instruct Kubernetes to use topology hints for routing traffic
to the service. In order for the hints to be effective, the operator reads the
local configuration `zones` field and uses the list of zones defined there as
hints for local endpoints. If this is not set, a dummy value will be used and
topology aware routing will not be feasible. The operator also uses the dummy
"remote" zone value as a hint for endpoits mirrored from remote clusters, to
make sure that no routing decisions will be made on those and kube-proxy will
not complain about missing hints.
The label to enable the above is configurable via `globalSvcTopologyLabel` field
in the global configuration.

### Fungible values

Since service endpoints that will be involved in a global service come from
multiple services in different clusters, based on the service name and
namespace, certain parameters need to match across all those service
definitions. In particular, service ports and topology labels values are
fungible and if their values differ between definitions of services that feed
endpoints to the same global service, there will be a race between services to
force their attributes to the global service. For a predictable behaviour, make
sure that ports match between services and either all or none set the topology
label.
