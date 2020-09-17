module github.com/docker/buildx

require (
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/bitly/go-hostpool v0.0.0-20171023180738-a3a6125de932 // indirect
	github.com/bugsnag/bugsnag-go v1.4.1 // indirect
	github.com/bugsnag/panicwrap v1.2.0 // indirect
	github.com/cenkalti/backoff v2.1.1+incompatible // indirect
	github.com/cloudflare/cfssl v0.0.0-20181213083726-b94e044bb51e // indirect
	github.com/containerd/console v1.0.0
	github.com/containerd/containerd v1.4.1-0.20200903181227-d4e78200d6da
	github.com/denisenkom/go-mssqldb v0.0.0-20190315220205-a8ed825ac853 // indirect
	github.com/docker/cli v0.0.0-20200911150641-2955ece02443
	github.com/docker/compose-on-kubernetes v0.4.19-0.20190128150448-356b2919c496 // indirect
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/docker v0.0.0
	github.com/docker/docker-credential-helpers v0.6.1 // indirect
	github.com/docker/go v1.5.1-1.0.20160303222718-d30aec9fd63c // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-metrics v0.0.1 // indirect
	github.com/docker/libtrust v0.0.0-20150526203908-9cbd2a1374f4 // indirect
	github.com/docker/spdystream v0.0.0-20181023171402-6480d4af844c // indirect
	github.com/elazarl/goproxy v0.0.0-20191011121108-aa519ddbe484 // indirect
	github.com/erikstmartin/go-testdb v0.0.0-20160219214506-8d10e4a1bae5 // indirect
	github.com/fvbommel/sortorder v1.0.1 // indirect
	github.com/go-sql-driver/mysql v1.4.1 // indirect
	github.com/gofrs/flock v0.7.3
	github.com/gofrs/uuid v3.2.0+incompatible // indirect
	github.com/google/certificate-transparency-go v1.0.21 // indirect
	github.com/google/shlex v0.0.0-20150127133951-6f45313302b9
	github.com/googleapis/gnostic v0.3.1 // indirect
	github.com/gophercloud/gophercloud v0.6.0 // indirect
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed // indirect
	github.com/hashicorp/hcl/v2 v2.6.0
	github.com/jinzhu/gorm v1.9.2 // indirect
	github.com/jinzhu/inflection v0.0.0-20180308033659-04140366298a // indirect
	github.com/jinzhu/now v1.0.0 // indirect
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/lib/pq v1.0.0 // indirect
	github.com/mattn/go-sqlite3 v1.10.0 // indirect
	github.com/miekg/pkcs11 v0.0.0-20190322140431-074fd7a1ed19 // indirect
	github.com/moby/buildkit v0.7.1-0.20200914033518-a2563079f719
	github.com/moby/term v0.0.0-20200911173544-4fc2018d01d9 // indirect
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.7.1 // indirect
	github.com/serialx/hashring v0.0.0-20190422032157-8b2912629002
	github.com/sirupsen/logrus v1.6.0
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.5.1
	github.com/theupdateframework/notary v0.6.1 // indirect
	github.com/tonistiigi/units v0.0.0-20180711220420-6950e57a87ea
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	github.com/zclconf/go-cty v1.4.0
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	gopkg.in/dancannon/gorethink.v3 v3.0.5 // indirect
	gopkg.in/fatih/pool.v2 v2.0.0 // indirect
	gopkg.in/gorethink/gorethink.v3 v3.0.5 // indirect
	k8s.io/api v0.16.7
	k8s.io/apimachinery v0.16.7
	k8s.io/client-go v0.16.7
)

replace github.com/docker/docker => github.com/docker/docker v17.12.0-ce-rc1.0.20200310163718-4634ce647cf2+incompatible

replace github.com/jaguilar/vt100 => github.com/tonistiigi/vt100 v0.0.0-20190402012908-ad4c4a574305

// protobuf: corresponds to containerd
replace github.com/golang/protobuf => github.com/golang/protobuf v1.3.5

// latest x/sys fails to build containerd/console
replace golang.org/x/sys => golang.org/x/sys v0.0.0-20200728102440-3e129f6d46b1

go 1.13
