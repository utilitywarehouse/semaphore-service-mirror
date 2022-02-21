SHELL := /bin/bash

release:
	@sd "newTag: master" "newTag: $(VERSION)" deploy/kustomize/namespaced/kustomization.yaml
	@git add deploy/kustomize/namespaced/kustomization.yaml
	@git commit -m "Release $(VERSION)"
	@git tag -m "Release $(VERSION)" -a $(VERSION)
	@sd "newTag: $(VERSION)" "newTag: master" deploy/kustomize/namespaced/kustomization.yaml
	@git add deploy/kustomize/namespaced/kustomization.yaml
	@git commit -m "Clean up release $(VERSION)"
