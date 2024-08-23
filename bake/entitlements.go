package bake

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/containerd/console"
	"github.com/docker/buildx/build"
	"github.com/moby/buildkit/util/entitlements"
	"github.com/pkg/errors"
)

type EntitlementKey string

const (
	EntitlementKeyNetworkHost      EntitlementKey = "network.host"
	EntitlementKeySecurityInsecure EntitlementKey = "security.insecure"
	EntitlementKeyFSRead           EntitlementKey = "fs.read"
	EntitlementKeyFSWrite          EntitlementKey = "fs.write"
	EntitlementKeyFS               EntitlementKey = "fs"
	EntitlementKeyImagePush        EntitlementKey = "image.push"
	EntitlementKeyImageLoad        EntitlementKey = "image.load"
	EntitlementKeyImage            EntitlementKey = "image"
	EntitlementKeySSH              EntitlementKey = "ssh"
)

type EntitlementConf struct {
	NetworkHost      bool
	SecurityInsecure bool
	FSRead           []string
	FSWrite          []string
	ImagePush        []string
	ImageLoad        []string
	SSH              bool
}

func ParseEntitlements(in []string) (EntitlementConf, error) {
	var conf EntitlementConf
	for _, e := range in {
		switch e {
		case string(EntitlementKeyNetworkHost):
			conf.NetworkHost = true
		case string(EntitlementKeySecurityInsecure):
			conf.SecurityInsecure = true
		case string(EntitlementKeySSH):
			conf.SSH = true
		default:
			k, v, _ := strings.Cut(e, "=")
			switch k {
			case string(EntitlementKeyFSRead):
				conf.FSRead = append(conf.FSRead, v)
			case string(EntitlementKeyFSWrite):
				conf.FSWrite = append(conf.FSWrite, v)
			case string(EntitlementKeyFS):
				conf.FSRead = append(conf.FSRead, v)
				conf.FSWrite = append(conf.FSWrite, v)
			case string(EntitlementKeyImagePush):
				conf.ImagePush = append(conf.ImagePush, v)
			case string(EntitlementKeyImageLoad):
				conf.ImageLoad = append(conf.ImageLoad, v)
			case string(EntitlementKeyImage):
				conf.ImagePush = append(conf.ImagePush, v)
				conf.ImageLoad = append(conf.ImageLoad, v)
			default:
				return conf, errors.Errorf("uknown entitlement key %q", k)
			}

			// TODO: dedupe slices and parent paths
		}
	}
	return conf, nil
}

func (c EntitlementConf) Validate(m map[string]build.Options) (EntitlementConf, error) {
	var expected EntitlementConf

	for _, v := range m {
		if err := c.check(v, &expected); err != nil {
			return EntitlementConf{}, err
		}
	}

	return expected, nil
}

func (c EntitlementConf) check(bo build.Options, expected *EntitlementConf) error {
	for _, e := range bo.Allow {
		switch e {
		case entitlements.EntitlementNetworkHost:
			if !c.NetworkHost {
				expected.NetworkHost = true
			}
		case entitlements.EntitlementSecurityInsecure:
			if !c.SecurityInsecure {
				expected.SecurityInsecure = true
			}
		}
	}
	return nil
}

func (c EntitlementConf) Prompt(ctx context.Context, out io.Writer) error {
	var term bool
	if _, err := console.ConsoleFromFile(os.Stdin); err == nil {
		term = true
	}

	var msgs []string
	var flags []string

	if c.NetworkHost {
		msgs = append(msgs, " - Running build containers that can access host network")
		flags = append(flags, "network.host")
	}
	if c.SecurityInsecure {
		msgs = append(msgs, " - Running privileged containers that can make system changes")
		flags = append(flags, "security.insecure")
	}

	if len(msgs) == 0 {
		return nil
	}

	fmt.Fprintf(out, "Your build is requesting privileges for following possibly insecure capabilities:\n\n")
	for _, m := range msgs {
		fmt.Fprintf(out, "%s\n", m)
	}

	for i, f := range flags {
		flags[i] = "--allow=" + f
	}

	if term {
		fmt.Fprintf(out, "\nIn order to not see this message in the future pass %q to grant requested privileges.\n", strings.Join(flags, " "))
	} else {
		fmt.Fprintf(out, "\nPass %q to grant requested privileges.\n", strings.Join(flags, " "))
	}

	args := append([]string(nil), os.Args...)
	if filepath.Base(args[0]) == "docker-buildx" {
		args[0] = "docker"
	}
	idx := slices.Index(args, "bake")

	if idx != -1 {
		fmt.Fprintf(out, "\nYour full command with requested privileges:\n\n")
		fmt.Fprintf(out, "%s %s %s\n\n", strings.Join(args[:idx+1], " "), strings.Join(flags, " "), strings.Join(args[idx+1:], " "))
	}

	if term {
		fmt.Fprintf(out, "Do you want to grant requested privileges and continue? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answerCh := make(chan string, 1)
		go func() {
			answer, _, _ := reader.ReadLine()
			answerCh <- string(answer)
			close(answerCh)
		}()

		select {
		case <-ctx.Done():
		case answer := <-answerCh:
			if strings.ToLower(string(answer)) == "y" {
				return nil
			}
		}
	}

	return errors.Errorf("additional privileges requested")
}
