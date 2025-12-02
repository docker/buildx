package policy

import "time"

type Input struct {
	Env   Env    `json:"env,omitzero"`
	Local *Local `json:"local,omitempty"`
	Image *Image `json:"image,omitempty"`
	HTTP  *HTTP  `json:"http,omitempty"`
	Git   *Git   `json:"git,omitempty"`
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

	PGPSignature *PGPSignature `json:"PGPSignature,omitempty"`
	SSHSignature *SSHSignature `json:"SSHSignature,omitempty"`
}

type Tag struct {
	Object  string `json:"object,omitempty"`
	Type    string `json:"type,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Tagger  Actor  `json:"tagger,omitzero"`
	Message string `json:"message,omitempty"`

	PGPSignature *PGPSignature `json:"PGPSignature,omitempty"`
	SSHSignature *SSHSignature `json:"SSHSignature,omitempty"`
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
	Kind       string             `json:"kind,omitempty"`
	Timestamps []TrustedTimestamp `json:"timestamps,omitempty"`
	// CertificateSummary
}

type TrustedTimestamp struct {
	Tlog      bool      `json:"tlog,omitempty"`
	Timestamp time.Time `json:"timestamp,omitzero"`
}

type Local struct {
	Name string `json:"name,omitempty"`
}
