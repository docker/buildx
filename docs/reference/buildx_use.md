# buildx use

```
docker buildx use [OPTIONS] NAME
```

<!---MARKER_GEN_START-->
Set the current builder instance

### Options

| Name | Description |
| --- | --- |
| `--builder string` | Override the configured builder instance |
| `--default` | Set builder as default for current context |
| `--global` | Builder persists context changes |


<!---MARKER_GEN_END-->

## Description

Switches the current builder instance. Build commands invoked after this command
will run on a specified builder. Alternatively, a context name can be used to
switch to the default builder of that context.
