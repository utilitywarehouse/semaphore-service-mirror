package semaphoremirrornamelength

test_ok {
  results := violation with input as {
    "parameters": {"prefixes": ["merit", "aws", "gcp"]},
    "review": {"object": {"metadata": {
      "name": "example",
      "namespace": "example-ns",
    }}},
  }

  count(results) == 0
}

test_violation {
  results := violation with input as {
    "parameters": {"prefixes": ["merit"]},
    "review": {"object": {"metadata": {
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
    "review": {"object": {"metadata": {
      "name": "too-long-with-merit-but-not-other-prefix",
      "namespace": "example-ns",
    }}},
  }

  count(results) == 1
}
