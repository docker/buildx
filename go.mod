module github.com/docker/buildx

go 1.13

require (
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/bitly/go-hostpool v0.0.0-20171023180738-a3a6125de932 // indirect
	github.com/bugsnag/bugsnag-go v1.4.1 // indirect
	github.com/bugsnag/panicwrap v1.2.0 // indirect
	github.com/cenkalti/backoff v2.1.1+incompatible // indirect
	github.com/cloudflare/cfssl v0.0.0-20181213083726-b94e044bb51e // indirect
	github.com/compose-spec/compose-go v0.0.0-20210706130854-69459d4976b5
	github.com/containerd/console v1.0.2
	github.com/containerd/containerd v1.5.2
	github.com/denisenkom/go-mssqldb v0.0.0-20190315220205-a8ed825ac853 // indirect
	github.com/docker/cli v20.10.7+incompatible
	github.com/docker/compose-on-kubernetes v0.4.19-0.20190128150448-356b2919c496 // indirect
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/docker v20.10.7+incompatible
	github.com/docker/docker-credential-helpers v0.6.4-0.20210125172408-38bea2ce277a // indirect
	github.com/docker/go v1.5.1-1.0.20160303222718-d30aec9fd63c // indirect
	github.com/docker/libtrust v0.0.0-20150526203908-9cbd2a1374f4 // indirect
	github.com/docker/spdystream v0.0.0-20181023171402-6480d4af844c // indirect
	github.com/elazarl/goproxy v0.0.0-20191011121108-aa519ddbe484 // indirect
	github.com/erikstmartin/go-testdb v0.0.0-20160219214506-8d10e4a1bae5 // indirect
	github.com/fvbommel/sortorder v1.0.1 // indirect
	github.com/gofrs/flock v0.7.3
	github.com/gofrs/uuid v3.3.0+incompatible // indirect
	github.com/google/certificate-transparency-go v1.0.21 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed // indirect
	github.com/hashicorp/go-cty-funcs v0.0.0-20200930094925-2721b1e36840
	github.com/hashicorp/hcl/v2 v2.8.2
	github.com/jinzhu/gorm v1.9.2 // indirect
	github.com/jinzhu/inflection v0.0.0-20180308033659-04140366298a // indirect
	github.com/jinzhu/now v1.0.0 // indirect
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/lib/pq v1.10.0 // indirect
	github.com/mattn/go-sqlite3 v1.10.0 // indirect
	github.com/moby/buildkit v0.8.2-0.20210702160134-1a7543a10527
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.1
	github.com/pkg/errors v0.9.1
	github.com/serialx/hashring v0.0.0-20190422032157-8b2912629002
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	github.com/theupdateframework/notary v0.6.1 // indirect
	github.com/tonistiigi/units v0.0.0-20180711220420-6950e57a87ea
	github.com/zclconf/go-cty v1.7.1
	go.opentelemetry.io/otel v1.0.0-RC1
	go.opentelemetry.io/otel/trace v1.0.0-RC1
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	gopkg.in/dancannon/gorethink.v3 v3.0.5 // indirect
	gopkg.in/fatih/pool.v2 v2.0.0 // indirect
	gopkg.in/gorethink/gorethink.v3 v3.0.5 // indirect
	k8s.io/api v0.20.6
	k8s.io/apimachinery v0.20.6
	k8s.io/client-go v0.20.6
)

replace (
	github.com/docker/cli => github.com/docker/cli v20.10.3-0.20210702143511-f782d1355eff+incompatible
	github.com/docker/docker => github.com/docker/docker v20.10.3-0.20210609100121-ef4d47340142+incompatible
)
