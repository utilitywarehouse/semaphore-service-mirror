This is an example manifest that contains the service account we need to deploy
on a remote cluster, which services we want to mirror locally, and the
respective permissions it needs to have. We can then grab the token from the
created kubernetes secrets to configure it in our local deployment.:
