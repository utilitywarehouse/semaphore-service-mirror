# kube-service-mirror

Small app that watches for kubernetes services and endpoints in a target cluster
and mirrors them in a local namespace. Can be used in conjunction with coredns
in cases where pod networks are reachable between clusters and one needs to be
able to route virtual services for remote pods.

## Usage
```
Usage of kube-service-mirror:
  -kube-config string
        Path of a kube config file, if not provided the app will try to get in cluster config
  -label-selector string
        (required) Label of services and endpoints to watch and mirror
  -log-level string
        Log level (default "info")
  -mirror-ns string
        The namespace to create dummy mirror services in
  -remote-api-url string
        Remote Kubernetes API server URL
  -remote-ca-url string
        Remote Kubernetes CA certificate URL
  -remote-sa-token-path string
        Remote Kubernetes cluster token path
  -resync-period duration
        Namespace watcher cache resync period (default 1h0m0s)
  -svc-prefix string
        (required) A prefix to apply on all mirrored services names. Will also be used for initial service sync
  -svc-sync
        Sync services on startup (default true)
  -target-kube-config string
        Path of the target cluster kube config file to mirrot services from
```

* `-svc-prefix` flag is non optional and has 2 different uses:
  - It is used as a prefix for your mirrored services, so that you can mirror
    the same ns/service from different clusters.
  - It is used to label your mirrored services as:
    `mirror-svc-prefix-sync: <value>`, so that the app can filter out which
    services to delete on the initial sync on startup.

You can set most flags via envvars instead, format: "KSM_FLAG_NAME". Example:
`-remote-ca-url` can be set as `KSM_REMOTE_CA_URL`.

If both are present, flags take precedence over envvars.

Only exception is `-remote-sa-token-path` flag and
`KSM_REMOTE_SERVICE_ACCOUNT_TOKEN` envvar.

## Generating mirrored service names

In order to make regex matching easier for dns rewrite purposes (see coredns
example bellow) we use a hardcoded separator between service names and
namespaces on the generated name for mirrored service.
We picked that to be `6d6972726f720a` from the hexdump of `mirror`:

```
$ echo mirror | xxd -p
6d6972726f720a
```

## Coredns config example

To create a smoother experience when accessing a service coredns can be
configured using the `rewrite` functionality:
```
cluster.<target> {
    errors
    health
    rewrite continue {
            name regex ([a-zA-Z0-9-_]*)\.([a-zv0-9-_]*)\.svc\.cluster\.<target> <target>-{1}-6d6972726f720a-{2}.<namespace>.svc.cluster.local
            answer name <target>-([a-zA-Z0-9-_]*)-6d6972726f720a-([a-zA-Z0-9-_]*)\.<namespace>\.svc\.cluster\.local {1}.{2}.svc.cluster.<target>
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
