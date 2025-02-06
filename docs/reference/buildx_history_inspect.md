# docker buildx history inspect

<!---MARKER_GEN_START-->
Inspect a build

### Subcommands

| Name                                                 | Description                |
|:-----------------------------------------------------|:---------------------------|
| [`attachment`](buildx_history_inspect_attachment.md) | Inspect a build attachment |


### Options

| Name                  | Type     | Default | Description                              |
|:----------------------|:---------|:--------|:-----------------------------------------|
| `--builder`           | `string` |         | Override the configured builder instance |
| `-D`, `--debug`       | `bool`   |         | Enable debug logging                     |
| [`--format`](#format) | `string` | `raw`   | Format the output                        |


<!---MARKER_GEN_END-->

## Examples

### <a name="format"></a> Format the output (--format)

Output format can be one of `raw`, `json`.

```console
$ docker buildx history inspect --format raw
Context:        .
Dockerfile:     Dockerfile
VCS Repository: https://github.com/crazy-max/buildx.git
VCS Revision:   04aab6958cb5feb012a3c607569573b5cab141e1
Target:         binaries
Platforms:      linux/amd64
Keep Git Dir:   true

Started:        2025-02-06 16:15:13
Duration:       1m  3s
Build Steps:    16/16 (25% cached)


Materials:
URI                                                             DIGEST
pkg:docker/docker/dockerfile@1                                  sha256:93bfd3b68c109427185cd78b4779fc82b484b0b7618e36d0f104d4d801e66d25
pkg:docker/golang@1.23-alpine3.21?platform=linux%2Famd64        sha256:2c49857f2295e89b23b28386e57e018a86620a8fede5003900f2d138ba9c4037
pkg:docker/tonistiigi/xx@1.6.1?platform=linux%2Famd64           sha256:923441d7c25f1e2eb5789f82d987693c47b8ed987c4ab3b075d6ed2b5d6779a3

Attachments:
DIGEST                                                                  PLATFORM        TYPE
sha256:1b44912514074d3e309d80f8a5886a4d89eeeb52bef4d3e57ced17d1781bfce1                 https://slsa.dev/provenance/v0.2

Print build logs: docker buildx history logs qrdbfvaoarfz42ye54lzx9aoy
```

```console
$ docker buildx history inspect --format json
{
  "name": "buildx (binaries)",
  "context": ".",
  "dockerfile": "Dockerfile",
  "vcs_repository": "https://github.com/crazy-max/buildx.git",
  "vcs_revision": "04aab6958cb5feb012a3c607569573b5cab141e1",
  "target": "binaries",
  "platform": [
    "linux/amd64"
  ],
  "keep_git_dir": true,
  "started_at": "2025-02-06T16:15:13.077644732+01:00",
  "complete_at": "2025-02-06T16:16:17.046656296+01:00",
  "duration": 63969011564,
  "status": "completed",
  "num_completed_steps": 16,
  "num_total_steps": 16,
  "num_cached_steps": 4,
  "config": {}
}
```
