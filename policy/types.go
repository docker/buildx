package policy

import (
	"time"

	"github.com/moby/buildkit/util/gitutil/gitobject"
	policytypes "github.com/moby/policy-helpers/types"
)

type Input struct {
	Env   Env    `json:"env,omitzero"`
	Local *Local `json:"local,omitempty"`
	Image *Image `json:"image,omitempty"`
	HTTP  *HTTP  `json:"http,omitempty"`
	Git   *Git   `json:"git,omitempty"`
}

type Decision struct {
	Allow        *bool    `json:"allow,omitempty"`
	DenyMessages []string `json:"deny_msg,omitempty"`
}

type Env struct {
	Args     map[string]*string `json:"args,omitempty"`
	Labels   map[string]string  `json:"labels,omitempty"`
	Filename string             `json:"filename,omitempty"`
	Target   string             `json:"target,omitempty"`
}

type HTTP struct {
	URL     string              `json:"url,omitempty"`
	Schema  string              `json:"schema,omitempty"`
	Host    string              `json:"host,omitempty"`
	Path    string              `json:"path,omitempty"`
	Query   map[string][]string `json:"query,omitempty"`
	HasAuth bool                `json:"hasAuth,omitempty"`

	Checksum string `json:"checksum,omitempty"`

	Signature         *PGPSignature      `json:"signature,omitempty"`
	AttestationBundle *AttestationBundle `json:"attestationBundle,omitempty"`
}

type AttestationBundle struct{}

type Git struct {
	Schema      string `json:"schema,omitempty"`
	Host        string `json:"host,omitempty"`
	Remote      string `json:"remote,omitempty"`
	FullURL     string `json:"fullURL,omitempty"`
	TagName     string `json:"tagName,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Subdir      string `json:"subDir,omitempty"`
	IsCommitRef bool   `json:"isCommitRef,omitempty"`

	IsSHA256       bool   `json:"isSHA256,omitempty"`
	Checksum       string `json:"checksum,omitempty"`
	CommitChecksum string `json:"commitChecksum,omitempty"`
	IsAnnotatedTag bool   `json:"isAnnotatedTag,omitempty"`

	Tag    *Tag    `json:"tag,omitempty"`
	Commit *Commit `json:"commit,omitempty"`
}

type Actor struct {
	Name  string     `json:"name,omitempty"`
	Email string     `json:"email,omitempty"`
	When  *time.Time `json:"when,omitempty"`
}

type Commit struct {
	Tree      string   `json:"tree,omitempty"`
	Parents   []string `json:"parents,omitempty"`
	Author    Actor    `json:"author,omitzero"`
	Committer Actor    `json:"committer,omitzero"`
	Message   string   `json:"message,omitempty"`

	PGPSignature *PGPSignature `json:"pgpSignature,omitempty"`
	SSHSignature *SSHSignature `json:"sshSignature,omitempty"`

	obj *gitobject.GitObject
}

type Tag struct {
	Object  string `json:"object,omitempty"`
	Type    string `json:"type,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Tagger  Actor  `json:"tagger,omitzero"`
	Message string `json:"message,omitempty"`

	PGPSignature *PGPSignature `json:"pgpSignature,omitempty"`
	SSHSignature *SSHSignature `json:"sshSignature,omitempty"`

	obj *gitobject.GitObject
}

type PGPSignature struct {
	Version int    `json:"version,omitempty"`
	KeyID   string `json:"keyID,omitempty"`
}

type SSHSignature struct {
	Version int    `json:"version,omitempty"`
	PubKey  string `json:"pubKey,omitempty"`
}

