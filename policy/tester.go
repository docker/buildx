package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/containerd/platforms"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type TestOptions struct {
	Run      string
	Filename string
	Root     fs.StatFS
	Resolver *TestResolver
}

type TestSummary struct {
	Results []TestResult
	Failed  int
}

type TestResult struct {
	Name           string
	Package        string
	Passed         bool
	Allow          *bool
	DenyMessages   []string
	Input          *Input
	Decision       *Decision
	MissingInput   []string
	MetadataNeeded []string
}

type testDef struct {
	Name    string
	PkgPath string
}

type TestResolver struct {
	Resolve          func(context.Context, *pb.SourceOp, *gwpb.ResolveSourceMetaRequest) (*gwpb.ResolveSourceMetaResponse, error)
	Platform         func(context.Context) (*ocispecs.Platform, error)
	VerifierProvider PolicyVerifierProvider
}

func RunPolicyTests(ctx context.Context, path string, opts TestOptions) (TestSummary, error) {
	var summary TestSummary
	if opts.Root == nil {
		return summary, errors.New("policy root filesystem is required")
	}
	policyModules, policyFiles, err := loadPolicyModules(opts.Root, opts.Filename)
	if err != nil {
		return summary, err
	}

	testModules, _, err := LoadTestModules(opts.Root, path)
	if err != nil {
		return summary, err
	}

	modules := make(map[string]*ast.Module, len(policyModules)+len(testModules))
	maps.Copy(modules, policyModules)
	maps.Copy(modules, testModules)

	fsProvider := func() (fs.StatFS, func() error, error) {
		return opts.Root, nil, nil
	}

	p := NewPolicy(Opt{
		FS: fsProvider,
	})

	comp, closeLoader, err := compilePolicyModules(modules, p, fsProvider)
	if err != nil {
		if closeLoader != nil {
			_ = closeLoader()
		}
		return summary, err
	}
	if closeLoader != nil {
		defer closeLoader()
	}

	tests := findPolicyTests(testModules)
	if opts.Run != "" {
		tests = filterPolicyTests(tests, opts.Run)
	}
	if len(tests) == 0 {
		return summary, errors.New("no tests found")
	}

	for _, t := range tests {
		result, err := runPolicyTest(ctx, policyModules, testModules, policyFiles, comp, p, t, opts, fsProvider)
		if err != nil {
			return summary, err
		}
		if !result.Passed {
			summary.Failed++
		}
		summary.Results = append(summary.Results, result)
	}
	return summary, nil
}

func LoadTestModules(root fs.StatFS, path string) (map[string]*ast.Module, []File, error) {
	path = filepath.ToSlash(path)
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "."
	}
	info, err := root.Stat(path)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "stat %s", path)
	}

	var files []string
	if info.IsDir() {
		entries, err := fs.ReadDir(root, path)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "read dir %s", path)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, "_test.rego") {
				continue
			}
			files = append(files, filepath.ToSlash(filepath.Join(path, name)))
		}
	} else {
		if !strings.HasSuffix(path, "_test.rego") {
			return nil, nil, errors.Errorf("test file must have _test.rego suffix: %s", path)
		}
		files = append(files, filepath.ToSlash(path))
	}

	if len(files) == 0 {
		return nil, nil, errors.New("no policy tests found")
	}

	sort.Strings(files)
	modules := make(map[string]*ast.Module, len(files))
	entries := make([]File, 0, len(files))
	for _, file := range files {
		dt, err := fs.ReadFile(root, file)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "read policy test module %s", file)
		}
		mod, err := ast.ParseModuleWithOpts(file, string(dt), ast.ParserOptions{
			RegoVersion: ast.RegoV1,
		})
		if err != nil {
			return nil, nil, errors.Wrapf(err, "parse policy test module %s", file)
		}
		modules[file] = mod
		entries = append(entries, File{
			Filename: file,
			Data:     dt,
		})
	}

	return modules, entries, nil
}

func loadPolicyModules(root fs.StatFS, filename string) (map[string]*ast.Module, []File, error) {
	if filename == "" {
		return nil, nil, errors.New("policy filename is required")
	}
	policyFile := filename + ".rego"
	dt, err := fs.ReadFile(root, policyFile)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "read policy module %s", policyFile)
	}
	mod, err := ast.ParseModuleWithOpts(policyFile, string(dt), ast.ParserOptions{
		RegoVersion: ast.RegoV1,
	})
	if err != nil {
		return nil, nil, errors.Wrapf(err, "parse policy module %s", policyFile)
	}
	modules := map[string]*ast.Module{
		filepath.ToSlash(policyFile): mod,
	}
	files := []File{
		{
			Filename: filepath.ToSlash(policyFile),
			Data:     dt,
		},
	}
	return modules, files, nil
}

