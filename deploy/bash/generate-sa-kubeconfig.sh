#!/bin/bash

read -p "Enter Kube Api Server Address: " server
read -p "Enter Kube Context: " ctx
read -p "Enter Kube Service Account Namespace: " ns
read -p "Enter Kube Service Account Token Secret Name: " name

ca=$(kubectl --context=${ctx} --namespace=${ns} get secret/$name -o jsonpath='{.data.ca\.crt}')
token=$(kubectl --context=${ctx} --namespace=${ns} get secret/$name -o jsonpath='{.data.token}' | base64 --decode)

echo "
apiVersion: v1
kind: Config
clusters:
- name: ${CTX}
  cluster:
    certificate-authority-data: ${ca}
    server: ${server}
contexts:
- name: ${CTX}
  context:
    cluster: ${CTX}
    user: service-account
current-context: ${CTX}
users:
- name: service-account
  user:
    token: ${token}
"
