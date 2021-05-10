package semaphoremirrornamelength

# <prefix>-<namespace>-73736d-<name>
name_fmt := "%s-%s-73736d-%s"

# always match if matchLabels is nil
match_labels {
  not input.parameters.matchLabels
}

# ensure that every key=value pair in matchLabels is also in metadata.labels
match_labels {
  count(input.parameters.matchLabels) == count({label_value | label_value := input.parameters.matchLabels[label_key]; has_label(label_key, label_value)})
}

# check that label_key=label_value is in metadata.labels
has_label(label_key, label_value) {
  input.review.object.metadata.labels[label_key] == label_value
}

violation[{"msg": msg}] {
  input.review.operation != "DELETE"

  match_labels

  prefix := input.parameters.prefixes[_]
  name := input.review.object.metadata.name
  namespace := input.review.object.metadata.namespace

  mirror_name := sprintf(name_fmt, [prefix, namespace, name])
  mirror_name_len := count(mirror_name)
  mirror_name_len > 63

  msg := sprintf("The name of the generated mirror service must not exceed 63 characters length=%d name=%s", [mirror_name_len, mirror_name])
}
