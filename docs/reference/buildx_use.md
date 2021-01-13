# buildx use

```
Usage:  docker buildx use [OPTIONS] NAME

Set the current builder instance

Options:
      --builder string   Override the configured builder instance
      --default          Set builder as default for current context
      --global           Builder persists context changes
```

## Description

Switches the current builder instance. Build commands invoked after this command
will run on a specified builder. Alternatively, a context name can be used to
switch to the default builder of that context.
