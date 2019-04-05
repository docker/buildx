group "default" {
	targets = ["db", "webapp"]
}

group "release" {
	targets = ["db", "webapp-plus"]
}

target "db" {
	context = "./"
	tags = ["docker.io/tonistiigi/db"]
}

target "webapp" {
	context = "./"
	dockerfile = "Dockerfile.webapp"
	args = {
		buildno = "123"
	}
}

target "cross" {
	platforms = [
		"linux/amd64",
		"linux/arm64"
	]
}

target "webapp-plus" {
	inherits = ["webapp", "cross"]
	args = {
		IAMPLUS = "true"
	}
}