type Image struct {
	Ref          string `json:"ref,omitempty"`
	Host         string `json:"host,omitempty"`
	Repo         string `json:"repo,omitempty"`
	FullRepo     string `json:"fullRepo,omitempty"` // domain + repo
	Tag          string `json:"tag,omitempty"`      // unset if canonical ref
	Platform     string `json:"platform,omitempty"`
	OS           string `json:"os,omitempty"`
	Architecture string `json:"arch,omitempty"`
	Variant      string `json:"variant,omitempty"`
	IsCanonical  bool   `json:"isCanonical,omitempty"`

	Checksum string `json:"checksum,omitempty"`

	// Config based
	CreatedTime string            `json:"createdTime,omitempty"`
	Env         []string          `json:"env,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	User        string            `json:"user,omitempty"`
	Volumes     []string          `json:"volumes,omitempty"`
	WorkingDir  string            `json:"workingDir,omitempty"`

	HasProvenance bool                   `json:"hasProvenance,omitempty"`
	Signatures    []AttestationSignature `json:"signatures,omitempty"`
}

type AttestationSignature struct {
	SignatureKind   SignatureKind                             `json:"kind,omitempty"`
	SignatureType   SignatureType                             `json:"type,omitempty"`
	Timestamps      []policytypes.TimestampVerificationResult `json:"timestamps,omitempty"`
	DockerReference string                                    `json:"dockerReference,omitempty"`
	IsDHI           bool                                      `json:"isDHI,omitempty"`
	Signer          *SignerInfo                               `json:"signer,omitempty"`

	raw *policytypes.SignatureInfo
}

type TrustedTimestamp struct {
	Tlog      bool      `json:"tlog,omitempty"`
	URI       string    `json:"uri,omitempty"`
	Timestamp time.Time `json:"timestamp,omitzero"`
}

type SignerInfo struct {
	// certificate.Summary with deprecated fields removed
	CertificateIssuer                   string `json:"certificateIssuer"`
	SubjectAlternativeName              string `json:"subjectAlternativeName"`
	Issuer                              string `json:"issuer,omitempty"`                              // OID 1.3.6.1.4.1.57264.1.8 and 1.3.6.1.4.1.57264.1.1 (Deprecated)
	BuildSignerURI                      string `json:"buildSignerURI,omitempty"`                      //nolint:tagliatelle // 1.3.6.1.4.1.57264.1.9
	BuildSignerDigest                   string `json:"buildSignerDigest,omitempty"`                   // 1.3.6.1.4.1.57264.1.10
	RunnerEnvironment                   string `json:"runnerEnvironment,omitempty"`                   // 1.3.6.1.4.1.57264.1.11
	SourceRepositoryURI                 string `json:"sourceRepositoryURI,omitempty"`                 //nolint:tagliatelle  // 1.3.6.1.4.1.57264.1.12
	SourceRepositoryDigest              string `json:"sourceRepositoryDigest,omitempty"`              // 1.3.6.1.4.1.57264.1.13
	SourceRepositoryRef                 string `json:"sourceRepositoryRef,omitempty"`                 // 1.3.6.1.4.1.57264.1.14
	SourceRepositoryIdentifier          string `json:"sourceRepositoryIdentifier,omitempty"`          // 1.3.6.1.4.1.57264.1.15
	SourceRepositoryOwnerURI            string `json:"sourceRepositoryOwnerURI,omitempty"`            //nolint:tagliatelle // 1.3.6.1.4.1.57264.1.16
	SourceRepositoryOwnerIdentifier     string `json:"sourceRepositoryOwnerIdentifier,omitempty"`     // 1.3.6.1.4.1.57264.1.17
	BuildConfigURI                      string `json:"buildConfigURI,omitempty"`                      //nolint:tagliatelle // 1.3.6.1.4.1.57264.1.18
	BuildConfigDigest                   string `json:"buildConfigDigest,omitempty"`                   // 1.3.6.1.4.1.57264.1.19
	BuildTrigger                        string `json:"buildTrigger,omitempty"`                        // 1.3.6.1.4.1.57264.1.20
	RunInvocationURI                    string `json:"runInvocationURI,omitempty"`                    //nolint:tagliatelle // 1.3.6.1.4.1.57264.1.21
	SourceRepositoryVisibilityAtSigning string `json:"sourceRepositoryVisibilityAtSigning,omitempty"` // 1.3.6.1.4.1.57264.1.22
}

type SignatureType string

const (
	SignatureTypeBundleV03       SignatureType = "bundle-v0.3"
	SignatureTypeSimpleSigningV1 SignatureType = "simplesigning-v1"
)

func toSignatureType(st policytypes.SignatureType) SignatureType {
	switch st {
	case policytypes.SignatureBundleV03:
		return SignatureTypeBundleV03
	case policytypes.SignatureSimpleSigningV1:
		return SignatureTypeSimpleSigningV1
	}
	return ""
}

type SignatureKind string

const (
	SignatureKindDockerGithubBuilder  SignatureKind = "docker-github-builder"
	SignatureKindDockerHardenedImage  SignatureKind = "docker-hardened-image"
	SignatureKindSelfSignedGithubRepo SignatureKind = "self-signed-github-repo"
	SignatureKindSelfSigned           SignatureKind = "self-signed"
	SignatureKindUntrusted            SignatureKind = "untrusted"
)

func toSignatureKind(k policytypes.Kind) SignatureKind {
	switch k {
	case policytypes.KindDockerGithubBuilder:
		return SignatureKindDockerGithubBuilder
	case policytypes.KindDockerHardenedImage:
		return SignatureKindDockerHardenedImage
	case policytypes.KindSelfSignedGithubRepo:
		return SignatureKindSelfSignedGithubRepo
	case policytypes.KindSelfSigned:
		return SignatureKindSelfSigned
	case policytypes.KindUntrusted:
		return SignatureKindUntrusted
	}
	return ""
}

type Local struct {
	Name string `json:"name,omitempty"`
}
