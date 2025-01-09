package bake

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/containerd/console"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/util/osutil"
	"github.com/moby/buildkit/util/entitlements"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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
				return conf, errors.Errorf("unknown entitlement key %q", k)
			}
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

	rwPaths := map[string]struct{}{}
	roPaths := map[string]struct{}{}

	for _, p := range collectLocalPaths(bo.Inputs) {
		roPaths[p] = struct{}{}
	}

	for _, p := range bo.ExportsLocalPathsTemporary {
		rwPaths[p] = struct{}{}
	}

	for _, ce := range bo.CacheTo {
		if ce.Type == "local" {
			if dest, ok := ce.Attrs["dest"]; ok {
				rwPaths[dest] = struct{}{}
			}
		}
	}

	for _, ci := range bo.CacheFrom {
		if ci.Type == "local" {
			if src, ok := ci.Attrs["src"]; ok {
				roPaths[src] = struct{}{}
			}
		}
	}

	for _, secret := range bo.SecretSpecs {
		if secret.FilePath != "" {
			roPaths[secret.FilePath] = struct{}{}
		}
	}

	for _, ssh := range bo.SSHSpecs {
		for _, p := range ssh.Paths {
			roPaths[p] = struct{}{}
		}
		if len(ssh.Paths) == 0 {
			if !c.SSH {
				expected.SSH = true
			}
		}
	}

	var err error
	expected.FSRead, err = findMissingPaths(c.FSRead, roPaths)
	if err != nil {
		return err
	}

	expected.FSWrite, err = findMissingPaths(c.FSWrite, rwPaths)
	if err != nil {
		return err
	}

	return nil
}

