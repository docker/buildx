# Contribute to the Buildx project

This page contains information about reporting issues as well as some tips and
guidelines useful to experienced open source contributors.

## Reporting security issues

The project maintainers take security seriously. If you discover a security
issue, please bring it to their attention right away!

**Please _DO NOT_ file a public issue**, instead send your report privately to
[security@docker.com](mailto:security@docker.com).

Security reports are greatly appreciated and we will publicly thank you for it.
We also like to send gifts&mdash;if you're into schwag, make sure to let
us know. We currently do not offer a paid security bounty program, but are not
ruling it out in the future.


## Reporting other issues

A great way to contribute to the project is to send a detailed report when you
encounter an issue. We always appreciate a well-written, thorough bug report,
and will thank you for it!

Check that [our issue database](https://github.com/docker/buildx/issues)
doesn't already include that problem or suggestion before submitting an issue.
If you find a match, you can use the "subscribe" button to get notified on
updates. Do *not* leave random "+1" or "I have this too" comments, as they
only clutter the discussion, and don't help resolving it. However, if you
have ways to reproduce the issue or have additional information that may help
resolving the issue, please leave a comment.

Include the steps required to reproduce the problem if possible and applicable.
This information will help us review and fix your issue faster. When sending
lengthy log-files, consider posting them as an attachment, instead of posting
inline.

**Do not forget to remove sensitive data from your logfiles before submitting**
 (you can replace those parts with "REDACTED").

### Pull requests are always welcome

Not sure if that typo is worth a pull request? Found a bug and know how to fix
it? Do it! We will appreciate it.

If your pull request is not accepted on the first try, don't be discouraged! If
there's a problem with the implementation, hopefully you received feedback on
what to improve.

We're trying very hard to keep Buildx lean and focused. We don't want it to
do everything for everybody. This means that we might decide against
incorporating a new feature. However, there might be a way to implement that
feature *on top of* Buildx.

### Design and cleanup proposals

You can propose new designs for existing features. You can also design
entirely new features. We really appreciate contributors who want to refactor or
otherwise cleanup our project.

### Sign your work

The sign-off is a simple line at the end of the explanation for the patch. Your
signature certifies that you wrote the patch or otherwise have the right to pass
it on as an open-source patch. The rules are pretty simple: if you can certify
the below (from [developercertificate.org](http://developercertificate.org/)):

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
1 Letterman Drive
Suite D4700
San Francisco, CA, 94129

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Then you just add a line to every git commit message:

    Signed-off-by: Joe Smith <joe.smith@email.com>

**Use your real name** (sorry, no pseudonyms or anonymous contributions.)

If you set your `user.name` and `user.email` git configs, you can sign your
commit automatically with `git commit -s`.

### Run the unit- and integration-tests

Running tests:

```bash
make test
```

This runs all unit and integration tests, in a containerized environment.
Locally, every package can be tested separately with standard Go tools, but
integration tests are skipped if local user doesn't have enough permissions or
worker binaries are not installed.

```bash
# run unit tests only
make test-unit

# run integration tests only
make test-integration

# test a specific package
TESTPKGS=./bake make test

# run all integration tests with a specific worker
TESTFLAGS="--run=//worker=remote -v" make test-integration

# run a specific integration test
TESTFLAGS="--run /TestBuild/worker=remote/ -v" make test-integration

# run a selection of integration tests using a regexp
TESTFLAGS="--run /TestBuild.*/worker=remote/ -v" make test-integration
```

> **Note**
>
> Set `TEST_KEEP_CACHE=1` for the test framework to keep external dependant
> images in a docker volume if you are repeatedly calling `make test`. This
> helps to avoid rate limiting on the remote registry side.

> **Note**
>
> Set `TEST_DOCKERD=1` for the test framework to enable the docker workers,
> specifically the `docker` and `docker-container` drivers.
>
> The docker tests cannot be run in parallel, so require passing `--parallel=1`
> in `TESTFLAGS`.

> **Note**
>
> If you are working behind a proxy, you can set some of or all
> `HTTP_PROXY=http://ip:port`, `HTTPS_PROXY=http://ip:port`, `NO_PROXY=http://ip:port`
> for the test framework to specify the proxy build args.


### Run the helper commands

To enter a demo container environment and experiment, you may run:

```
$ make shell
```

To validate PRs before submitting them you should run:

```
$ make validate-all
```

To generate new vendored files with go modules run:

```
$ make vendor
```

### Generate profiling data

You can configure Buildx to generate [`pprof`](https://github.com/google/pprof)
memory and CPU profiles to analyze and optimize your builds. These profiles are
useful for identifying performance bottlenecks, detecting memory
inefficiencies, and ensuring the program (Buildx) runs efficiently.

The following environment variables control whether Buildx generates profiling
data for builds:

```console
$ export BUILDX_CPU_PROFILE=buildx_cpu.prof
$ export BUILDX_MEM_PROFILE=buildx_mem.prof
```

When set, Buildx emits profiling samples for the builds to the location
specified by the environment variable.

To analyze and visualize profiling samples, you need `pprof` from the Go
toolchain, and (optionally) GraphViz for visualization in a graphical format.

To inspect profiling data with `pprof`:

1. Build a local binary of Buildx from source.

   ```console
   $ docker buildx bake
   ```

   The binary gets exported to `./bin/build/buildx`.

2. Run a build and with the environment variables set to generate profiling data.

   ```console
   $ export BUILDX_CPU_PROFILE=buildx_cpu.prof
   $ export BUILDX_MEM_PROFILE=buildx_mem.prof
   $ ./bin/build/buildx bake
   ```

   This creates `buildx_cpu.prof` and `buildx_mem.prof` for the build.

3. Start `pprof` and specify the filename of the profile that you want to
   analyze.

   ```console
   $ go tool pprof buildx_cpu.prof
   ```

   This opens the `pprof` interactive console. From here, you can inspect the
   profiling sample using various commands. For example, use `top 10` command
   to view the top 10 most time-consuming entries.

   ```plaintext
   (pprof) top 10
   Showing nodes accounting for 3.04s, 91.02% of 3.34s total
   Dropped 123 nodes (cum <= 0.02s)
   Showing top 10 nodes out of 159
         flat  flat%   sum%        cum   cum%
        1.14s 34.13% 34.13%      1.14s 34.13%  syscall.syscall
        0.91s 27.25% 61.38%      0.91s 27.25%  runtime.kevent
        0.35s 10.48% 71.86%      0.35s 10.48%  runtime.pthread_cond_wait
        0.22s  6.59% 78.44%      0.22s  6.59%  runtime.pthread_cond_signal
        0.15s  4.49% 82.93%      0.15s  4.49%  runtime.usleep
        0.10s  2.99% 85.93%      0.10s  2.99%  runtime.memclrNoHeapPointers
        0.10s  2.99% 88.92%      0.10s  2.99%  runtime.memmove
        0.03s   0.9% 89.82%      0.03s   0.9%  runtime.madvise
        0.02s   0.6% 90.42%      0.02s   0.6%  runtime.(*mspan).typePointersOfUnchecked
        0.02s   0.6% 91.02%      0.02s   0.6%  runtime.pcvalue
   ```

To view the call graph in a GUI, run `go tool pprof -http=:8081 <sample>`.

> [!NOTE]
> Requires [GraphViz](https://www.graphviz.org/) to be installed.

```console
$ go tool pprof -http=:8081 buildx_cpu.prof
Serving web UI on http://127.0.0.1:8081
http://127.0.0.1:8081
```

For more information about using `pprof` and how to interpret the call graph,
refer to the [`pprof` README](https://github.com/google/pprof/blob/main/doc/README.md).

### Conventions

- Fork the repository and make changes on your fork in a feature branch
- Submit tests for your changes. See [run the unit- and integration-tests](#run-the-unit--and-integration-tests)
  for details.
- [Sign your work](#sign-your-work)

Write clean code. Universally formatted code promotes ease of writing, reading,
and maintenance. Always run `gofmt -s -w file.go` on each changed file before
committing your changes. Most editors have plug-ins that do this automatically.

Pull request descriptions should be as clear as possible and include a
reference to all the issues that they address. Be sure that the [commit
messages](#commit-messages) also contain the relevant information.

### Successful Changes

Before contributing large or high impact changes, make the effort to coordinate
with the maintainers of the project before submitting a pull request. This
prevents you from doing extra work that may or may not be merged.

Large PRs that are just submitted without any prior communication are unlikely
to be successful.

While pull requests are the methodology for submitting changes to code, changes
are much more likely to be accepted if they are accompanied by additional
engineering work. While we don't define this explicitly, most of these goals
are accomplished through communication of the design goals and subsequent
solutions. Often times, it helps to first state the problem before presenting
solutions.

Typically, the best methods of accomplishing this are to submit an issue,
stating the problem. This issue can include a problem statement and a
checklist with requirements. If solutions are proposed, alternatives should be
listed and eliminated. Even if the criteria for elimination of a solution is
frivolous, say so.

Larger changes typically work best with design documents. These are focused on
providing context to the design at the time the feature was conceived and can
inform future documentation contributions.

### Commit Messages

Commit messages must start with a capitalized and short summary (max. 50 chars)
written in the imperative, followed by an optional, more detailed explanatory
text which is separated from the summary by an empty line.

Commit messages should follow best practices, including explaining the context
of the problem and how it was solved, including in caveats or follow up changes
required. They should tell the story of the change and provide readers
understanding of what led to it.

If you're lost about what this even means, please see [How to Write a Git
Commit Message](http://chris.beams.io/posts/git-commit/) for a start.

In practice, the best approach to maintaining a nice commit message is to
leverage a `git add -p` and `git commit --amend` to formulate a solid
changeset. This allows one to piece together a change, as information becomes
available.

If you squash a series of commits, don't just submit that. Re-write the commit
message, as if the series of commits was a single stroke of brilliance.

That said, there is no requirement to have a single commit for a PR, as long as
each commit tells the story. For example, if there is a feature that requires a
package, it might make sense to have the package in a separate commit then have
a subsequent commit that uses it.

Remember, you're telling part of the story with the commit message. Don't make
your chapter weird.

### Review

Code review comments may be added to your pull request. Discuss, then make the
suggested modifications and push additional commits to your feature branch. Post
a comment after pushing. New commits show up in the pull request automatically,
but the reviewers are notified only when you comment.

Pull requests must be cleanly rebased on top of master without multiple branches
mixed into the PR.

> **Git tip**: If your PR no longer merges cleanly, use `rebase master` in your
> feature branch to update your pull request rather than `merge master`.

Before you make a pull request, squash your commits into logical units of work
using `git rebase -i` and `git push -f`. A logical unit of work is a consistent
set of patches that should be reviewed together: for example, upgrading the
version of a vendored dependency and taking advantage of its now available new
feature constitute two separate units of work. Implementing a new function and
calling it in another file constitute a single logical unit of work. The very
high majority of submissions should have a single commit, so if in doubt: squash
down to one.

- After every commit, [make sure the test suite passes](#run-the-unit--and-integration-tests).
  Include documentation changes in the same pull request so that a revert would
  remove all traces of the feature or fix.
- Include an issue reference like `closes #XXXX` or `fixes #XXXX` in the PR
  description that close an issue. Including references automatically closes
  the issue on a merge.
- Do not add yourself to the `AUTHORS` file, as it is regenerated regularly
  from the Git history.
- See the [Coding Style](#coding-style) for further guidelines.


### Merge approval

Project maintainers use LGTM (Looks Good To Me) in comments on the code review to
indicate acceptance, or use the Github review approval feature.


## Coding Style

Unless explicitly stated, we follow all coding guidelines from the Go
community. While some of these standards may seem arbitrary, they somehow seem
to result in a solid, consistent codebase.

It is possible that the code base does not currently comply with these
guidelines. We are not looking for a massive PR that fixes this, since that
goes against the spirit of the guidelines. All new contributions should make a
best effort to clean up and make the code base better than they left it.
Obviously, apply your best judgement. Remember, the goal here is to make the
code base easier for humans to navigate and understand. Always keep that in
mind when nudging others to comply.

The rules:

1. All code should be formatted with `gofmt -s`.
2. All code should pass the default levels of
   [`golint`](https://github.com/golang/lint).
3. All code should follow the guidelines covered in [Effective
   Go](http://golang.org/doc/effective_go.html) and [Go Code Review
   Comments](https://github.com/golang/go/wiki/CodeReviewComments).
4. Comment the code. Tell us the why, the history and the context.
5. Document _all_ declarations and methods, even private ones. Declare
   expectations, caveats and anything else that may be important. If a type
   gets exported, having the comments already there will ensure it's ready.
6. Variable name length should be proportional to its context and no longer.
   `noCommaALongVariableNameLikeThisIsNotMoreClearWhenASimpleCommentWouldDo`.
   In practice, short methods will have short variable names and globals will
   have longer names.
7. No underscores in package names. If you need a compound name, step back,
   and re-examine why you need a compound name. If you still think you need a
   compound name, lose the underscore.
8. No utils or helpers packages. If a function is not general enough to
   warrant its own package, it has not been written generally enough to be a
   part of a util package. Just leave it unexported and well-documented.
9. All tests should run with `go test` and outside tooling should not be
   required. No, we don't need another unit testing framework. Assertion
   packages are acceptable if they provide _real_ incremental value.
10. Even though we call these "rules" above, they are actually just
    guidelines. Since you've read all the rules, you now know that.

If you are having trouble getting into the mood of idiomatic Go, we recommend
reading through [Effective Go](https://golang.org/doc/effective_go.html). The
[Go Blog](https://blog.golang.org) is also a great resource.
