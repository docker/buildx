package workers

type backend struct {
	builder string
	context string
}

func (s *backend) Address() string {
	return s.builder
}

func (s *backend) DockerAddress() string {
	return s.context
}

func (s *backend) ContainerdAddress() string {
	return ""
}

func (s *backend) Snapshotter() string {
	return ""
}

func (s *backend) Rootless() bool {
	return false
}
