package semaphoremirrornamelength

# <prefix>-<namespace>-73736d-<name>
name_fmt := "%s-%s-73736d-%s"

violation[{"msg": msg}] {
  prefix := input.parameters.prefixes[_]
  name := input.review.object.metadata.name
  namespace := input.review.object.metadata.namespace

  mirror_name := sprintf(name_fmt, [prefix, namespace, name])
  mirror_name_len := count(mirror_name)
  mirror_name_len > 63

  msg := sprintf("The name of the generated mirror service must not exceed 63 characters length=%d name=%s", [mirror_name_len, mirror_name])
}