func compilePolicyModules(modules map[string]*ast.Module, p *Policy, fsProvider func() (fs.StatFS, func() error, error)) (*ast.Compiler, func() error, error) {
	caps := &ast.Capabilities{
		Builtins: builtins(),
		Features: slices.Clone(ast.Features),
	}
	comp := ast.NewCompiler().WithCapabilities(caps).WithKeepModules(true)

	builtinDefs := make(map[string]*ast.Builtin)
	for _, f := range p.funcs {
		builtinDefs[f.decl.Name] = &ast.Builtin{
			Name: f.decl.Name,
			Decl: f.decl.Decl,
		}
	}
	comp = comp.WithBuiltins(builtinDefs)

	loader, closeLoader := newPolicyModuleLoader(fsProvider)
	comp = comp.WithModuleLoader(loader)

	comp.Compile(modules)
	if comp.Failed() {
		return nil, closeLoader, errors.Errorf("compile: %v", comp.Errors)
	}
	return comp, closeLoader, nil
}

func findPolicyTests(modules map[string]*ast.Module) []testDef {
	seen := map[string]testDef{}
	for _, mod := range modules {
		pkgPath := mod.Package.Path.String()
		for _, rule := range mod.Rules {
			if len(rule.Head.Args) > 0 {
				continue
			}
			if rule.Head.Value != nil {
				if _, ok := rule.Head.Value.Value.(ast.Boolean); !ok {
					continue
				}
			}
			name := string(rule.Head.Name)
			if strings.HasPrefix(name, "test_") {
				seen[name] = testDef{Name: name, PkgPath: pkgPath}
			}
		}
	}

	out := make([]testDef, 0, len(seen))
	for _, t := range seen {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func filterPolicyTests(tests []testDef, substr string) []testDef {
	out := make([]testDef, 0, len(tests))
	for _, t := range tests {
		if strings.Contains(t.Name, substr) {
			out = append(out, t)
		}
	}
	return out
}

func runPolicyTest(ctx context.Context, policyModules map[string]*ast.Module, testModules map[string]*ast.Module, policyFiles []File, compiler *ast.Compiler, p *Policy, t testDef, opts TestOptions, fsProvider func() (fs.StatFS, func() error, error)) (TestResult, error) {
	result := TestResult{
		Name:    t.Name,
		Package: t.PkgPath,
	}
	policyPackageModules := modulesForPackage(policyModules, t.PkgPath)
	input, err := lookupTestInput(testModules, t)
	if err != nil {
		return result, err
	}
	effectiveInput := input
	if opts.Resolver != nil {
		resolvedInput, ok, err := resolveTestInput(ctx, policyFiles, opts.Resolver, policyPackageModules, input, fsProvider)
		if err != nil {
			return result, err
		}
		if ok {
			effectiveInput = resolvedInput
		}
	}
	result.Input = effectiveInput

	testState := stateFromInput(effectiveInput)
	query := fmt.Sprintf("%s.%s", t.PkgPath, t.Name)
	ok, err := evalBool(ctx, compiler, p, testState, query, effectiveInput)
	if err != nil {
		return result, err
	}
	result.Passed = ok

	decisionState := stateFromInput(effectiveInput)
	decision, allow, deny := evalDecision(ctx, compiler, p, decisionState, t.PkgPath, effectiveInput)
	result.Decision = decision
	result.Allow = allow
	result.DenyMessages = deny

	missing := missingInputRefs(policyPackageModules, effectiveInput)
	result.MissingInput = uniqueSortedStrings(missing)
	result.MetadataNeeded = summarizeMetadataRequests(result.MissingInput)

	return result, nil
}

func lookupTestInput(testModules map[string]*ast.Module, t testDef) (*Input, error) {
	if len(testModules) == 0 {
		return nil, nil
	}
	var inputTerm *ast.Term
	for _, mod := range modulesForPackage(testModules, t.PkgPath) {
		for _, rule := range mod.Rules {
			if string(rule.Head.Name) != t.Name {
				continue
			}
			term, err := inputTermFromRule(rule, t)
			if err != nil {
				return nil, err
			}
			if term == nil {
				continue
			}
			if inputTerm != nil && !inputTerm.Equal(term) {
				return nil, errors.Errorf("multiple input overrides for %s", t.Name)
			}
			inputTerm = term
		}
	}
	if inputTerm == nil {
		return nil, nil
	}
	var inp Input
	if err := ast.As(inputTerm.Value, &inp); err != nil {
		return nil, errors.Wrapf(err, "failed to decode test input for %s", t.Name)
	}
	return &inp, nil
}

func inputTermFromRule(rule *ast.Rule, t testDef) (*ast.Term, error) {
	var inputTerm *ast.Term
	for _, expr := range rule.Body {
		for _, w := range expr.With {
			if w == nil || w.Target == nil || w.Value == nil {
				continue
			}
			ref, ok := w.Target.Value.(ast.Ref)
			if !ok || !ref.Equal(ast.InputRootRef) {
				continue
			}
			if inputTerm != nil && !inputTerm.Equal(w.Value) {
				return nil, errors.Errorf("multiple input overrides for %s", t.Name)
			}
			inputTerm = w.Value
		}
	}
	return inputTerm, nil
}

func evalDecision(ctx context.Context, compiler *ast.Compiler, p *Policy, st *state, pkgPath string, input *Input) (*Decision, *bool, []string) {
	query := fmt.Sprintf("%s.decision", pkgPath)
	val, err := evalValue(ctx, compiler, p, st, query, input)
	if err != nil {
		return nil, nil, nil
	}
	decision := decodeDecision(val)
	if decision == nil {
		return nil, nil, nil
	}
	deny := decision.DenyMessages
	if len(deny) == 0 {
		deny = nil
	}
	return decision, decision.Allow, deny
}

func stateFromInput(input *Input) *state {
	st := &state{}
	if input == nil {
		return st
	}
	st.Input = *input
	return st
}

func resolveTestInput(ctx context.Context, files []File, resolver *TestResolver, policyModules []*ast.Module, input *Input, fsProvider func() (fs.StatFS, func() error, error)) (*Input, bool, error) {
	if resolver == nil {
		return nil, false, nil
	}
	source, err := sourceFromInput(input)
	if err != nil || source == nil {
		return nil, false, err
	}

	platform, err := inputPlatform(input)
	if err != nil {
		return nil, false, err
	}
	if platform == nil && strings.HasPrefix(source.Identifier, "docker-image://") {
		if resolver.Platform == nil {
			return nil, false, errors.New("resolver platform not configured")
		}
		platform, err = resolver.Platform(ctx)
		if err != nil {
			return nil, false, err
		}
	}

	var env Env
	if input != nil && hasEnv(input.Env) {
		env = input.Env
	}

	policyEval := NewPolicy(Opt{
		Files:            files,
		Env:              env,
		FS:               fsProvider,
		VerifierProvider: resolver.VerifierProvider,
	})

	srcReq := &gwpb.ResolveSourceMetaResponse{
		Source: source,
	}

	var platformPB *pb.Platform
	if platform != nil {
		platformPB = &pb.Platform{
			Architecture: platform.Architecture,
			OS:           platform.OS,
			Variant:      platform.Variant,
		}
	}

	for range 5 {
		_, next, err := policyEval.CheckPolicy(ctx, &policysession.CheckPolicyRequest{
			Platform: platformPB,
			Source:   srcReq,
		})
		if err != nil {
			return nil, false, err
		}
		if next == nil {
			inp, _, err := SourceToInputWithLogger(ctx, resolver.VerifierProvider, srcReq, platform, nil)
			if err != nil {
				return nil, false, err
			}
			if hasEnv(env) {
				inp.Env = env
			}
			if resolver.Resolve != nil && len(policyModules) > 0 {
				missing := missingInputRefs(policyModules, &inp)
				resolveMissing := filterResolvableMissing(missing)
				if len(resolveMissing) > 0 {
					req := &gwpb.ResolveSourceMetaRequest{}
					if err := AddUnknowns(req, resolveMissing); err == nil && (req.Image != nil || req.Git != nil) {
						resp, err := resolver.Resolve(ctx, source, req)
						if err != nil {
							return nil, false, err
						}
						srcReq = resp
						continue
					}
				}
			}
			return mergeInputOverrides(inp, input), true, nil
		}
		if resolver.Resolve == nil {
			return nil, false, nil
		}
		resp, err := resolver.Resolve(ctx, source, next)
		if err != nil {
			return nil, false, err
		}
		srcReq = resp
	}
	return nil, false, errors.New("maximum attempts reached for resolving policy metadata")
}

func sourceFromInput(input *Input) (*pb.SourceOp, error) {
	if input != nil && input.Image != nil {
		ref := ""
		switch {
		case input.Image.Ref != "":
			ref = input.Image.Ref
		case input.Image.FullRepo != "":
			ref = input.Image.FullRepo
		case input.Image.Repo != "":
			ref = input.Image.Repo
		}
		if ref != "" && input.Image.Tag != "" && !strings.Contains(ref, ":") {
			ref = ref + ":" + input.Image.Tag
		}
		if ref != "" {
			return &pb.SourceOp{Identifier: "docker-image://" + ref}, nil
		}
	}
	return nil, nil
}

func mergeInputOverrides(resolved Input, input *Input) *Input {
	if input == nil {
		return &resolved
	}
	if hasEnv(input.Env) {
		resolved.Env = input.Env
	}
	return &resolved
}

func hasEnv(env Env) bool {
	if env.Filename != "" || env.Target != "" {
		return true
	}
	if len(env.Args) > 0 || len(env.Labels) > 0 {
		return true
	}
	return false
}

func filterResolvableMissing(missing []string) []string {
	out := make([]string, 0, len(missing))
	for _, m := range missing {
		if strings.HasPrefix(m, "input.image.") || strings.HasPrefix(m, "input.git.") {
			out = append(out, m)
		}
	}
	return out
}

func evalBool(ctx context.Context, compiler *ast.Compiler, p *Policy, st *state, query string, input *Input) (bool, error) {
	r := newPolicyRego(compiler, p, st, query, input)
	rs, err := r.Eval(ctx)
	if err != nil {
		return false, err
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return false, nil
	}
	val, ok := rs[0].Expressions[0].Value.(bool)
	if !ok {
		return false, nil
	}
	return val, nil
}

func evalValue(ctx context.Context, compiler *ast.Compiler, p *Policy, st *state, query string, input *Input) (any, error) {
	r := newPolicyRego(compiler, p, st, query, input)
	rs, err := r.Eval(ctx)
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return nil, errors.New("empty result")
	}
	return rs[0].Expressions[0].Value, nil
}

func newPolicyRego(compiler *ast.Compiler, p *Policy, st *state, query string, input *Input) *rego.Rego {
	opts := []func(*rego.Rego){
		rego.SetRegoVersion(ast.RegoV1),
		rego.Query(query),
		rego.SkipPartialNamespace(true),
		rego.Compiler(compiler),
	}
	if input != nil {
		opts = append(opts, rego.Input(input))
	}
	for _, f := range p.funcs {
		opts = append(opts, f.impl(st))
	}
	return rego.New(opts...)
}

func modulesForPackage(modules map[string]*ast.Module, pkgPath string) []*ast.Module {
	out := make([]*ast.Module, 0, len(modules))
	for _, mod := range modules {
		if mod.Package.Path.String() == pkgPath {
			out = append(out, mod)
		}
	}
	return out
}

func missingInputRefs(mods []*ast.Module, input *Input) []string {
	if len(mods) == 0 {
		return nil
	}
	inputMap := normalizeInput(input)
	refs := collectUnknowns(mods)
	missing := make([]string, 0, len(refs))
	for _, ref := range refs {
		key := strings.TrimPrefix(ref, "input.")
		if key == ref {
			continue
		}
		key = trimKey(key)
		if key == "" {
			continue
		}
		if !inputHasPath(inputMap, strings.Split(key, ".")) {
			missing = append(missing, "input."+key)
		}
	}
	return missing
}

type inputMap map[string]json.RawMessage

func inputHasPath(input inputMap, path []string) bool {
	if input == nil {
		return false
	}
	cur := input
	for i, p := range path {
		next, ok := cur[p]
		if !ok {
			return false
		}
		if i == len(path)-1 {
			return true
		}
		var decoded inputMap
		if err := json.Unmarshal(next, &decoded); err != nil {
			return false
		}
		cur = decoded
	}
	return true
}

func normalizeInput(input *Input) inputMap {
	if input == nil {
		return nil
	}
	out, err := decodeJSONValue[inputMap](input)
	if err != nil {
		return nil
	}
	return out
}

func decodeDecision(decision any) *Decision {
	obj, err := decodeJSONValue[map[string]any](decision)
	if err != nil {
		return nil
	}
	var allow *bool
	if v, ok := obj["allow"]; ok {
		if b, ok := v.(bool); ok {
			allow = &b
		}
	}
	denyMsgs := []string{}
	if v, ok := obj["deny_msg"]; ok {
		switch val := v.(type) {
		case string:
			denyMsgs = append(denyMsgs, val)
		case []any:
			for _, entry := range val {
				if s, ok := entry.(string); ok {
					denyMsgs = append(denyMsgs, s)
				}
			}
		}
	}
	if len(denyMsgs) == 0 {
		denyMsgs = nil
	}
	return &Decision{
		Allow:        allow,
		DenyMessages: denyMsgs,
	}
}

func decodeJSONValue[T any](v any) (T, error) {
	var out T
	b, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	return out, nil
}

func summarizeMetadataRequests(missing []string) []string {
	if len(missing) == 0 {
		return nil
	}
	req := &gwpb.ResolveSourceMetaRequest{}
	trimmed := make([]string, 0, len(missing))
	for _, m := range missing {
		trimmed = append(trimmed, strings.TrimPrefix(m, "input."))
	}
	if err := AddUnknowns(req, trimmed); err != nil {
		return nil
	}
	var out []string
	if req.Image != nil {
		out = append(out, "image")
	}
	if req.Git != nil {
		out = append(out, "git")
	}
	sort.Strings(out)
	return out
}

func inputPlatform(input *Input) (*ocispecs.Platform, error) {
	if input == nil || input.Image == nil {
		return nil, nil
	}
	if input.Image.Platform != "" {
		p, err := platforms.Parse(input.Image.Platform)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid platform %s", input.Image.Platform)
		}
		p = platforms.Normalize(p)
		return &ocispecs.Platform{
			OS:           p.OS,
			Architecture: p.Architecture,
			Variant:      p.Variant,
		}, nil
	}
	if input.Image.OS != "" || input.Image.Architecture != "" || input.Image.Variant != "" {
		return &ocispecs.Platform{
			OS:           input.Image.OS,
			Architecture: input.Image.Architecture,
			Variant:      input.Image.Variant,
		}, nil
	}
	return nil, nil
}

func uniqueSortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, s := range in {
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func newPolicyModuleLoader(fsProvider func() (fs.StatFS, func() error, error)) (func(map[string]*ast.Module) (map[string]*ast.Module, error), func() error) {
	var (
		root    fs.StatFS
		closeFS func() error
	)
	loader := func(resolved map[string]*ast.Module) (map[string]*ast.Module, error) {
		out := make(map[string]*ast.Module)
		for k, v := range resolved {
			for _, imp := range v.Imports {
				pv := imp.Path.Value.String()
				pkgPath, ok := strings.CutPrefix(pv, "data.")
				if !ok {
					continue
				}
				if resolvedHasPackage(resolved, pkgPath) {
					continue
				}
				fn := strings.ReplaceAll(pkgPath, ".", "/") + ".rego"
				if _, ok := resolved[fn]; ok {
					continue
				}
				if root == nil {
					if fsProvider == nil {
						return nil, errors.Errorf("no policy FS defined for import %s", pv)
					}
					f, cf, err := fsProvider()
					if err != nil {
						return nil, errors.Wrapf(err, "failed to get policy FS for import %s", pv)
					}
					root = f
					closeFS = cf
				}
				loadName := fn
				if _, err := root.Stat(loadName); err != nil {
					return nil, errors.Wrapf(err, "import %s not found for module %s", pv, k)
				}
				dt, err := fs.ReadFile(root, loadName)
				if err != nil {
					return nil, errors.Wrapf(err, "failed to read imported policy file %s for module %s", loadName, k)
				}
				mod, err := ast.ParseModuleWithOpts(loadName, string(dt), ast.ParserOptions{
					RegoVersion: ast.RegoV1,
				})
				if err != nil {
					return nil, errors.Wrapf(err, "failed to parse imported policy file %s for module %s", loadName, k)
				}
				// rewrite package to be less strict
				pkgParts := strings.Split(pkgPath, ".")
				ref := ast.Ref{mod.Package.Path[0]}
				for _, p := range pkgParts {
					ref = append(ref, ast.StringTerm(p))
				}
				mod.Package = &ast.Package{Path: ref}
				out[fn] = mod
			}
		}
		return out, nil
	}
	return loader, func() error {
		if closeFS != nil {
			return closeFS()
		}
		return nil
	}
}

func resolvedHasPackage(resolved map[string]*ast.Module, pkgPath string) bool {
	for _, mod := range resolved {
		if mod.Package != nil && mod.Package.Path.String() == pkgPath {
			return true
		}
		if mod.Package != nil && strings.TrimPrefix(mod.Package.Path.String(), "data.") == pkgPath {
			return true
		}
	}
	return false
}
