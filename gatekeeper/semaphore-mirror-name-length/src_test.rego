package semaphoremirrornamelength

test_ok {
  results := violation with input as {
    "parameters": {"prefixes": ["merit", "aws", "gcp"]},
    "review": {"operation": "CREATE", "object": {"metadata": {
      "name": "example",
      "namespace": "example-ns",
    }}},
  }

  count(results) == 0
}

test_ok_delete {
  results := violation with input as {
    "parameters": {"prefixes": ["merit"]},
    "review": {"operation": "DELETE", "object": {"metadata": {
      "name": "this-name-is-far-too-long",
      "namespace": "this-namespace-is-also-too-long",
    }}},
  }

  count(results) == 0
}

test_violation {
  results := violation with input as {
    "parameters": {"prefixes": ["merit"]},
    "review": {"operation": "CREATE", "object": {"metadata": {
      "name": "this-name-is-far-too-long",
      "namespace": "this-namespace-is-also-too-long",
    }}},
  }

  count(results) == 1
}

# test that a violation is produced when the generated mirror name is too long
# with one prefix but not the others
test_violation_with_longest_prefix {
  results := violation with input as {
    "parameters": {"prefixes": ["merit", "aws", "gcp"]},
    "review": {"operation": "CREATE", "object": {"metadata": {
      "name": "too-long-with-merit-but-not-other-prefix",
      "namespace": "example-ns",
    }}},
  }

  count(results) == 1
}

test_ok_label_missing {
  results := violation with input as {
    "parameters": {"matchLabels": {"foo": "bar", "baz": "foo"}, "prefixes": ["merit"]},
    "review": {"operation": "CREATE", "object": {"metadata": {
      "name": "this-name-is-far-too-long",
      "namespace": "this-namespace-is-also-too-long",
      "labels": {"foo": "bar"},
    }}},
  }

  count(results) == 0
}

test_ok_label_match {
  results := violation with input as {
    "parameters": {"matchLabels": {"foo": "bar", "baz": "foo"}, "prefixes": ["merit", "aws", "gcp"]},
    "review": {"operation": "CREATE", "object": {"metadata": {
      "name": "example",
      "namespace": "example-ns",
      "labels": {"foo": "bar", "baz": "foo"},
    }}},
  }

  count(results) == 0
}

test_ok_label_match_subset {
  results := violation with input as {
    "parameters": {"matchLabels": {"foo": "bar", "baz": "foo"}, "prefixes": ["merit", "aws", "gcp"]},
    "review": {"operation": "CREATE", "object": {"metadata": {
      "name": "example",
      "namespace": "example-ns",
      "labels": {"foo": "bar", "baz": "foo", "another": "label"},
    }}},
  }

  count(results) == 0
}

test_violation_label_match {
  results := violation with input as {
    "parameters": {"matchLabels": {"foo": "bar", "baz": "foo"}, "prefixes": ["merit"]},
    "review": {"operation": "CREATE", "object": {"metadata": {
      "name": "this-name-is-far-too-long",
      "namespace": "this-namespace-is-also-too-long",
      "labels": {"foo": "bar", "baz": "foo"},
    }}},
  }

  count(results) == 1
}

test_violation_label_match_subset {
  results := violation with input as {
    "parameters": {"matchLabels": {"foo": "bar", "baz": "foo"}, "prefixes": ["merit"]},
    "review": {"operation": "CREATE", "object": {"metadata": {
      "name": "this-name-is-far-too-long",
      "namespace": "this-namespace-is-also-too-long",
      "labels": {"foo": "bar", "baz": "foo", "another": "label"},
    }}},
  }

  count(results) == 1
}
