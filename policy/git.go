package policy

import (
	"fmt"

	"github.com/moby/buildkit/util/gitutil/gitobject"
	"github.com/moby/buildkit/util/gitutil/gitsign"
	"golang.org/x/crypto/ssh"
)

type gitSignature struct {
	PGPSignature *PGPSignature
	SSHSignature *SSHSignature
}

func parseGitSignature(obj *gitobject.GitObject) *gitSignature {
	if obj.Signature == "" {
		return &gitSignature{}
	}
	sig, err := gitsign.ParseSignature([]byte(obj.Signature))
	if err != nil {
		return &gitSignature{}
	}
	out := &gitSignature{}
	if s := sig.PGPSignature; s != nil {
		out.PGPSignature = &PGPSignature{
			Version: s.Version,
		}
		if s.IssuerKeyId != nil {
			ki := *s.IssuerKeyId
			// convert uint64 to hex string
			out.PGPSignature.KeyID = fmt.Sprintf("%016x", ki)
		}
	}
	if s := sig.SSHSignature; s != nil {
		out.SSHSignature = &SSHSignature{
			Version: int(s.Version),
			PubKey:  ssh.FingerprintSHA256(s.PublicKey),
		}
	}
	return out
}