func (c EntitlementConf) Prompt(ctx context.Context, isRemote bool, out io.Writer) error {
	var term bool
	if _, err := console.ConsoleFromFile(os.Stdin); err == nil {
		term = true
	}

	var msgs []string
	var flags []string

	// these warnings are currently disabled to give users time to update
	var msgsFS []string
	var flagsFS []string

	if c.NetworkHost {
		msgs = append(msgs, " - Running build containers that can access host network")
		flags = append(flags, string(EntitlementKeyNetworkHost))
	}
	if c.SecurityInsecure {
		msgs = append(msgs, " - Running privileged containers that can make system changes")
		flags = append(flags, string(EntitlementKeySecurityInsecure))
	}

	if c.SSH {
		msgsFS = append(msgsFS, " - Forwarding default SSH agent socket")
		flagsFS = append(flagsFS, string(EntitlementKeySSH))
	}

	roPaths, rwPaths, commonPaths := groupSamePaths(c.FSRead, c.FSWrite)
	wd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(err, "failed to get current working directory")
	}
	wd, err = filepath.EvalSymlinks(wd)
	if err != nil {
		return errors.Wrap(err, "failed to evaluate working directory")
	}
	roPaths = toRelativePaths(roPaths, wd)
	rwPaths = toRelativePaths(rwPaths, wd)
	commonPaths = toRelativePaths(commonPaths, wd)

	if len(commonPaths) > 0 {
		for _, p := range commonPaths {
			msgsFS = append(msgsFS, fmt.Sprintf(" - Read and write access to path %s", p))
			flagsFS = append(flagsFS, string(EntitlementKeyFS)+"="+p)
		}
	}

	if len(roPaths) > 0 {
		for _, p := range roPaths {
			msgsFS = append(msgsFS, fmt.Sprintf(" - Read access to path %s", p))
			flagsFS = append(flagsFS, string(EntitlementKeyFSRead)+"="+p)
		}
	}

	if len(rwPaths) > 0 {
		for _, p := range rwPaths {
			msgsFS = append(msgsFS, fmt.Sprintf(" - Write access to path %s", p))
			flagsFS = append(flagsFS, string(EntitlementKeyFSWrite)+"="+p)
		}
	}

	if len(msgs) == 0 && len(msgsFS) == 0 {
		return nil
	}

	fmt.Fprintf(out, "Your build is requesting privileges for following possibly insecure capabilities:\n\n")
	for _, m := range slices.Concat(msgs, msgsFS) {
		fmt.Fprintf(out, "%s\n", m)
	}

	for i, f := range flags {
		flags[i] = "--allow=" + f
	}
	for i, f := range flagsFS {
		flagsFS[i] = "--allow=" + f
	}

	if term {
		fmt.Fprintf(out, "\nIn order to not see this message in the future pass %q to grant requested privileges.\n", strings.Join(slices.Concat(flags, flagsFS), " "))
	} else {
		fmt.Fprintf(out, "\nPass %q to grant requested privileges.\n", strings.Join(slices.Concat(flags, flagsFS), " "))
	}

	args := append([]string(nil), os.Args...)
	if v, ok := os.LookupEnv("DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND"); ok && v != "" {
		args[0] = v
	}
	idx := slices.Index(args, "bake")

	if idx != -1 {
		fmt.Fprintf(out, "\nYour full command with requested privileges:\n\n")
		fmt.Fprintf(out, "%s %s %s\n\n", strings.Join(args[:idx+1], " "), strings.Join(slices.Concat(flags, flagsFS), " "), strings.Join(args[idx+1:], " "))
	}

	fsEntitlementsEnabled := true
	if isRemote {
		if v, ok := os.LookupEnv("BAKE_ALLOW_REMOTE_FS_ACCESS"); ok {
			vv, err := strconv.ParseBool(v)
			if err != nil {
				return errors.Wrapf(err, "failed to parse BAKE_ALLOW_REMOTE_FS_ACCESS value %q", v)
			}
			fsEntitlementsEnabled = !vv
		}
	}
	v, fsEntitlementsSet := os.LookupEnv("BUILDX_BAKE_ENTITLEMENTS_FS")
	if fsEntitlementsSet {
		vv, err := strconv.ParseBool(v)
		if err != nil {
			return errors.Wrapf(err, "failed to parse BUILDX_BAKE_ENTITLEMENTS_FS value %q", v)
		}
		fsEntitlementsEnabled = vv
	}

	if !fsEntitlementsEnabled && len(msgs) == 0 {
		return nil
	}
	if fsEntitlementsEnabled && !fsEntitlementsSet && len(msgsFS) != 0 {
		fmt.Fprintf(out, "To disable filesystem entitlements checks, you can set BUILDX_BAKE_ENTITLEMENTS_FS=0 .\n\n")
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

func isParentOrEqualPath(p, parent string) bool {
	if p == parent || parent == "/" {
		return true
	}
	if strings.HasPrefix(p, filepath.Clean(parent+string(filepath.Separator))) {
		return true
	}
	return false
}

func findMissingPaths(set []string, paths map[string]struct{}) ([]string, error) {
	set, allowAny, err := evaluatePaths(set)
	if err != nil {
		return nil, err
	} else if allowAny {
		return nil, nil
	}

	paths, err = evaluateToExistingPaths(paths)
	if err != nil {
		return nil, err
	}
	paths, err = dedupPaths(paths)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(paths))
loop0:
	for p := range paths {
		for _, c := range set {
			if isParentOrEqualPath(p, c) {
				continue loop0
			}
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, nil
	}

	slices.Sort(out)

	return out, nil
}

func dedupPaths(in map[string]struct{}) (map[string]struct{}, error) {
	arr := make([]string, 0, len(in))
	for p := range in {
		arr = append(arr, filepath.Clean(p))
	}

	slices.SortFunc(arr, func(a, b string) int {
		return cmp.Compare(len(a), len(b))
	})

	m := make(map[string]struct{}, len(arr))
loop0:
	for _, p := range arr {
		for parent := range m {
			if strings.HasPrefix(p, parent+string(filepath.Separator)) {
				continue loop0
			}
		}
		m[p] = struct{}{}
	}
	return m, nil
}

func toRelativePaths(in []string, wd string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		rel, err := filepath.Rel(wd, p)
		if err == nil {
			// allow up to one level of ".." in the path
			if !strings.HasPrefix(rel, ".."+string(filepath.Separator)+"..") {
				out = append(out, rel)
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

func groupSamePaths(in1, in2 []string) ([]string, []string, []string) {
	if in1 == nil || in2 == nil {
		return in1, in2, nil
	}

	slices.Sort(in1)
	slices.Sort(in2)

	common := []string{}
	i, j := 0, 0

	for i < len(in1) && j < len(in2) {
		switch {
		case in1[i] == in2[j]:
			common = append(common, in1[i])
			i++
			j++
		case in1[i] < in2[j]:
			i++
		default:
			j++
		}
	}

	in1 = removeCommonPaths(in1, common)
	in2 = removeCommonPaths(in2, common)

	return in1, in2, common
}

func removeCommonPaths(in, common []string) []string {
	filtered := make([]string, 0, len(in))
	commonIndex := 0
	for _, path := range in {
		if commonIndex < len(common) && path == common[commonIndex] {
			commonIndex++
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func evaluatePaths(in []string) ([]string, bool, error) {
	out := make([]string, 0, len(in))
	allowAny := false
	for _, p := range in {
		if p == "*" {
			allowAny = true
			continue
		}
		v, err := filepath.Abs(p)
		if err != nil {
			logrus.Warnf("failed to evaluate entitlement path %q: %v", p, err)
			continue
		}
		v, rest, err := evaluateToExistingPath(v)
		if err != nil {
			return nil, false, errors.Wrapf(err, "failed to evaluate path %q", p)
		}
		v, err = osutil.GetLongPathName(v)
		if err != nil {
			return nil, false, errors.Wrapf(err, "failed to evaluate path %q", p)
		}
		if rest != "" {
			v = filepath.Join(v, rest)
		}
		out = append(out, v)
	}
	return out, allowAny, nil
}

func evaluateToExistingPaths(in map[string]struct{}) (map[string]struct{}, error) {
	m := make(map[string]struct{}, len(in))
	for p := range in {
		v, _, err := evaluateToExistingPath(p)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to evaluate path %q", p)
		}
		v, err = osutil.GetLongPathName(v)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to evaluate path %q", p)
		}
		m[v] = struct{}{}
	}
	return m, nil
}

func evaluateToExistingPath(in string) (string, string, error) {
	in, err := filepath.Abs(in)
	if err != nil {
		return "", "", err
	}

	volLen := volumeNameLen(in)
	pathSeparator := string(os.PathSeparator)

	if volLen < len(in) && os.IsPathSeparator(in[volLen]) {
		volLen++
	}
	vol := in[:volLen]
	dest := vol
	linksWalked := 0
	var end int
	for start := volLen; start < len(in); start = end {
		for start < len(in) && os.IsPathSeparator(in[start]) {
			start++
		}
		end = start
		for end < len(in) && !os.IsPathSeparator(in[end]) {
			end++
		}

		if end == start {
			break
		} else if in[start:end] == "." {
			continue
		} else if in[start:end] == ".." {
			var r int
			for r = len(dest) - 1; r >= volLen; r-- {
				if os.IsPathSeparator(dest[r]) {
					break
				}
			}
			if r < volLen || dest[r+1:] == ".." {
				if len(dest) > volLen {
					dest += pathSeparator
				}
				dest += ".."
			} else {
				dest = dest[:r]
			}
			continue
		}

		if len(dest) > volumeNameLen(dest) && !os.IsPathSeparator(dest[len(dest)-1]) {
			dest += pathSeparator
		}
		dest += in[start:end]

		fi, err := os.Lstat(dest)
		if err != nil {
			// If the component doesn't exist, return the last valid path
			if os.IsNotExist(err) {
				for r := len(dest) - 1; r >= volLen; r-- {
					if os.IsPathSeparator(dest[r]) {
						return dest[:r], in[start:], nil
					}
				}
				return vol, in[start:], nil
			}
			return "", "", err
		}

		if fi.Mode()&fs.ModeSymlink == 0 {
			if !fi.Mode().IsDir() && end < len(in) {
				return "", "", syscall.ENOTDIR
			}
			continue
		}

		linksWalked++
		if linksWalked > 255 {
			return "", "", errors.New("too many symlinks")
		}

		link, err := os.Readlink(dest)
		if err != nil {
			return "", "", err
		}

		in = link + in[end:]

		v := volumeNameLen(link)
		if v > 0 {
			if v < len(link) && os.IsPathSeparator(link[v]) {
				v++
			}
			vol = link[:v]
			dest = vol
			end = len(vol)
		} else if len(link) > 0 && os.IsPathSeparator(link[0]) {
			dest = link[:1]
			end = 1
			vol = link[:1]
			volLen = 1
		} else {
			var r int
			for r = len(dest) - 1; r >= volLen; r-- {
				if os.IsPathSeparator(dest[r]) {
					break
				}
			}
			if r < volLen {
				dest = vol
			} else {
				dest = dest[:r]
			}
			end = 0
		}
	}
	return filepath.Clean(dest), "", nil
}

func volumeNameLen(s string) int {
	return len(filepath.VolumeName(s))
}
