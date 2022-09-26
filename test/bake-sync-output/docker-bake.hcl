group "default" {
  targets = ["foo", "bar"]
}

target "foo" {
  dockerfile = "foo.Dockerfile"
  output = ["out"]
}

target "bar" {
  dockerfile = "bar.Dockerfile"
  output = ["out"]
}
